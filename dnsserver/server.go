package dnsserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/ping"
	"smartdnssort/stats"
	"smartdnssort/upstream"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// Server DNS 服务器
type Server struct {
	cfg       *config.Config
	stats     *stats.Stats
	cache     *cache.Cache
	upstream  *upstream.Upstream
	pinger    *ping.Pinger
	dnsServer *dns.Server
	sortQueue *cache.SortQueue
}

// NewServer 创建新的 DNS 服务器
func NewServer(cfg *config.Config, s *stats.Stats) *Server {
	// 创建异步排序队列（4 个工作线程，队列大小 200，超时时间 10 秒）
	sortQueue := cache.NewSortQueue(4, 200, 10*time.Second)

	server := &Server{
		cfg:       cfg,
		stats:     s,
		cache:     cache.NewCache(),
		upstream:  upstream.NewUpstream(cfg.Upstream.Servers, cfg.Upstream.Strategy, cfg.Upstream.TimeoutMs, cfg.Upstream.Concurrency, s),
		pinger:    ping.NewPinger(cfg.Ping.Count, cfg.Ping.TimeoutMs, cfg.Ping.Concurrency, cfg.Ping.Strategy),
		sortQueue: sortQueue,
	}

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
	s.stats.IncQueries()

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
		s.stats.IncCacheHits()
		log.Printf("[handleQuery] 排序缓存命中: %s (type=%s) -> %v (TTL=%d秒)\n",
			domain, dns.TypeToString[question.Qtype], sorted.IPs, sorted.TTL)

		// 计算剩余 TTL
		elapsedSeconds := int(time.Since(sorted.Timestamp).Seconds())
		remainingTTL := sorted.TTL - elapsedSeconds
		if remainingTTL <= 0 {
			remainingTTL = 1
		}

		s.buildDNSResponse(msg, domain, sorted.IPs, question.Qtype, uint32(remainingTTL))
		w.WriteMsg(msg)
		return
	}

	s.stats.IncCacheMisses()

	// ========== 阶段三：缓存过期后再次访问 ==========
	// 检查原始缓存（上游 DNS 响应缓存）
	if raw, ok := s.cache.GetRaw(domain, question.Qtype); ok {
		log.Printf("[handleQuery] 原始缓存命中（缓存已过期，但仍在池中）: %s (type=%s) -> %v\n",
			domain, dns.TypeToString[question.Qtype], raw.IPs)

		// 立即返回旧缓存，使用 fast_response_ttl
		fastTTL := uint32(s.cfg.Cache.FastResponseTTL)
		s.buildDNSResponse(msg, domain, raw.IPs, question.Qtype, fastTTL)
		w.WriteMsg(msg)

		// 异步重新查询和排序（更新缓存）
		go s.refreshCacheAsync(domain, question.Qtype)
		return
	}

	// ========== 阶段一：首次查询（无缓存）==========
	log.Printf("[handleQuery] 首次查询，无缓存: %s (type=%s)\n", domain, dns.TypeToString[question.Qtype])

	// 查询上游 DNS
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.Upstream.TimeoutMs)*time.Millisecond)
	defer cancel()

	result, err := s.upstream.QueryAll(ctx, domain)
	var ips []string
	var upstreamTTL uint32 = uint32(s.cfg.Cache.MaxTTLSeconds)

	if err != nil {
		// 回退到特定类型查询
		result, err = s.upstream.Query(ctx, domain, question.Qtype)
		if err != nil {
			s.stats.IncUpstreamFailures()
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

	// 缓存原始响应
	s.cache.SetRaw(domain, question.Qtype, ips, upstreamTTL)

	// 使用 fast_response_ttl 快速返回给用户
	fastTTL := uint32(s.cfg.Cache.FastResponseTTL)
	s.buildDNSResponse(msg, domain, ips, question.Qtype, fastTTL)
	w.WriteMsg(msg)

	// 异步启动 IP 排序任务
	go s.sortIPsAsync(domain, question.Qtype, ips, upstreamTTL)
}

// sortIPsAsync 异步排序 IP 地址
// 排序完成后会更新排序缓存
func (s *Server) sortIPsAsync(domain string, qtype uint16, ips []string, upstreamTTL uint32) {
	// 检查是否已有排序任务在进行
	_, isNew := s.cache.GetOrStartSort(domain, qtype)
	if !isNew {
		log.Printf("[sortIPsAsync] 排序任务已在进行: %s (type=%s)，跳过重复排序\n",
			domain, dns.TypeToString[qtype])
		return
	}

	log.Printf("[sortIPsAsync] 启动异步排序任务: %s (type=%s), IP数量=%d\n",
		domain, dns.TypeToString[qtype], len(ips))

	// 创建排序任务
	task := &cache.SortTask{
		Domain: domain,
		Qtype:  qtype,
		IPs:    ips,
		TTL:    upstreamTTL,
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

	// 应用 TTL 范围限制
	finalTTL := uint32(result.TTL)
	if finalTTL < uint32(s.cfg.Cache.MinTTLSeconds) {
		finalTTL = uint32(s.cfg.Cache.MinTTLSeconds)
	}
	if finalTTL > uint32(s.cfg.Cache.MaxTTLSeconds) {
		finalTTL = uint32(s.cfg.Cache.MaxTTLSeconds)
	}
	result.TTL = int(finalTTL)

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
	go s.sortIPsAsync(domain, qtype, result.IPs, result.TTL)
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
func (s *Server) cleanCacheRoutine() {
	ticker := time.NewTicker(time.Duration(s.cfg.Cache.MinTTLSeconds) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.cache.CleanExpired()
	}
}

// GetStats 获取统计信息
func (s *Server) GetStats() map[string]interface{} {
	return s.stats.GetStats()
}

// GetCache 获取缓存实例（供 WebAPI 使用）
func (s *Server) GetCache() *cache.Cache {
	return s.cache
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown() {
	log.Printf("[Server] 开始关闭服务器...\n")
	s.sortQueue.Stop()
	log.Printf("[Server] 服务器已关闭\n")
}
