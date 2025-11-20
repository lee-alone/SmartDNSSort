package upstream

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"smartdnssort/stats"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// QueryResult 查询结果
type QueryResult struct {
	IPs    []string
	TTL    uint32 // 上游 DNS 返回的 TTL（对所有 IP 取最小值）
	Error  error
	Server string // 添加服务器字段
}

// QueryResultWithTTL 带 TTL 信息的查询结果
type QueryResultWithTTL struct {
	IPs []string
	TTL uint32 // 上游 DNS 返回的 TTL
}

// Upstream 上游 DNS 查询模块
type Upstream struct {
	servers     []string
	strategy    string // parallel, random
	timeoutMs   int
	concurrency int
	stats       *stats.Stats
}

// NewUpstream 创建上游 DNS 查询器
func NewUpstream(servers []string, strategy string, timeoutMs, concurrency int, s *stats.Stats) *Upstream {
	if len(servers) == 0 {
		servers = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}
	if strategy == "" {
		strategy = "parallel"
	}
	if timeoutMs <= 0 {
		timeoutMs = 300
	}
	if concurrency <= 0 {
		concurrency = 4
	}

	return &Upstream{
		servers:     servers,
		strategy:    strategy,
		timeoutMs:   timeoutMs,
		concurrency: concurrency,
		stats:       s,
	}
}

// Query 查询域名，返回 IP 列表和 TTL
func (u *Upstream) Query(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	switch u.strategy {
	case "parallel":
		return u.queryParallel(ctx, domain, qtype)
	case "random":
		return u.queryRandom(ctx, domain, qtype)
	default:
		return u.queryParallel(ctx, domain, qtype)
	}
}

// queryParallel 并行查询所有上游 DNS，合并所有服务器的结果
func (u *Upstream) queryParallel(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	// 限制并发数
	sem := make(chan struct{}, u.concurrency)
	resultCh := make(chan *QueryResult, len(u.servers))
	var wg sync.WaitGroup

	log.Printf("[queryParallel] 开始并行查询 %s (type=%s)，查询 %d 个上游服务器\n", domain, dns.TypeToString[qtype], len(u.servers))

	for idx, server := range u.servers {
		wg.Add(1)
		go func(serverIdx int, srv string) {
			defer wg.Done()
			log.Printf("[queryParallel] 服务器 #%d (%s) 等待信号量...\n", serverIdx+1, srv)
			sem <- struct{}{}
			defer func() { <-sem }()

			log.Printf("[queryParallel] 服务器 #%d (%s) 开始查询 %s\n", serverIdx+1, srv, domain)
			result := u.querySingleServer(ctx, srv, domain, qtype)
			resultCh <- result
		}(idx, server)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 合并所有成功的结果
	ipMap := make(map[string]bool) // 使用 map 进行去重
	var allIPs []string
	var minTTL uint32 = 0 // 所有 IP 中的最小 TTL
	successCount := 0
	failureCount := 0

	for result := range resultCh {
		if result.Error == nil && len(result.IPs) > 0 {
			successCount++
			log.Printf("[queryParallel] 服务器 %s 查询成功，返回 %d 个IP (TTL=%d秒): %v\n", result.Server, len(result.IPs), result.TTL, result.IPs)
			if u.stats != nil {
				u.stats.IncUpstreamSuccess(result.Server)
			}
			// 合并 IP，去重
			for _, ip := range result.IPs {
				if !ipMap[ip] {
					ipMap[ip] = true
					allIPs = append(allIPs, ip)
				}
			}
			// 取最小 TTL
			if minTTL == 0 || result.TTL < minTTL {
				minTTL = result.TTL
			}
		} else {
			failureCount++
			log.Printf("[queryParallel] 服务器 %s 查询失败: %v\n", result.Server, result.Error)
			if u.stats != nil {
				u.stats.IncUpstreamFailure(result.Server)
			}
		}
	}

	log.Printf("[queryParallel] 查询完成: 成功%d个, 失败%d个, 合并后共 %d 个唯一IP (最小TTL=%d秒): %v\n", successCount, failureCount, len(allIPs), minTTL, allIPs)

	if len(allIPs) == 0 {
		return nil, fmt.Errorf("all upstream servers failed")
	}

	return &QueryResultWithTTL{IPs: allIPs, TTL: minTTL}, nil
}

// queryRandom 随机选择一个上游 DNS 服务器进行查询
func (u *Upstream) queryRandom(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	// 随机选择一个服务器
	server := u.servers[rand.Intn(len(u.servers))]

	log.Printf("[queryRandom] 随机选择服务器 %s 查询 %s (type=%s)\n", server, domain, dns.TypeToString[qtype])

	result := u.querySingleServer(ctx, server, domain, qtype)
	if result.Error != nil {
		if u.stats != nil {
			u.stats.IncUpstreamFailure(server)
		}
		return nil, result.Error
	}

	if u.stats != nil {
		u.stats.IncUpstreamSuccess(server)
	}
	log.Printf("[queryRandom] 查询成功，返回 %d 个IP (TTL=%d秒): %v\n", len(result.IPs), result.TTL, result.IPs)
	return &QueryResultWithTTL{IPs: result.IPs, TTL: result.TTL}, nil
}

// querySingleServer 查询单个上游 DNS 服务器
func (u *Upstream) querySingleServer(ctx context.Context, server, domain string, qtype uint16) *QueryResult {
	// 确保服务器地址格式正确
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, "53")
	}

	client := &dns.Client{
		Timeout: time.Duration(u.timeoutMs) * time.Millisecond,
		Net:     "udp",
	}

	// 构造 DNS 请求
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), qtype)

	log.Printf("[querySingleServer] 向 %s 查询 %s (type=%s)\n", server, domain, dns.TypeToString[qtype])

	// 执行查询
	reply, _, err := client.ExchangeContext(ctx, msg, server)
	if err != nil {
		log.Printf("[querySingleServer] 查询 %s 失败: %v\n", server, err)
		return &QueryResult{Error: err, Server: server}
	}

	if reply == nil || reply.Rcode != dns.RcodeSuccess {
		log.Printf("[querySingleServer] %s 返回错误代码: %d\n", server, reply.Rcode)
		return &QueryResult{Error: fmt.Errorf("dns query failed: rcode=%d", reply.Rcode), Server: server}
	}

	// 提取 IP 地址和 TTL
	ips, ttl := extractIPs(reply)
	log.Printf("[querySingleServer] %s 返回 %d 个IP (TTL=%d秒): %v\n", server, len(ips), ttl, ips)
	return &QueryResult{IPs: ips, TTL: ttl, Server: server}
}

// Exchange 原始 DNS 消息交换（QueryAll 必须依赖它）
// 为了保持和 queryRandom 一样的行为，这里随机选一个上游服务器进行查询
func (u *Upstream) Exchange(ctx context.Context, m *dns.Msg) (*dns.Msg, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	// 随机选一个服务器，和 queryRandom 策略保持一致（最快）
	server := u.servers[rand.Intn(len(u.servers))]

	// 确保有端口
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, "53")
	}

	client := &dns.Client{
		Net:     "udp",
		Timeout: time.Duration(u.timeoutMs) * time.Millisecond,
	}

	// 直接使用 miekg/dns 的 ExchangeContext
	reply, _, err := client.ExchangeContext(ctx, m, server)
	if err != nil {
		return nil, err
	}
	return reply, nil
}

// extractIPs 从 DNS 响应中提取 IP 地址和最小 TTL
// 返回值：IP 列表、最小 TTL（秒）
func extractIPs(msg *dns.Msg) ([]string, uint32) {
	var ips []string
	var minTTL uint32 = 0 // 0 表示未设置

	for _, answer := range msg.Answer {
		switch rr := answer.(type) {
		case *dns.A:
			ips = append(ips, rr.A.String())
			// 取最小 TTL
			if minTTL == 0 || rr.Hdr.Ttl < minTTL {
				minTTL = rr.Hdr.Ttl
			}
		case *dns.AAAA:
			ips = append(ips, rr.AAAA.String())
			// 取最小 TTL
			if minTTL == 0 || rr.Hdr.Ttl < minTTL {
				minTTL = rr.Hdr.Ttl
			}
		}
	}

	// 如果没有找到任何记录，使用默认 TTL（60 秒）
	if minTTL == 0 {
		minTTL = 60
	}

	return ips, minTTL
}

// QueryAll —— 保底版，Answer + Extra + Ns 段全扫，永不漏 AAAA
func (u *Upstream) QueryAll(ctx context.Context, domain string) (*QueryResultWithTTL, error) {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	m.SetEdns0(4096, true) // 必须！不然很多上游会截断附加记录

	in, err := u.Exchange(ctx, m)
	if err != nil {
		return nil, err
	}
	if in.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("bad rcode: %d", in.Rcode)
	}

	var ips []string
	minTTL := uint32(math.MaxUint32)
	questionName := in.Question[0].Name

	// 1. Answer 段（最常见）
	for _, rr := range in.Answer {
		if rr.Header().Name != questionName {
			continue
		}
		switch v := rr.(type) {
		case *dns.A:
			ips = append(ips, v.A.String())
			if v.Header().Ttl < minTTL {
				minTTL = v.Header().Ttl
			}
		case *dns.AAAA:
			ips = append(ips, v.AAAA.String())
			if v.Header().Ttl < minTTL {
				minTTL = v.Header().Ttl
			}
		}
	}

	// 2. Extra 段（Unbound 疯狂爱放这里！）
	for _, rr := range in.Extra {
		if rr.Header().Name != questionName {
			continue
		}
		if aaaa, ok := rr.(*dns.AAAA); ok {
			ips = append(ips, aaaa.AAAA.String())
			if aaaa.Header().Ttl < minTTL {
				minTTL = aaaa.Header().Ttl
			}
		}
	}

	// 3. 极少数情况会放在 Ns 段（保险起见也扫一下）
	for _, rr := range in.Ns {
		if rr.Header().Name != questionName {
			continue
		}
		if aaaa, ok := rr.(*dns.AAAA); ok {
			ips = append(ips, aaaa.AAAA.String())
			if aaaa.Header().Ttl < minTTL {
				minTTL = aaaa.Header().Ttl
			}
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no A/AAAA found for %s", domain)
	}
	if minTTL == math.MaxUint32 {
		minTTL = 60
	}

	return &QueryResultWithTTL{IPs: ips, TTL: minTTL}, nil
}
