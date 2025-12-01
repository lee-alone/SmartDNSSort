package dnsserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"reflect"
	"smartdnssort/adblock"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"smartdnssort/prefetch"
	"smartdnssort/stats"
	"smartdnssort/upstream"
	"smartdnssort/upstream/bootstrap"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
)

// Server DNS 服务器
type Server struct {
	mu                 sync.RWMutex
	cfg                *config.Config
	stats              *stats.Stats
	cache              *cache.Cache
	upstream           *upstream.Manager
	pinger             *ping.Pinger
	sortQueue          *cache.SortQueue
	prefetcher         *prefetch.Prefetcher
	refreshQueue       *RefreshQueue
	recentQueries      [20]string // Circular buffer for recent queries
	recentQueriesIndex int
	recentQueriesMu    sync.Mutex
	udpServer          *dns.Server
	tcpServer          *dns.Server
	adblockManager     *adblock.AdBlockManager // 广告拦截管理器
	requestGroup       singleflight.Group      // 用于合并并发请求
}

// NewServer 创建新的 DNS 服务器
func NewServer(cfg *config.Config, s *stats.Stats) *Server {
	// 创建异步排序队列
	sortQueue := cache.NewSortQueue(cfg.System.SortQueueWorkers, 200, 10*time.Second)

	// 创建异步刷新队列
	refreshQueue := NewRefreshQueue(cfg.System.RefreshWorkers, 100)

	// Initialize Bootstrap Resolver
	boot := bootstrap.NewResolver(cfg.Upstream.BootstrapDNS)

	// Initialize Upstream Interfaces
	var upstreams []upstream.Upstream
	for _, serverUrl := range cfg.Upstream.Servers {
		u, err := upstream.NewUpstream(serverUrl, boot)
		if err != nil {
			logger.Errorf("Failed to create upstream for %s: %v", serverUrl, err)
			continue
		}
		upstreams = append(upstreams, u)
	}

	server := &Server{
		cfg:          cfg,
		stats:        s,
		cache:        cache.NewCache(&cfg.Cache),
		upstream:     upstream.NewManager(upstreams, cfg.Upstream.Strategy, cfg.Upstream.TimeoutMs, cfg.Upstream.Concurrency, s, convertHealthCheckConfig(&cfg.Upstream.HealthCheck)),
		pinger:       ping.NewPinger(cfg.Ping.Count, cfg.Ping.TimeoutMs, cfg.Ping.Concurrency, cfg.Ping.MaxTestIPs, cfg.Ping.RttCacheTtlSeconds, cfg.Ping.Strategy),
		sortQueue:    sortQueue,
		refreshQueue: refreshQueue,
	}

	// 初始化 AdBlock 管理器
	logger.Info("[AdBlock] Initializing AdBlock Manager...")
	adblockMgr, err := adblock.NewManager(&cfg.AdBlock)
	if err != nil {
		logger.Errorf("[AdBlock] Failed to initialize manager: %v", err)
		// If initialization fails, we must ensure it's disabled in config
		cfg.AdBlock.Enable = false
	} else {
		server.adblockManager = adblockMgr
		// Start the adblock manager (downloads rules, etc.)
		go server.adblockManager.Start(context.Background())
		if cfg.AdBlock.Enable {
			logger.Info("[AdBlock] Manager initialized and started (Enabled).")
		} else {
			logger.Info("[AdBlock] Manager initialized and started (Disabled).")
		}
	}

	// 设置刷新队列的工作函数
	refreshQueue.SetWorkFunc(server.refreshCacheAsync)

	// Create the prefetcher and link it with the cache
	server.prefetcher = prefetch.NewPrefetcher(&cfg.Prefetch, s, server.cache, server)
	server.cache.SetPrefetcher(server.prefetcher)

	// 设置排序函数：使用 ping 进行 IP 排序
	sortQueue.SetSortFunc(func(ctx context.Context, ips []string) ([]string, []int, error) {
		return server.performPingSort(ctx, ips)
	})

	// 设置上游管理器的缓存更新回调
	server.setupUpstreamCallback(server.upstream)

	return server
}

// setupUpstreamCallback 设置上游管理器的缓存更新回调
func (s *Server) setupUpstreamCallback(u *upstream.Manager) {
	u.SetCacheUpdateCallback(func(domain string, qtype uint16, ips []string, cname string, ttl uint32) {
		logger.Debugf("[CacheUpdateCallback] 更新缓存: %s (type=%s), IP数量=%d, CNAME=%s, TTL=%d秒",
			domain, dns.TypeToString[qtype], len(ips), cname, ttl)

		// 获取当前原始缓存中的 IP 数量
		var oldIPCount int
		if oldEntry, exists := s.cache.GetRaw(domain, qtype); exists {
			oldIPCount = len(oldEntry.IPs)
		}

		// 更新原始缓存中的IP列表
		// 注意：这里使用 time.Now() 作为获取时间，因为这是后台收集完成的时间
		s.cache.SetRaw(domain, qtype, ips, cname, ttl)

		// 如果后台收集的 IP 数量比之前多，需要重新排序
		if len(ips) > oldIPCount {
			logger.Debugf("[CacheUpdateCallback] 后台收集到更多IP (%d -> %d)，清除旧排序状态并重新排序",
				oldIPCount, len(ips))

			// 清除旧的排序状态，允许重新排序
			s.cache.CancelSort(domain, qtype)

			// 触发异步排序，更新排序缓存
			go s.sortIPsAsync(domain, qtype, ips, ttl, time.Now())
		} else {
			logger.Debugf("[CacheUpdateCallback] IP数量未增加 (%d)，保持现有排序", len(ips))
		}
	})
}

// performPingSort 执行 ping 排序操作
func (s *Server) performPingSort(ctx context.Context, ips []string) ([]string, []int, error) {
	logger.Debugf("[performPingSort] 对 %d 个 IP 进行 ping 排序", len(ips))

	s.mu.RLock()
	pinger := s.pinger
	s.mu.RUnlock()

	// 使用现有的 Pinger 进行 ping 测试和排序
	pingResults := pinger.PingAndSort(ctx, ips)

	if len(pingResults) == 0 {
		return nil, nil, fmt.Errorf("ping sort returned no results")
	}

	// 提取排序后的 IP 和 RTT
	var sortedIPs []string
	var rtts []int
	for _, result := range pingResults {
		sortedIPs = append(sortedIPs, result.IP)
		rtts = append(rtts, result.RTT)
		s.stats.IncPingSuccesses()
	}

	return sortedIPs, rtts, nil
}

// Start 启动 DNS 服务器
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.cfg.DNS.ListenPort)

	// 注册 DNS 处理函数
	dns.HandleFunc(".", s.handleQuery)

	// 启动 UDP 服务器
	s.udpServer = &dns.Server{
		Addr:    addr,
		Net:     "udp",
		Handler: dns.DefaultServeMux,
	}

	// 启动 TCP 服务器（如果启用）
	if s.cfg.DNS.EnableTCP {
		s.tcpServer = &dns.Server{
			Addr:    addr,
			Net:     "tcp",
			Handler: dns.DefaultServeMux,
		}

		go func() {
			logger.Infof("TCP DNS server started on %s", addr)
			if err := s.tcpServer.ListenAndServe(); err != nil {
				logger.Errorf("TCP server error: %v", err)
			}
		}()
	}

	// 启动清理过期缓存的 goroutine
	go s.cleanCacheRoutine()

	// Start the prefetcher
	s.prefetcher.Start()

	logger.Infof("UDP DNS server started on %s", addr)
	return s.udpServer.ListenAndServe()
}

func (s *Server) handleQuery(w dns.ResponseWriter, r *dns.Msg) {
	s.mu.RLock()
	// Copy pointers and values needed for the query under the read lock
	currentUpstream := s.upstream
	currentCfg := s.cfg
	currentStats := s.stats
	adblockMgr := s.adblockManager
	s.mu.RUnlock() // Release the lock early

	currentStats.IncQueries()

	if len(r.Question) == 0 {
		msg := new(dns.Msg)
		msg.SetReply(r)
		w.WriteMsg(msg)
		return
	}

	question := r.Question[0]
	domain := strings.TrimRight(question.Name, ".")
	qtype := question.Qtype

	// AdBlock 过滤检查
	if adblockMgr != nil && currentCfg.AdBlock.Enable {
		// 1. 检查拦截缓存 (快速路径)
		if entry, hit := s.cache.GetBlocked(domain); hit {
			logger.Debugf("[AdBlock] Cache Hit (Blocked): %s (rule: %s)", domain, entry.Rule)
			adblockMgr.RecordBlock(domain, entry.Rule)

			// 使用缓存中的 BlockType 或当前配置
			// 这里我们使用当前配置以保持一致性，但缓存的存在意味着它被拦截了
			switch currentCfg.AdBlock.BlockMode {
			case "nxdomain":
				buildNXDomainResponse(w, r)
			case "zero_ip":
				buildZeroIPResponse(w, r, currentCfg.AdBlock.BlockedResponseIP, currentCfg.AdBlock.BlockedTTL)
			case "refuse":
				buildRefuseResponse(w, r)
			default:
				buildNXDomainResponse(w, r)
			}
			return
		}

		// 2. 检查白名单缓存 (快速路径)
		// 如果在白名单缓存中，直接跳过 AdBlock 检查
		if s.cache.GetAllowed(domain) {
			// log.Printf("[AdBlock] Cache Hit (Allowed): %s", domain)
			// 继续执行后续 DNS 逻辑
		} else {
			// 3. 执行完整的规则匹配
			if blocked, rule := adblockMgr.CheckHost(domain); blocked {
				logger.Debugf("[AdBlock] Blocked: %s (rule: %s)", domain, rule)

				// 记录统计
				adblockMgr.RecordBlock(domain, rule)

				// 写入拦截缓存
				s.cache.SetBlocked(domain, &cache.BlockedCacheEntry{
					BlockType: currentCfg.AdBlock.BlockMode,
					Rule:      rule,
					ExpiredAt: time.Now().Add(time.Duration(currentCfg.AdBlock.BlockedTTL) * time.Second),
				})

				// 根据配置返回响应
				switch currentCfg.AdBlock.BlockMode {
				case "nxdomain":
					buildNXDomainResponse(w, r)
				case "zero_ip":
					buildZeroIPResponse(w, r, currentCfg.AdBlock.BlockedResponseIP, currentCfg.AdBlock.BlockedTTL)
				case "refuse":
					buildRefuseResponse(w, r)
				default:
					buildNXDomainResponse(w, r)
				}
				return
			} else {
				// 写入白名单缓存
				// 缓存 10 分钟 (600秒)，避免频繁检查热门白名单域名
				s.cache.SetAllowed(domain, &cache.AllowedCacheEntry{
					ExpiredAt: time.Now().Add(600 * time.Second),
				})
			}
		}
	}

	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Compress = false

	// ========== 规则过滤 ==========
	// 在处理任何逻辑之前，首先应用本地规则
	if s.handleLocalRules(w, r, msg, domain, question) {
		return // 如果规则已处理该请求，则直接返回
	}

	// 仅处理 A 和 AAAA 查询
	if question.Qtype != dns.TypeA && question.Qtype != dns.TypeAAAA {
		msg.SetRcode(r, dns.RcodeNotImplemented)
		w.WriteMsg(msg)
		return
	}

	// ✅ 记录域名查询逻辑已移动到解析成功后
	// 这样只统计有效域名（能解析出IP的域名）
	s.RecordRecentQuery(domain)

	logger.Debugf("[handleQuery] 查询: %s (type=%s)", domain, dns.TypeToString[question.Qtype])

	// ========== 优先检查错误缓存 ==========
	// 只缓存 NXDOMAIN（域名不存在）错误，不缓存 SERVFAIL 等临时错误
	if _, ok := s.cache.GetError(domain, question.Qtype); ok {
		currentStats.IncCacheHits()
		logger.Debugf("[handleQuery] NXDOMAIN 缓存命中: %s (type=%s)",
			domain, dns.TypeToString[question.Qtype])
		msg.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(msg)
		return
	}

	// ========== 阶段二：排序完成后缓存命中 ==========
	// 优先检查排序缓存（排序完成后的结果）
	if sorted, ok := s.cache.GetSorted(domain, question.Qtype); ok {
		s.cache.RecordAccess(domain, question.Qtype) // 记录访问
		currentStats.IncCacheHits()
		currentStats.RecordDomainQuery(domain) // ✅ 统计有效域名查询

		// 计算剩余 TTL
		elapsed := time.Since(sorted.Timestamp).Seconds()
		remaining := int(sorted.TTL) - int(elapsed)
		if remaining < 0 {
			remaining = 0
		}

		// 计算返回给用户的 TTL
		var userTTL uint32
		if currentCfg.Cache.UserReturnTTL > 0 {
			cycleOffset := int(elapsed) % currentCfg.Cache.UserReturnTTL
			cappedTTL := currentCfg.Cache.UserReturnTTL - cycleOffset

			if remaining < cappedTTL {
				userTTL = uint32(remaining)
			} else {
				userTTL = uint32(cappedTTL)
			}
		} else {
			userTTL = uint32(remaining)
		}

		logger.Debugf("[handleQuery] 排序缓存命中: %s (type=%s) -> %v (原始TTL=%d, 剩余=%d, 返回=%d)",
			domain, dns.TypeToString[question.Qtype], sorted.IPs, sorted.TTL, remaining, userTTL)

		// 检查是否有 CNAME（从原始缓存获取）
		var cname string
		if raw, ok := s.cache.GetRaw(domain, question.Qtype); ok && raw.CNAME != "" {
			cname = raw.CNAME
		}

		// 构造响应
		if cname != "" {
			s.buildDNSResponseWithCNAME(msg, domain, cname, sorted.IPs, question.Qtype, userTTL)
		} else {
			s.buildDNSResponse(msg, domain, sorted.IPs, question.Qtype, userTTL)
		}
		w.WriteMsg(msg)
		return
	}

	// ========== 阶段三:缓存过期后再次访问 ==========
	// 检查原始缓存(上游 DNS 响应缓存)
	if raw, ok := s.cache.GetRaw(domain, question.Qtype); ok {
		s.cache.RecordAccess(domain, question.Qtype) // 记录访问
		currentStats.IncCacheHits()
		currentStats.RecordDomainQuery(domain) // ✅ 统计有效域名查询
		logger.Debugf("[handleQuery] 原始缓存命中: %s (type=%s) -> %v, CNAME=%s (过期:%v)",
			domain, dns.TypeToString[question.Qtype], raw.IPs, raw.CNAME, raw.IsExpired())

		fastTTL := uint32(currentCfg.Cache.FastResponseTTL)

		if raw.CNAME != "" {
			s.buildDNSResponseWithCNAME(msg, domain, raw.CNAME, raw.IPs, question.Qtype, fastTTL)
		} else {
			s.buildDNSResponse(msg, domain, raw.IPs, question.Qtype, fastTTL)
		}
		w.WriteMsg(msg)

		if raw.IsExpired() {
			logger.Debugf("[handleQuery] 原始缓存已过期,触发异步刷新: %s (type=%s)",
				domain, dns.TypeToString[question.Qtype])
			task := RefreshTask{Domain: domain, Qtype: question.Qtype}
			s.refreshQueue.Submit(task)
		} else {
			go s.sortIPsAsync(domain, question.Qtype, raw.IPs, raw.UpstreamTTL, raw.AcquisitionTime)
		}
		return
	}

	currentStats.IncCacheMisses()

	// ========== IPv6 开关检查 ==========
	if question.Qtype == dns.TypeAAAA && !currentCfg.DNS.EnableIPv6 {
		logger.Debugf("[handleQuery] IPv6 已禁用，直接返回空响应: %s", domain)
		msg.SetRcode(r, dns.RcodeSuccess)
		msg.Answer = nil
		w.WriteMsg(msg)
		return
	}

	// ========== 阶段一：首次查询（无缓存）==========
	logger.Debugf("[handleQuery] 首次查询，无缓存: %s (type=%s)", domain, dns.TypeToString[question.Qtype])

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(currentCfg.Upstream.TimeoutMs)*time.Millisecond)
	defer cancel()

	// 使用 singleflight 合并相同的并发请求
	// 这可以防止在高并发下对同一域名发起大量重复的上游查询，避免资源竞争和缓存覆盖问题
	sfKey := fmt.Sprintf("query:%s:%d", domain, question.Qtype)

	v, err, shared := s.requestGroup.Do(sfKey, func() (interface{}, error) {
		return currentUpstream.Query(ctx, domain, question.Qtype)
	})

	if shared {
		logger.Debugf("[handleQuery] 合并并发请求: %s (type=%s)", domain, dns.TypeToString[question.Qtype])
	}

	var result *upstream.QueryResultWithTTL
	if err == nil {
		result = v.(*upstream.QueryResultWithTTL)
	}

	var ips []string
	var cname string
	var upstreamTTL uint32 = uint32(currentCfg.Cache.MaxTTLSeconds)

	if err != nil {
		logger.Warnf("[handleQuery] 上游查询失败: %v", err)
		originalRcode := parseRcodeFromError(err)
		if originalRcode != dns.RcodeNameError {
			currentStats.IncUpstreamFailures()
		}

		if originalRcode == dns.RcodeNameError {
			s.cache.SetError(domain, question.Qtype, originalRcode, currentCfg.Cache.ErrorCacheTTL)
			logger.Debugf("[handleQuery] NXDOMAIN 错误，缓存并返回: %s", domain)
			msg.SetRcode(r, dns.RcodeNameError)
			w.WriteMsg(msg)
		} else {
			logger.Debugf("[handleQuery] SERVFAIL/超时错误，返回空响应（不缓存）: %s, Rcode=%d", domain, originalRcode)
			msg.SetRcode(r, dns.RcodeSuccess)
			msg.Answer = nil
			w.WriteMsg(msg)
		}
		return
	}

	if result != nil {
		ips = result.IPs
		cname = result.CNAME
		upstreamTTL = result.TTL
	}

	if len(ips) > 0 {
		currentStats.RecordDomainQuery(domain)
		logger.Debugf("[handleQuery] 上游查询完成: %s (type=%s) 获得 %d 个IP, CNAME=%s (TTL=%d秒): %v",
			domain, dns.TypeToString[question.Qtype], len(ips), cname, upstreamTTL, ips)

		s.cache.SetRaw(domain, question.Qtype, ips, cname, upstreamTTL)
		go s.sortIPsAsync(domain, question.Qtype, ips, upstreamTTL, time.Now())

		fastTTL := uint32(currentCfg.Cache.FastResponseTTL)
		if cname != "" {
			logger.Debugf("[handleQuery] 构造 CNAME 响应链: %s -> %s -> IPs", domain, cname)
			s.buildDNSResponseWithCNAME(msg, domain, cname, ips, question.Qtype, fastTTL)
		} else {
			s.buildDNSResponse(msg, domain, ips, question.Qtype, fastTTL)
		}
		w.WriteMsg(msg)
		return
	}

	if cname != "" {
		logger.Debugf("[handleQuery] 上游查询返回 CNAME，开始递归解析: %s -> %s", domain, cname)

		finalResult, err := s.resolveCNAME(ctx, cname, question.Qtype)
		if err != nil {
			logger.Warnf("[handleQuery] CNAME 递归解析失败: %v", err)
			msg.SetRcode(r, dns.RcodeServerFailure)
			w.WriteMsg(msg)
			return
		}

		s.cache.SetRaw(domain, qtype, nil, cname, upstreamTTL)

		cnameTargetDomain := strings.TrimRight(dns.Fqdn(cname), ".")
		s.cache.SetRaw(cnameTargetDomain, question.Qtype, finalResult.IPs, "", finalResult.TTL)

		fastTTL := uint32(currentCfg.Cache.FastResponseTTL)
		currentStats.RecordDomainQuery(domain)
		s.buildDNSResponseWithCNAME(msg, domain, cname, finalResult.IPs, question.Qtype, fastTTL)
		w.WriteMsg(msg)

		go s.sortIPsAsync(cnameTargetDomain, question.Qtype, finalResult.IPs, finalResult.TTL, time.Now())
		return
	}

	logger.Debugf("[handleQuery] 上游查询返回空结果 (NODATA): %s", domain)
	msg.SetRcode(r, dns.RcodeSuccess)
	msg.Answer = nil
	w.WriteMsg(msg)
}

// handleLocalRules applies a set of hardcoded rules to block or redirect common bogus queries.
// It returns true if the query was handled, meaning the caller should stop processing.
func (s *Server) handleLocalRules(w dns.ResponseWriter, r *dns.Msg, msg *dns.Msg, domain string, question dns.Question) bool {
	// Rule: Single-label domain (no dots)
	if !strings.Contains(domain, ".") {
		logger.Debugf("[QueryFilter] REFUSED: single-label domain query for '%s'", domain)
		msg.SetRcode(r, dns.RcodeRefused)
		w.WriteMsg(msg)
		return true
	}

	// Rule: localhost
	if domain == "localhost" {
		logger.Debugf("[QueryFilter] STATIC: localhost query for '%s'", domain)
		var ips []string
		switch question.Qtype {
		case dns.TypeA:
			ips = []string{"127.0.0.1"}
		case dns.TypeAAAA:
			ips = []string{"::1"}
		}
		s.buildDNSResponse(msg, domain, ips, question.Qtype, 3600) // 1 hour TTL
		w.WriteMsg(msg)
		return true
	}

	// Rule: Reverse DNS queries
	if strings.HasSuffix(domain, ".in-addr.arpa") || strings.HasSuffix(domain, ".ip6.arpa") {
		logger.Debugf("[QueryFilter] REFUSED: reverse DNS query for '%s'", domain)
		msg.SetRcode(r, dns.RcodeRefused)
		w.WriteMsg(msg)
		return true
	}

	// Rule: Blocklist for specific domains and suffixes
	// Using a map for exact matches is efficient.
	blockedDomains := map[string]int{
		"local":                     dns.RcodeRefused,
		"corp":                      dns.RcodeRefused,
		"home":                      dns.RcodeRefused,
		"lan":                       dns.RcodeRefused,
		"internal":                  dns.RcodeRefused,
		"intranet":                  dns.RcodeRefused,
		"private":                   dns.RcodeRefused,
		"home.arpa":                 dns.RcodeRefused,
		"wpad":                      dns.RcodeNameError, // NXDOMAIN is better for wpad
		"isatap":                    dns.RcodeRefused,
		"teredo.ipv6.microsoft.com": dns.RcodeNameError,
	}

	if rcode, ok := blockedDomains[domain]; ok {
		logger.Debugf("[QueryFilter] Rule match for '%s', responding with %s", domain, dns.RcodeToString[rcode])
		msg.SetRcode(r, rcode)
		w.WriteMsg(msg)
		return true
	}

	return false // Not handled by filter
}

// calculateRemainingTTL 计算剩余 TTL
// 基于上游 TTL 和获取时间，减去已过去的时间，并应用 min/max 限制
// 特殊语义：
//   - min 和 max 都为 0: 不修改上游 TTL
//   - 仅 min 为 0: 只限制最大值
//   - 仅 max 为 0: 只限制最小值
func (s *Server) calculateRemainingTTL(upstreamTTL uint32, acquisitionTime time.Time) int {
	elapsed := time.Since(acquisitionTime).Seconds()
	remaining := int(upstreamTTL) - int(elapsed)

	minTTL := s.cfg.Cache.MinTTLSeconds
	maxTTL := s.cfg.Cache.MaxTTLSeconds

	// 如果 min 和 max 都为 0，不修改上游 TTL
	if minTTL == 0 && maxTTL == 0 {
		return remaining
	}

	// 应用最小值限制（如果 min > 0）
	if minTTL > 0 && remaining < minTTL {
		remaining = minTTL
	}

	// 应用最大值限制（如果 max > 0）
	if maxTTL > 0 && remaining > maxTTL {
		remaining = maxTTL
	}

	return remaining
}

// sortIPsAsync 异步排序 IP 地址
// 排序完成后会更新排序缓存
func (s *Server) sortIPsAsync(domain string, qtype uint16, ips []string, upstreamTTL uint32, acquisitionTime time.Time) {
	// 检查是否已有排序任务在进行
	state, isNew := s.cache.GetOrStartSort(domain, qtype)
	if !isNew {
		logger.Debugf("[sortIPsAsync] 排序任务已在进行: %s (type=%s)，跳过重复排序",
			domain, dns.TypeToString[qtype])
		return
	}

	// 优化：如果只有一个IP，则无需排序
	if len(ips) == 1 {
		logger.Debugf("[sortIPsAsync] 只有一个IP，跳过排序: %s (type=%s) -> %s",
			domain, dns.TypeToString[qtype], ips[0])

		// 直接创建排序结果
		result := &cache.SortedCacheEntry{
			IPs:       ips,
			RTTs:      []int{0}, // RTT 为 0，因为没有测试
			Timestamp: time.Now(),
			TTL:       int(upstreamTTL),
			IsValid:   true,
		}

		// 直接调用回调函数处理排序完成的逻辑
		s.handleSortComplete(domain, qtype, result, nil, state)
		return
	}

	logger.Debugf("[sortIPsAsync] 启动异步排序任务: %s (type=%s), IP数量=%d",
		domain, dns.TypeToString[qtype], len(ips))

	// 创建排序任务
	task := &cache.SortTask{
		Domain: domain,
		Qtype:  qtype,
		IPs:    ips,
		TTL:    uint32(s.calculateRemainingTTL(upstreamTTL, acquisitionTime)),
		Callback: func(result *cache.SortedCacheEntry, err error) {
			s.handleSortComplete(domain, qtype, result, err, state)
		},
	}

	// 提交到排序队列
	// 如果队列已满，回退到同步排序（立即执行）
	if !s.sortQueue.Submit(task) {
		logger.Warnf("[sortIPsAsync] 排序队列已满，改用同步排序: %s (type=%s)",
			domain, dns.TypeToString[qtype])
		task.Callback(nil, fmt.Errorf("sort queue full"))
	}
}

// handleSortComplete 处理排序完成事件
func (s *Server) handleSortComplete(domain string, qtype uint16, result *cache.SortedCacheEntry, err error, state *cache.SortingState) {
	if err != nil {
		logger.Warnf("[handleSortComplete] 排序失败: %s (type=%s), 错误: %v",
			domain, dns.TypeToString[qtype], err)
		s.cache.FinishSort(domain, qtype, nil, err, state)
		return
	}

	if result == nil {
		logger.Warnf("[handleSortComplete] 排序结果为空: %s (type=%s)",
			domain, dns.TypeToString[qtype])
		s.cache.FinishSort(domain, qtype, nil, fmt.Errorf("sort result is nil"), state)
		return
	}

	logger.Debugf("[handleSortComplete] 排序完成: %s (type=%s) -> %v (RTT: %v)",
		domain, dns.TypeToString[qtype], result.IPs, result.RTTs)

	// 从原始缓存获取获取时间，计算剩余 TTL
	raw, exists := s.cache.GetRaw(domain, qtype)
	if exists && raw != nil {
		result.TTL = s.calculateRemainingTTL(raw.UpstreamTTL, raw.AcquisitionTime)
	} else {
		// 如果原始缓存不存在（极少发生），使用最小 TTL 作为兜底
		result.TTL = s.cfg.Cache.MinTTLSeconds
	}

	// 缓存排序结果
	s.cache.SetSorted(domain, qtype, result)

	// 完成排序任务
	s.cache.FinishSort(domain, qtype, result, nil, state)
}

// refreshCacheAsync 异步刷新缓存（用于缓存过期后）
// 重新查询上游 DNS 并排序，更新缓存
func (s *Server) refreshCacheAsync(task RefreshTask) {
	domain := task.Domain
	qtype := task.Qtype

	logger.Debugf("[refreshCacheAsync] 开始异步刷新缓存: %s (type=%s)", domain, dns.TypeToString[qtype])

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.Upstream.TimeoutMs)*time.Millisecond)
	defer cancel()

	// 查询上游 DNS
	result, err := s.upstream.Query(ctx, domain, qtype)
	if err != nil {
		logger.Warnf("[refreshCacheAsync] 刷新缓存失败: %s (type=%s), 错误: %v",
			domain, dns.TypeToString[qtype], err)
		return
	}

	if result == nil || len(result.IPs) == 0 {
		logger.Debugf("[refreshCacheAsync] 刷新缓存返回空结果: %s (type=%s)",
			domain, dns.TypeToString[qtype])
		return
	}

	logger.Debugf("[refreshCacheAsync] 刷新缓存成功，获得 %d 个IP: %v", len(result.IPs), result.IPs)

	// 更新原始缓存
	s.cache.SetRaw(domain, qtype, result.IPs, result.CNAME, result.TTL)

	// 异步排序更新
	go s.sortIPsAsync(domain, qtype, result.IPs, result.TTL, time.Now())
}

// resolveCNAME 递归解析 CNAME，直到找到 IP 地址
func (s *Server) resolveCNAME(ctx context.Context, domain string, qtype uint16) (*upstream.QueryResultWithTTL, error) {
	const maxRedirects = 10
	currentDomain := domain

	for i := 0; i < maxRedirects; i++ {
		logger.Debugf("[resolveCNAME] 递归查询 #%d: %s (type=%s)", i+1, currentDomain, dns.TypeToString[qtype])

		// 检查上下文是否已取消
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// 去掉末尾的点, 以符合内部查询习惯
		queryDomain := strings.TrimRight(currentDomain, ".")

		result, err := s.upstream.Query(ctx, queryDomain, qtype)
		if err != nil {
			return nil, fmt.Errorf("cname resolution failed for %s: %v", queryDomain, err)
		}

		// 如果找到了 IP，解析结束
		if len(result.IPs) > 0 {
			logger.Debugf("[resolveCNAME] 成功解析到 IP: %v for domain %s", result.IPs, queryDomain)
			// CNAME链的最终结果的CNAME字段应为空
			result.CNAME = ""
			return result, nil
		}

		// 如果没有 IP 但有 CNAME，继续重定向
		if result.CNAME != "" {
			logger.Debugf("[resolveCNAME] 发现下一跳 CNAME: %s -> %s", queryDomain, result.CNAME)
			currentDomain = result.CNAME
			continue
		}

		// 如果既没有 IP 也没有 CNAME，说明解析中断
		return nil, fmt.Errorf("cname resolution failed: no IPs or further CNAME found for %s", queryDomain)
	}

	return nil, fmt.Errorf("cname resolution failed: exceeded max redirects for %s", domain)
}

// RefreshDomain is the public method to trigger a cache refresh for a domain.
// It satisfies the prefetch.Refresher interface.
func (s *Server) RefreshDomain(domain string, qtype uint16) {
	// Run in a goroutine to avoid blocking the caller (e.g., the prefetcher loop)
	task := RefreshTask{Domain: domain, Qtype: qtype}
	s.refreshQueue.Submit(task)
}

// buildDNSResponse 构造 DNS 响应
func (s *Server) buildDNSResponse(msg *dns.Msg, domain string, ips []string, qtype uint16, ttl uint32) {
	fqdn := dns.Fqdn(domain)
	logger.Debugf("[buildDNSResponse] 构造响应: %s (type=%s) 包含 %d 个IP, TTL=%d",
		domain, dns.TypeToString[qtype], len(ips), ttl)

	for _, ip := range ips {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}

		switch qtype {
		case dns.TypeA:
			// 返回 IPv4
			if parsedIP.To4() != nil {
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{
						Name:   fqdn,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    ttl,
					},
					A: parsedIP,
				})
			}
		case dns.TypeAAAA:
			// 返回 IPv6
			if parsedIP.To4() == nil && parsedIP.To16() != nil {
				msg.Answer = append(msg.Answer, &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   fqdn,
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    ttl,
					},
					AAAA: parsedIP,
				})
			}
		}
	}
}

// buildDNSResponseWithCNAME 构造包含 CNAME 和 IP 的完整 DNS 响应
// 响应格式：
//
//	www.example.com.  300  IN  CNAME  cdn.example.com.
//	cdn.example.com.  300  IN  A      1.2.3.4
func (s *Server) buildDNSResponseWithCNAME(msg *dns.Msg, domain string, cname string, ips []string, qtype uint16, ttl uint32) {
	fqdn := dns.Fqdn(domain)
	target := dns.Fqdn(cname)

	logger.Debugf("[buildDNSResponseWithCNAME] 构造 CNAME 响应链: %s -> %s, 包含 %d 个IP, TTL=%d\n",
		domain, cname, len(ips), ttl)

	// 1. 首先添加 CNAME 记录
	msg.Answer = append(msg.Answer, &dns.CNAME{
		Hdr: dns.RR_Header{
			Name:   fqdn,
			Rrtype: dns.TypeCNAME,
			Class:  dns.ClassINET,
			Ttl:    ttl,
		},
		Target: target,
	})

	// 2. 然后添加目标域名的 A/AAAA 记录
	for _, ip := range ips {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}

		switch qtype {
		case dns.TypeA:
			// 返回 IPv4，记录名称使用 CNAME 目标
			if parsedIP.To4() != nil {
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{
						Name:   target, // 使用 CNAME 目标作为记录名
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    ttl,
					},
					A: parsedIP,
				})
			}
		case dns.TypeAAAA:
			// 返回 IPv6，记录名称使用 CNAME 目标
			if parsedIP.To4() == nil && parsedIP.To16() != nil {
				msg.Answer = append(msg.Answer, &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   target, // 使用 CNAME 目标作为记录名
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    ttl,
					},
					AAAA: parsedIP,
				})
			}
		}
	}
}

// cleanCacheRoutine 定期清理过期缓存
// 使用固定的清理间隔,与 min_ttl_seconds 配置无关
func (s *Server) cleanCacheRoutine() {
	// 使用固定的60秒清理间隔
	// 注意：这个间隔与 min_ttl_seconds 是独立的概念
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.cache.CleanExpired()
	}
}

// GetStats 获取统计信息
func (s *Server) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats.GetStats()
}

// ClearStats clears all collected statistics.
func (s *Server) ClearStats() {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Println("Clearing all statistics via API request.")
	s.stats.Reset()
}

// RecordRecentQuery adds a domain to the recent queries list.
func (s *Server) RecordRecentQuery(domain string) {
	s.recentQueriesMu.Lock()
	defer s.recentQueriesMu.Unlock()

	s.recentQueries[s.recentQueriesIndex] = domain
	s.recentQueriesIndex = (s.recentQueriesIndex + 1) % len(s.recentQueries)
}

// GetRecentQueries returns a slice of the most recent queries.
func (s *Server) GetRecentQueries() []string {
	s.recentQueriesMu.Lock()
	defer s.recentQueriesMu.Unlock()

	// The buffer is circular, so we need to reconstruct the order.
	// The oldest element is at `s.recentQueriesIndex`.
	var orderedQueries []string
	for i := 0; i < len(s.recentQueries); i++ {
		idx := (s.recentQueriesIndex + i) % len(s.recentQueries)
		if s.recentQueries[idx] != "" {
			orderedQueries = append(orderedQueries, s.recentQueries[idx])
		}
	}
	// Reverse to get the most recent first
	for i, j := 0, len(orderedQueries)-1; i < j; i, j = i+1, j-1 {
		orderedQueries[i], orderedQueries[j] = orderedQueries[j], orderedQueries[i]
	}

	return orderedQueries
}

// GetCache 获取缓存实例（供 WebAPI 使用）
func (s *Server) GetCache() *cache.Cache {
	return s.cache
}

// GetConfig returns the current server configuration.
func (s *Server) GetConfig() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy to prevent race conditions if the caller modifies it
	cfgCopy := *s.cfg
	return &cfgCopy
}

// ApplyConfig applies a new configuration to the running server (hot-reload).
func (s *Server) ApplyConfig(newCfg *config.Config) error {
	log.Println("Applying new configuration...")

	// Create new components outside the lock to avoid blocking.
	// Create new components outside the lock to avoid blocking.
	var newUpstream *upstream.Manager
	if !reflect.DeepEqual(s.cfg.Upstream, newCfg.Upstream) {
		log.Println("Reloading Upstream client due to configuration changes.")

		// Re-initialize bootstrap resolver
		boot := bootstrap.NewResolver(newCfg.Upstream.BootstrapDNS)

		var upstreams []upstream.Upstream
		for _, serverUrl := range newCfg.Upstream.Servers {
			u, err := upstream.NewUpstream(serverUrl, boot)
			if err != nil {
				log.Printf("Failed to create upstream for %s: %v", serverUrl, err)
				continue
			}
			upstreams = append(upstreams, u)
		}

		newUpstream = upstream.NewManager(upstreams, newCfg.Upstream.Strategy, newCfg.Upstream.TimeoutMs, newCfg.Upstream.Concurrency, s.stats, convertHealthCheckConfig(&newCfg.Upstream.HealthCheck))
		// 设置缓存更新回调
		s.setupUpstreamCallback(newUpstream)
	}

	var newPinger *ping.Pinger
	if !reflect.DeepEqual(s.cfg.Ping, newCfg.Ping) {
		log.Println("Reloading Pinger due to configuration changes.")
		newPinger = ping.NewPinger(newCfg.Ping.Count, newCfg.Ping.TimeoutMs, newCfg.Ping.Concurrency, newCfg.Ping.MaxTestIPs, newCfg.Ping.RttCacheTtlSeconds, newCfg.Ping.Strategy)
	}

	var newSortQueue *cache.SortQueue
	if s.cfg.System.SortQueueWorkers != newCfg.System.SortQueueWorkers {
		logger.Infof("Reloading SortQueue from %d to %d workers.", s.cfg.System.SortQueueWorkers, newCfg.System.SortQueueWorkers)
		newSortQueue = cache.NewSortQueue(newCfg.System.SortQueueWorkers, 200, 10*time.Second)
		newSortQueue.SetSortFunc(func(ctx context.Context, ips []string) ([]string, []int, error) {
			return s.performPingSort(ctx, ips)
		})
	}

	var newRefreshQueue *RefreshQueue
	if s.cfg.System.RefreshWorkers != newCfg.System.RefreshWorkers {
		logger.Infof("Reloading RefreshQueue from %d to %d workers.", s.cfg.System.RefreshWorkers, newCfg.System.RefreshWorkers)
		newRefreshQueue = NewRefreshQueue(newCfg.System.RefreshWorkers, 100)
		newRefreshQueue.SetWorkFunc(s.refreshCacheAsync)
	}

	var newPrefetcher *prefetch.Prefetcher
	if !reflect.DeepEqual(s.cfg.Prefetch, newCfg.Prefetch) {
		logger.Info("Reloading Prefetcher due to configuration changes.")
		newPrefetcher = prefetch.NewPrefetcher(&newCfg.Prefetch, s.stats, s.cache, s)
	}

	// Now, acquire the lock and swap the components.
	s.mu.Lock()
	defer s.mu.Unlock()

	if newUpstream != nil {
		s.upstream = newUpstream
	}

	if newPinger != nil {
		if s.pinger != nil {
			s.pinger.Stop()
		}
		s.pinger = newPinger
	}

	if newSortQueue != nil {
		s.sortQueue.Stop()
		s.sortQueue = newSortQueue
	}

	if newRefreshQueue != nil {
		s.refreshQueue.Stop()
		s.refreshQueue = newRefreshQueue
	}

	if newPrefetcher != nil {
		s.prefetcher.Stop()
		s.prefetcher = newPrefetcher
		s.prefetcher.Start()
	}

	// Update the config reference
	s.cfg = newCfg

	logger.Info("New configuration applied successfully.")
	return nil
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown() {
	logger.Info("[Server] 开始关闭服务器...")

	if s.udpServer != nil {
		if err := s.udpServer.Shutdown(); err != nil {
			logger.Errorf("[Server] UDP server shutdown error: %v", err)
		}
	}
	if s.tcpServer != nil {
		if err := s.tcpServer.Shutdown(); err != nil {
			logger.Errorf("[Server] TCP server shutdown error: %v", err)
		}
	}

	s.sortQueue.Stop()
	s.prefetcher.Stop()
	s.refreshQueue.Stop()
	logger.Info("[Server] 服务器已关闭")
}

// GetAdBlockManager returns the adblock manager instance.
func (s *Server) GetAdBlockManager() *adblock.AdBlockManager {
	return s.adblockManager
}

// SetAdBlockEnabled dynamically enables or disables AdBlock filtering
func (s *Server) SetAdBlockEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cfg.AdBlock.Enable = enabled
	logger.Infof("[AdBlock] Filtering status changed to: %v", enabled)
}

// convertHealthCheckConfig 将 config.HealthCheckConfig 转换为 upstream.HealthCheckConfig
func convertHealthCheckConfig(cfg *config.HealthCheckConfig) *upstream.HealthCheckConfig {
	if cfg == nil || !cfg.Enabled {
		// 如果未启用健康检查，返回 nil（将使用默认配置）
		return nil
	}

	return &upstream.HealthCheckConfig{
		FailureThreshold:        cfg.FailureThreshold,
		CircuitBreakerThreshold: cfg.CircuitBreakerThreshold,
		CircuitBreakerTimeout:   cfg.CircuitBreakerTimeout,
		SuccessThreshold:        cfg.SuccessThreshold,
	}
}
