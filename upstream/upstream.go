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
	CNAME  string // 添加 CNAME 字段
	TTL    uint32 // 上游 DNS 返回的 TTL（对所有 IP 取最小值）
	Error  error
	Server string // 添加服务器字段
	Rcode  int    // DNS 响应代码
}

// QueryResultWithTTL 带 TTL 信息的查询结果
type QueryResultWithTTL struct {
	IPs   []string
	CNAME string // 添加 CNAME 字段
	TTL   uint32 // 上游 DNS 返回的 TTL
}

// Upstream 上游 DNS 查询模块
type Upstream struct {
	servers     []string
	strategy    string // parallel, random
	timeoutMs   int
	concurrency int // 并行查询时的并发数
	stats       *stats.Stats
}

// NewUpstream 创建上游 DNS 查询器
func NewUpstream(servers []string, strategy string, timeoutMs int, concurrency int, s *stats.Stats) *Upstream {
	if len(servers) == 0 {
		servers = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}
	if strategy == "" {
		strategy = "random"
	}
	if timeoutMs <= 0 {
		timeoutMs = 300
	}
	if concurrency <= 0 {
		concurrency = 3
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
	if u.strategy == "parallel" {
		return u.queryParallel(ctx, domain, qtype)
	}
	return u.queryRandom(ctx, domain, qtype)
}

// queryParallel 并行查询多个上游 DNS 服务器，返回最快的成功响应
func (u *Upstream) queryParallel(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	log.Printf("[queryParallel] 并行查询 %d 个服务器，查询 %s (type=%s)，并发数=%d\n",
		len(u.servers), domain, dns.TypeToString[qtype], u.concurrency)

	// 创建结果通道
	resultChan := make(chan *QueryResult, len(u.servers))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 使用 semaphore 控制并发数
	sem := make(chan struct{}, u.concurrency)
	var wg sync.WaitGroup

	// 并发查询所有服务器
	for _, server := range u.servers {
		wg.Add(1)
		go func(srv string) {
			defer wg.Done()

			// 获取信号量
			sem <- struct{}{}
			defer func() { <-sem }()

			// 如果已经取消，直接返回
			select {
			case <-ctx.Done():
				return
			default:
			}

			result := u.querySingleServer(ctx, srv, domain, qtype)

			// 只发送结果，不管成功还是失败
			select {
			case resultChan <- result:
			case <-ctx.Done():
			}
		}(server)
	}

	// 启动一个 goroutine 等待所有查询完成后关闭通道
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 收集结果
	var firstError error
	successCount := 0
	failureCount := 0

	for result := range resultChan {
		if result.Error != nil {
			failureCount++
			if u.stats != nil {
				// 只有非 NXDOMAIN 的错误才计为上游失败
				if result.Rcode != dns.RcodeNameError {
					u.stats.IncUpstreamFailure(result.Server)
				}
			}
			if firstError == nil {
				firstError = result.Error
			}
			log.Printf("[queryParallel] 服务器 %s 查询失败: %v\n", result.Server, result.Error)
			continue
		}

		// 找到第一个成功的响应
		successCount++
		if u.stats != nil {
			u.stats.IncUpstreamSuccess(result.Server)
		}
		log.Printf("[queryParallel] 服务器 %s 查询成功（第%d个成功），返回 %d 个IP, CNAME=%s (TTL=%d秒): %v\n",
			result.Server, successCount, len(result.IPs), result.CNAME, result.TTL, result.IPs)

		// 取消其他正在进行的查询
		cancel()

		return &QueryResultWithTTL{IPs: result.IPs, CNAME: result.CNAME, TTL: result.TTL}, nil
	}

	// 所有服务器都失败了
	log.Printf("[queryParallel] 所有 %d 个服务器查询均失败\n", failureCount)
	if firstError != nil {
		return nil, firstError
	}
	return nil, fmt.Errorf("all upstream servers failed")
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
			// 只有非 NXDOMAIN 的错误才计为上游失败
			// NXDOMAIN 是正常的业务响应（域名不存在），不应计为服务器故障
			if result.Rcode != dns.RcodeNameError {
				u.stats.IncUpstreamFailure(server)
			}
		}
		return nil, result.Error
	}

	if u.stats != nil {
		u.stats.IncUpstreamSuccess(server)
	}
	log.Printf("[queryRandom] 查询成功，返回 %d 个IP, CNAME=%s (TTL=%d秒): %v\n", len(result.IPs), result.CNAME, result.TTL, result.IPs)
	return &QueryResultWithTTL{IPs: result.IPs, CNAME: result.CNAME, TTL: result.TTL}, nil
}

// querySingleServer 查询单个上游 DNS 服务器
func (u *Upstream) querySingleServer(ctx context.Context, server, domain string, qtype uint16) *QueryResult {
	// 构造 DNS 请求
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), qtype)

	log.Printf("[querySingleServer] 向 %s 查询 %s (type=%s)\n", server, domain, dns.TypeToString[qtype])

	// 执行查询
	reply, _, err := u.doExchange(ctx, server, msg)
	if err != nil {
		log.Printf("[querySingleServer] 查询 %s 失败: %v\n", server, err)
		return &QueryResult{Error: err, Server: server}
	}

	if reply == nil || reply.Rcode != dns.RcodeSuccess {
		rcode := dns.RcodeServerFailure
		if reply != nil {
			rcode = reply.Rcode
			log.Printf("[querySingleServer] %s 返回错误代码: %d\n", server, reply.Rcode)
		} else {
			log.Printf("[querySingleServer] %s 返回空响应\n", server)
		}
		return &QueryResult{Error: fmt.Errorf("dns query failed: rcode=%d", rcode), Server: server, Rcode: rcode}
	}

	// 提取 IP 地址和 TTL
	ips, cname, ttl := extractIPs(reply)
	log.Printf("[querySingleServer] %s 返回 %d 个IP, CNAME=%s (TTL=%d秒): %v\n", server, len(ips), cname, ttl, ips)
	return &QueryResult{IPs: ips, CNAME: cname, TTL: ttl, Server: server, Rcode: reply.Rcode}
}

// doExchange 执行底层的 DNS 交换
func (u *Upstream) doExchange(ctx context.Context, server string, m *dns.Msg) (*dns.Msg, time.Duration, error) {
	// 确保有端口
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, "53")
	}

	client := &dns.Client{
		Net:     "udp",
		Timeout: time.Duration(u.timeoutMs) * time.Millisecond,
	}

	return client.ExchangeContext(ctx, m, server)
}

// extractIPs 从 DNS 响应中提取 IP 地址、CNAME 和最小 TTL
// 返回值：IP 列表、CNAME、最小 TTL（秒）
func extractIPs(msg *dns.Msg) ([]string, string, uint32) {
	var ips []string
	var cname string
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
		case *dns.CNAME:
			if cname == "" {
				cname = rr.Target
			}
			if minTTL == 0 || rr.Hdr.Ttl < minTTL {
				minTTL = rr.Hdr.Ttl
			}
		}
	}

	// 如果没有找到任何记录，使用默认 TTL（60 秒）
	if minTTL == 0 {
		minTTL = 60
	}

	return ips, cname, minTTL
}
