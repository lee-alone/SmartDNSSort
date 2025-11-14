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
}

// NewServer 创建新的 DNS 服务器
func NewServer(cfg *config.Config, s *stats.Stats) *Server {
	return &Server{
		cfg:      cfg,
		stats:    s,
		cache:    cache.NewCache(),
		upstream: upstream.NewUpstream(cfg.Upstream.Servers, cfg.Upstream.Strategy, cfg.Upstream.TimeoutMs, cfg.Upstream.Concurrency),
		pinger:   ping.NewPinger(cfg.Ping.Count, cfg.Ping.TimeoutMs, cfg.Ping.Concurrency, cfg.Ping.Strategy),
	}
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

// handleQuery DNS 查询处理函数
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

	log.Printf("Query: %s (type=%s)\n", domain, dns.TypeToString[question.Qtype])

	// 查询缓存
	if entry, ok := s.cache.Get(domain, question.Qtype); ok {
		s.stats.IncCacheHits()
		log.Printf("Cache hit: %s (type=%s) -> %v\n", domain, dns.TypeToString[question.Qtype], entry.IPs)

		s.buildDNSResponse(msg, domain, entry.IPs, question.Qtype)
		w.WriteMsg(msg)
		return
	}

	s.stats.IncCacheMisses()

	// 查询上游 DNS - 获取所有 A 和 AAAA 记录
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.Upstream.TimeoutMs)*time.Millisecond)
	defer cancel()

	// 优先使用 QueryAll 获取所有 IP
	ips, err := s.upstream.QueryAll(ctx, domain)
	if err != nil {
		// QueryAll 失败则回退到特定类型查询
		ips, err = s.upstream.Query(ctx, domain, question.Qtype)
		if err != nil {
			s.stats.IncUpstreamFailures()
			log.Printf("[handleQuery] Upstream query failed: %v\n", err)

			msg.SetRcode(r, dns.RcodeServerFailure)
			w.WriteMsg(msg)
			return
		}
	}

	log.Printf("[handleQuery] 上游查询完成: %s 获得 %d 个IP: %v\n", domain, len(ips), ips)

	// 并发 ping 排序
	// 超时时间计算: (每个IP的timeout * count * 并发数考虑因素) + 缓冲
	// 公式: timeoutMs * count * (concurrency / avgParallelProcessing) + buffer
	// 例: 500ms * 3次 * (16并发 / 4) + 500ms buffer = 6500ms
	pingTimeoutMs := s.cfg.Ping.TimeoutMs*s.cfg.Ping.Count*(s.cfg.Ping.Concurrency/4) + 500
	log.Printf("[handleQuery] 即将进行ping测试: %d个IP, 超时时间: %dms (单个超时:%d * 次数:%d * 并发:%d + 缓冲:500)\n", len(ips), pingTimeoutMs, s.cfg.Ping.TimeoutMs, s.cfg.Ping.Count, s.cfg.Ping.Concurrency)
	ctx, cancel = context.WithTimeout(context.Background(), time.Duration(pingTimeoutMs)*time.Millisecond)
	defer cancel()

	// 获取排序后的 IP 及其 RTT
	pingResults := s.pinger.PingAndSort(ctx, ips)

	// 提取 IP 和 RTT
	var sortedIPs []string
	var rtts []int
	for _, result := range pingResults {
		sortedIPs = append(sortedIPs, result.IP)
		rtts = append(rtts, result.RTT)
		s.stats.IncPingSuccesses()
	}

	log.Printf("Ping results for %s: %v with RTTs: %v\n", domain, sortedIPs, rtts)

	// 缓存结果
	entry := &cache.CacheEntry{
		IPs:       sortedIPs,
		RTTs:      rtts,
		Timestamp: time.Now(),
		TTL:       s.cfg.Cache.TTLSeconds,
	}
	s.cache.Set(domain, question.Qtype, entry)

	// 构造响应
	s.buildDNSResponse(msg, domain, sortedIPs, question.Qtype)
	w.WriteMsg(msg)
}

// buildDNSResponse 构造 DNS 响应
// 注意：DNS 协议本身只支持返回 IP，RTT 信息存储在服务器缓存中
// 如需要返回 RTT，可通过自定义 API 接口实现（见 WebUI 模块）
func (s *Server) buildDNSResponse(msg *dns.Msg, domain string, ips []string, qtype uint16) {
	fqdn := dns.Fqdn(domain)
	log.Printf("Building DNS response for %s (type=%s) with IPs: %v\n", domain, dns.TypeToString[qtype], ips)

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
						Ttl:    uint32(s.cfg.Cache.TTLSeconds),
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
						Ttl:    uint32(s.cfg.Cache.TTLSeconds),
					},
					AAAA: parsedIP,
				})
			}
		}
	}
}

// cleanCacheRoutine 定期清理过期缓存
func (s *Server) cleanCacheRoutine() {
	ticker := time.NewTicker(time.Duration(s.cfg.Cache.TTLSeconds) * time.Second)
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
