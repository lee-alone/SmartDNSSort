package dnsserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"reflect"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/ping"
	"smartdnssort/prefetch"
	"smartdnssort/stats"
	"smartdnssort/upstream"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Server DNS 服务器
type Server struct {
	mu                 sync.RWMutex
	cfg                *config.Config
	stats              *stats.Stats
	cache              *cache.Cache
	upstream           *upstream.Upstream
	pinger             *ping.Pinger
	sortQueue          *cache.SortQueue
	prefetcher         *prefetch.Prefetcher
	recentQueries      [20]string // Circular buffer for recent queries
	recentQueriesIndex int
	recentQueriesMu    sync.Mutex
}

// NewServer 创建新的 DNS 服务器
func NewServer(cfg *config.Config, s *stats.Stats) *Server {
	// 创建异步排序队列
	sortQueue := cache.NewSortQueue(cfg.System.SortQueueWorkers, 200, 10*time.Second)

	server := &Server{
		cfg:       cfg,
		stats:     s,
		cache:     cache.NewCache(),
		upstream:  upstream.NewUpstream(cfg.Upstream.Servers, cfg.Upstream.Strategy, cfg.Upstream.TimeoutMs, cfg.Upstream.Concurrency, s),
		pinger:    ping.NewPinger(cfg.Ping.Count, cfg.Ping.TimeoutMs, cfg.Ping.Concurrency, cfg.Ping.Strategy),
		sortQueue: sortQueue,
	}

	// Create the prefetcher, but don't start it yet
	server.prefetcher = prefetch.NewPrefetcher(&cfg.Prefetch, s, server.cache, server)

	// 设置排序函数：使用 ping 进行 IP 排序
	sortQueue.SetSortFunc(func(ctx context.Context, ips []string) ([]string, []int, error) {
		return server.performPingSort(ctx, ips)
	})

	return server
}

// performPingSort 执行 ping 排序操作
func (s *Server) performPingSort(ctx context.Context, ips []string) ([]string, []int, error) {
	log.Printf("[performPingSort] 对 %d 个 IP 进行 ping 排序\n", len(ips))

	// 使用现有的 Pinger 进行 ping 测试和排序
	pingResults := s.pinger.PingAndSort(ctx, ips)

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
	udpServer := &dns.Server{
		Addr:    addr,
		Net:     "udp",
		Handler: dns.DefaultServeMux,
	}

	// 启动 TCP 服务器（如果启用）
	if s.cfg.DNS.EnableTCP {
		tcpServer := &dns.Server{
			Addr:    addr,
			Net:     "tcp",
			Handler: dns.DefaultServeMux,
		}

		go func() {
			log.Printf("TCP DNS server started on %s\n", addr)
			if err := tcpServer.ListenAndServe(); err != nil {
				log.Printf("TCP server error: %v\n", err)
			}
		}()
	}

	// 启动清理过期缓存的 goroutine
	go s.cleanCacheRoutine()

	// Start the prefetcher
	s.prefetcher.Start()

	log.Printf("UDP DNS server started on %s\n", addr)
	return udpServer.ListenAndServe()
}

// handleQuery DNS 查询处理函数（三阶段逻辑）
// 阶段一：首次查询（无缓存）
//   - 向上游 DNS 转发请求，获取原始响应
//   - 将响应中的 TTL 修改为 fast_response_ttl，快速返回给用户
//   - 异步启动 IP 排序任务
//
// 阶段二：排序完成后缓存命中
//   - 返回排序后的 IP 列表
//   - TTL 使用 config 中的 ttl 设定规则
//
// 阶段三：缓存过期后再次访问
//   - 立即返回旧缓存内容，TTL 设置为 fast_response_ttl
//   - 异步重新查询上游 DNS，更新缓存与排序结果
func (s *Server) handleQuery(w dns.ResponseWriter, r *dns.Msg) {
	s.mu.RLock()
	// Copy pointers and values needed for the query under the read lock
	currentUpstream := s.upstream
	currentCfg := s.cfg
	currentStats := s.stats
	s.mu.RUnlock() // Release the lock early

	currentStats.IncQueries()

	// 记录域名查询
	if len(r.Question) > 0 {
		domain := strings.TrimRight(r.Question[0].Name, ".")
		currentStats.RecordDomainQuery(domain)
		s.RecordRecentQuery(domain)
	}

	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Compress = false

	if len(r.Question) == 0 {
		w.WriteMsg(msg)
		return
	}

	question := r.Question[0]
	domain := strings.TrimRight(question.Name, ".")

	// 仅处理 A 和 AAAA 查询
	if question.Qtype != dns.TypeA && question.Qtype != dns.TypeAAAA {
		msg.SetRcode(r, dns.RcodeNotImplemented)
		w.WriteMsg(msg)
		return
	}

	log.Printf("[handleQuery] 查询: %s (type=%s)\n", domain, dns.TypeToString[question.Qtype])

	// ========== 阶段二：排序完成后缓存命中 ==========
	// 优先检查排序缓存（排序完成后的结果）
	if sorted, ok := s.cache.GetSorted(domain, question.Qtype); ok {
		currentStats.IncCacheHits()
		log.Printf("[handleQuery] 排序缓存命中: %s (type=%s) -> %v (TTL=%d秒)\n",
			domain, dns.TypeToString[question.Qtype], sorted.IPs, currentCfg.Cache.UserReturnTTL)

		// 使用 user_return_ttl 作为响应的 TTL
		s.buildDNSResponse(msg, domain, sorted.IPs, question.Qtype, uint32(currentCfg.Cache.UserReturnTTL))
		w.WriteMsg(msg)
		return
	}

	currentStats.IncCacheMisses()

	// ========== 阶段三:缓存过期后再次访问 ==========
	// 检查原始缓存(上游 DNS 响应缓存)
	// GetRaw会返回过期的缓存,我们需要检查并决定如何处理
	if raw, ok := s.cache.GetRaw(domain, question.Qtype); ok {
		// 无论是否过期,都立即返回缓存数据
		log.Printf("[handleQuery] 原始缓存命中: %s (type=%s) -> %v (过期:%v)\n",
			domain, dns.TypeToString[question.Qtype], raw.IPs, raw.IsExpired())

		// 立即返回缓存,使用 fast_response_ttl
		fastTTL := uint32(currentCfg.Cache.FastResponseTTL)
		s.buildDNSResponse(msg, domain, raw.IPs, question.Qtype, fastTTL)
		w.WriteMsg(msg)

		// 如果缓存已过期,异步重新查询和排序(更新缓存)
		if raw.IsExpired() {
			log.Printf("[handleQuery] 原始缓存已过期,触发异步刷新: %s (type=%s)\n",
				domain, dns.TypeToString[question.Qtype])
			go s.refreshCacheAsync(domain, question.Qtype)
		}
		return
	}

	// ========== 阶段一：首次查询（无缓存）==========
	log.Printf("[handleQuery] 首次查询，无缓存: %s (type=%s)\n", domain, dns.TypeToString[question.Qtype])

	// 查询上游 DNS
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(currentCfg.Upstream.TimeoutMs)*time.Millisecond)
	defer cancel()

	result, err := currentUpstream.QueryAll(ctx, domain)
	var ips []string
	var upstreamTTL uint32 = uint32(currentCfg.Cache.MaxTTLSeconds)

	if err != nil {
		// 回退到特定类型查询
		result, err = currentUpstream.Query(ctx, domain, question.Qtype)
		if err != nil {
			currentStats.IncUpstreamFailures()
			log.Printf("[handleQuery] 上游查询失败: %v\n", err)
			msg.SetRcode(r, dns.RcodeServerFailure)
			w.WriteMsg(msg)
			return
		}
	}

	if result != nil {
		ips = result.IPs
		upstreamTTL = result.TTL
	}

	log.Printf("[handleQuery] 上游查询完成: %s 获得 %d 个IP (TTL=%d秒): %v\n",
		domain, len(ips), upstreamTTL, ips)

	// 使用 fast_response_ttl 缓存原始响应
	s.cache.SetRaw(domain, question.Qtype, ips, upstreamTTL)

	// 使用 fast_response_ttl 快速返回给用户
	fastTTL := uint32(currentCfg.Cache.FastResponseTTL)
	s.buildDNSResponse(msg, domain, ips, question.Qtype, fastTTL)
	w.WriteMsg(msg)

	// 异步启动 IP 排序任务
	go s.sortIPsAsync(domain, question.Qtype, ips, upstreamTTL, time.Now())
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
	_, isNew := s.cache.GetOrStartSort(domain, qtype)
	if !isNew {
		log.Printf("[sortIPsAsync] 排序任务已在进行: %s (type=%s)，跳过重复排序\n",
			domain, dns.TypeToString[qtype])
		return
	}

	// 优化：如果只有一个IP，则无需排序
	if len(ips) == 1 {
		log.Printf("[sortIPsAsync] 只有一个IP，跳过排序: %s (type=%s) -> %s\n",
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
		s.handleSortComplete(domain, qtype, result, nil)
		return
	}

	log.Printf("[sortIPsAsync] 启动异步排序任务: %s (type=%s), IP数量=%d\n",
		domain, dns.TypeToString[qtype], len(ips))

	// 创建排序任务
	task := &cache.SortTask{
		Domain: domain,
		Qtype:  qtype,
		IPs:    ips,
		TTL:    uint32(s.calculateRemainingTTL(upstreamTTL, acquisitionTime)),
		Callback: func(result *cache.SortedCacheEntry, err error) {
			s.handleSortComplete(domain, qtype, result, err)
		},
	}

	// 提交到排序队列
	// 如果队列已满，回退到同步排序（立即执行）
	if !s.sortQueue.Submit(task) {
		log.Printf("[sortIPsAsync] 排序队列已满，改用同步排序: %s (type=%s)\n",
			domain, dns.TypeToString[qtype])
		task.Callback(nil, fmt.Errorf("sort queue full"))
	}
}

// handleSortComplete 处理排序完成事件
func (s *Server) handleSortComplete(domain string, qtype uint16, result *cache.SortedCacheEntry, err error) {
	if err != nil {
		log.Printf("[handleSortComplete] 排序失败: %s (type=%s), 错误: %v\n",
			domain, dns.TypeToString[qtype], err)
		s.cache.FinishSort(domain, qtype, nil, err)
		return
	}

	if result == nil {
		log.Printf("[handleSortComplete] 排序结果为空: %s (type=%s)\n",
			domain, dns.TypeToString[qtype])
		s.cache.FinishSort(domain, qtype, nil, fmt.Errorf("sort result is nil"))
		return
	}

	log.Printf("[handleSortComplete] 排序完成: %s (type=%s) -> %v (RTT: %v)\n",
		domain, dns.TypeToString[qtype], result.IPs, result.RTTs)

	// 从原始缓存获取获取时间，计算剩余 TTL
	raw, exists := s.cache.GetRawUnsafe(domain, qtype)
	if exists && raw != nil {
		result.TTL = s.calculateRemainingTTL(raw.UpstreamTTL, raw.AcquisitionTime)
	} else {
		// 如果原始缓存不存在（极少发生），使用最小 TTL 作为兜底
		result.TTL = s.cfg.Cache.MinTTLSeconds
	}

	// 缓存排序结果
	s.cache.SetSorted(domain, qtype, result)

	// 完成排序任务
	s.cache.FinishSort(domain, qtype, result, nil)
}

// refreshCacheAsync 异步刷新缓存（用于缓存过期后）
// 重新查询上游 DNS 并排序，更新缓存
func (s *Server) refreshCacheAsync(domain string, qtype uint16) {
	log.Printf("[refreshCacheAsync] 开始异步刷新缓存: %s (type=%s)\n", domain, dns.TypeToString[qtype])

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.Upstream.TimeoutMs)*time.Millisecond)
	defer cancel()

	// 查询上游 DNS
	result, err := s.upstream.Query(ctx, domain, qtype)
	if err != nil {
		log.Printf("[refreshCacheAsync] 刷新缓存失败: %s (type=%s), 错误: %v\n",
			domain, dns.TypeToString[qtype], err)
		return
	}

	if result == nil || len(result.IPs) == 0 {
		log.Printf("[refreshCacheAsync] 刷新缓存返回空结果: %s (type=%s)\n",
			domain, dns.TypeToString[qtype])
		return
	}

	log.Printf("[refreshCacheAsync] 刷新缓存成功，获得 %d 个IP: %v\n", len(result.IPs), result.IPs)

	// 更新原始缓存
	s.cache.SetRaw(domain, qtype, result.IPs, result.TTL)

	// 异步排序更新
	go s.sortIPsAsync(domain, qtype, result.IPs, result.TTL, time.Now())
}

// RefreshDomain is the public method to trigger a cache refresh for a domain.
// It satisfies the prefetch.Refresher interface.
func (s *Server) RefreshDomain(domain string, qtype uint16) {
	// Run in a goroutine to avoid blocking the caller (e.g., the prefetcher loop)
	go s.refreshCacheAsync(domain, qtype)
}

// buildDNSResponse 构造 DNS 响应
func (s *Server) buildDNSResponse(msg *dns.Msg, domain string, ips []string, qtype uint16, ttl uint32) {
	fqdn := dns.Fqdn(domain)
	log.Printf("[buildDNSResponse] 构造响应: %s (type=%s) 包含 %d 个IP, TTL=%d\n",
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

// cleanCacheRoutine 定期清理过期缓存
// 使用固定的清理间隔,与 min_ttl_seconds 配置无关
func (s *Server) cleanCacheRoutine() {
	// 使用固定的60秒清理间隔
	// 注意：这个间隔与 min_ttl_seconds 是独立的概念
	// min_ttl_seconds 用于限制返回给用户的 TTL，而这里决定多久清理一次过期缓存
	const cleanInterval = 60 * time.Second

	ticker := time.NewTicker(cleanInterval)
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
	var newUpstream *upstream.Upstream
	if !reflect.DeepEqual(s.cfg.Upstream, newCfg.Upstream) {
		log.Println("Reloading Upstream client due to configuration changes.")
		newUpstream = upstream.NewUpstream(newCfg.Upstream.Servers, newCfg.Upstream.Strategy, newCfg.Upstream.TimeoutMs, newCfg.Upstream.Concurrency, s.stats)
	}

	var newPinger *ping.Pinger
	if !reflect.DeepEqual(s.cfg.Ping, newCfg.Ping) {
		log.Println("Reloading Pinger due to configuration changes.")
		newPinger = ping.NewPinger(newCfg.Ping.Count, newCfg.Ping.TimeoutMs, newCfg.Ping.Concurrency, newCfg.Ping.Strategy)
	}

	var newSortQueue *cache.SortQueue
	if s.cfg.System.SortQueueWorkers != newCfg.System.SortQueueWorkers {
		log.Printf("Reloading SortQueue from %d to %d workers.", s.cfg.System.SortQueueWorkers, newCfg.System.SortQueueWorkers)
		newSortQueue = cache.NewSortQueue(newCfg.System.SortQueueWorkers, 200, 10*time.Second)
		newSortQueue.SetSortFunc(func(ctx context.Context, ips []string) ([]string, []int, error) {
			// This function will be called by the queue worker.
			// We need to ensure it uses the *current* pinger from the server.
			s.mu.RLock()
			p := s.pinger
			s.mu.RUnlock()

			pingResults := p.PingAndSort(ctx, ips)

			if len(pingResults) == 0 {
				return nil, nil, fmt.Errorf("ping sort returned no results")
			}

			var sortedIPs []string
			var rtts []int
			for _, result := range pingResults {
				sortedIPs = append(sortedIPs, result.IP)
				rtts = append(rtts, result.RTT)
				// Note: s.stats.IncPingSuccesses() is not called here as it's handled in performPingSort
			}
			return sortedIPs, rtts, nil
		})
	}

	var newPrefetcher *prefetch.Prefetcher
	if !reflect.DeepEqual(s.cfg.Prefetch, newCfg.Prefetch) {
		log.Println("Reloading Prefetcher due to configuration changes.")
		newPrefetcher = prefetch.NewPrefetcher(&newCfg.Prefetch, s.stats, s.cache, s)
	}

	// Now, acquire the lock and swap the components.
	s.mu.Lock()
	defer s.mu.Unlock()

	if newUpstream != nil {
		s.upstream = newUpstream
	}

	if newPinger != nil {
		s.pinger = newPinger
	}

	if newSortQueue != nil {
		s.sortQueue.Stop()
		s.sortQueue = newSortQueue
	}

	if newPrefetcher != nil {
		s.prefetcher.Stop()
		s.prefetcher = newPrefetcher
		s.prefetcher.Start()
	}

	// Update the config reference
	s.cfg = newCfg

	log.Println("New configuration applied successfully.")
	return nil
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown() {
	log.Printf("[Server] 开始关闭服务器...\n")
	s.sortQueue.Stop()
	s.prefetcher.Stop()
	log.Printf("[Server] 服务器已关闭\n")
}
