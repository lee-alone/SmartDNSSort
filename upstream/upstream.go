package upstream

import (
	"context"
	"fmt"
	"log"
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
	Error  error
	Server string // 添加服务器字段
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

// Query 查询域名，返回 IP 列表
func (u *Upstream) Query(ctx context.Context, domain string, qtype uint16) ([]string, error) {
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
func (u *Upstream) queryParallel(ctx context.Context, domain string, qtype uint16) ([]string, error) {
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
	successCount := 0
	failureCount := 0

	for result := range resultCh {
		if result.Error == nil && len(result.IPs) > 0 {
			successCount++
			log.Printf("[queryParallel] 服务器 %s 查询成功，返回 %d 个IP: %v\n", result.Server, len(result.IPs), result.IPs)
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
		} else {
			failureCount++
			log.Printf("[queryParallel] 服务器 %s 查询失败: %v\n", result.Server, result.Error)
			if u.stats != nil {
				u.stats.IncUpstreamFailure(result.Server)
			}
		}
	}

	log.Printf("[queryParallel] 查询完成: 成功%d个, 失败%d个, 合并后共 %d 个唯一IP: %v\n", successCount, failureCount, len(allIPs), allIPs)

	if len(allIPs) == 0 {
		return nil, fmt.Errorf("all upstream servers failed")
	}

	return allIPs, nil
}

// queryRandom 随机选择一个上游 DNS 服务器进行查询
func (u *Upstream) queryRandom(ctx context.Context, domain string, qtype uint16) ([]string, error) {
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
	log.Printf("[queryRandom] 查询成功，返回 %d 个IP: %v\n", len(result.IPs), result.IPs)
	return result.IPs, nil
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

	// 提取 IP 地址
	ips := extractIPs(reply)
	log.Printf("[querySingleServer] %s 返回 %d 个IP: %v\n", server, len(ips), ips)
	return &QueryResult{IPs: ips, Server: server}
}

// extractIPs 从 DNS 响应中提取 IP 地址
func extractIPs(msg *dns.Msg) []string {
	var ips []string

	for _, answer := range msg.Answer {
		switch rr := answer.(type) {
		case *dns.A:
			ips = append(ips, rr.A.String())
		case *dns.AAAA:
			ips = append(ips, rr.AAAA.String())
		}
	}

	return ips
}

// QueryAll 查询域名的所有 A 和 AAAA 记录，返回混合的 IP 列表
func (u *Upstream) QueryAll(ctx context.Context, domain string) ([]string, error) {
	// 并发查询 A 和 AAAA 记录
	ipsChan := make(chan []string, 2)
	errChan := make(chan error, 2)

	// 查询 A 记录
	go func() {
		ips, err := u.Query(ctx, domain, dns.TypeA)
		if err != nil {
			errChan <- nil // A 记录可能不存在，不作为错误
		} else {
			ipsChan <- ips
		}
	}()

	// 查询 AAAA 记录
	go func() {
		ips, err := u.Query(ctx, domain, dns.TypeAAAA)
		if err != nil {
			errChan <- nil // AAAA 记录可能不存在，不作为错误
		} else {
			ipsChan <- ips
		}
	}()

	// 收集结果
	var allIPs []string
	count := 0

	for count < 2 {
		select {
		case ips := <-ipsChan:
			allIPs = append(allIPs, ips...)
			count++
		case <-errChan:
			count++
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if len(allIPs) == 0 {
		return nil, fmt.Errorf("no A or AAAA records found")
	}

	return allIPs, nil
}
