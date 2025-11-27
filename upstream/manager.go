package upstream

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"smartdnssort/stats"
	"sync"

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

// Manager 上游 DNS 查询管理器
type Manager struct {
	servers     []Upstream // 接口列表
	strategy    string     // parallel, random
	timeoutMs   int
	concurrency int // 并行查询时的并发数
	stats       *stats.Stats
}

// NewManager 创建上游 DNS 管理器
func NewManager(servers []Upstream, strategy string, timeoutMs int, concurrency int, s *stats.Stats) *Manager {
	if strategy == "" {
		strategy = "random"
	}
	if timeoutMs <= 0 {
		timeoutMs = 300
	}
	if concurrency <= 0 {
		concurrency = 3
	}

	return &Manager{
		servers:     servers,
		strategy:    strategy,
		timeoutMs:   timeoutMs,
		concurrency: concurrency,
		stats:       s,
	}
}

// Query 查询域名，返回 IP 列表和 TTL
func (u *Manager) Query(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	if u.strategy == "parallel" {
		return u.queryParallel(ctx, domain, qtype)
	}
	return u.queryRandom(ctx, domain, qtype)
}

// queryParallel 并行查询多个上游 DNS 服务器
func (u *Manager) queryParallel(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	log.Printf("[queryParallel] 并行查询 %d 个服务器,查询 %s (type=%s),并发数=%d\n",
		len(u.servers), domain, dns.TypeToString[qtype], u.concurrency)

	// 创建结果通道
	resultChan := make(chan *QueryResult, len(u.servers))

	// 使用 semaphore 控制并发数
	sem := make(chan struct{}, u.concurrency)
	var wg sync.WaitGroup

	// 并发查询所有服务器
	for _, server := range u.servers {
		wg.Add(1)
		go func(srv Upstream) {
			defer wg.Done()

			// 获取信号量
			sem <- struct{}{}
			defer func() { <-sem }()

			// 检查上下文是否已取消
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Execute query using interface
			msg := new(dns.Msg)
			msg.SetQuestion(dns.Fqdn(domain), qtype)

			reply, err := srv.Exchange(ctx, msg)

			var result *QueryResult
			if err != nil {
				result = &QueryResult{Error: err, Server: srv.Address()}
			} else {
				if reply.Rcode != dns.RcodeSuccess {
					result = &QueryResult{
						Error:  fmt.Errorf("dns query failed: rcode=%d", reply.Rcode),
						Server: srv.Address(),
						Rcode:  reply.Rcode,
					}
				} else {
					ips, cname, ttl := extractIPs(reply)
					result = &QueryResult{
						IPs:    ips,
						CNAME:  cname,
						TTL:    ttl,
						Server: srv.Address(),
						Rcode:  reply.Rcode,
					}
				}
			}

			// 发送结果到通道
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

	// 收集所有结果
	var firstSuccessResult *QueryResult
	var firstError error
	allSuccessResults := make([]*QueryResult, 0, len(u.servers))
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

		// 记录成功的响应
		successCount++
		if u.stats != nil {
			u.stats.IncUpstreamSuccess(result.Server)
		}
		log.Printf("[queryParallel] 服务器 %s 查询成功(第%d个成功),返回 %d 个IP, CNAME=%s (TTL=%d秒): %v\n",
			result.Server, successCount, len(result.IPs), result.CNAME, result.TTL, result.IPs)

		// 保存第一个成功的结果(用于快速响应用户)
		if firstSuccessResult == nil {
			firstSuccessResult = result
		}

		// 收集所有成功的结果(用于IP汇总)
		allSuccessResults = append(allSuccessResults, result)
	}

	// 如果没有任何成功的响应,返回错误
	if firstSuccessResult == nil {
		log.Printf("[queryParallel] 所有 %d 个服务器查询均失败\n", failureCount)
		if firstError != nil {
			return nil, firstError
		}
		return nil, fmt.Errorf("all upstream servers failed")
	}

	// 汇总所有IP地址并去重
	mergedIPs := u.mergeAndDeduplicateIPs(allSuccessResults)

	// 选择最小的TTL(最保守的策略)
	minTTL := firstSuccessResult.TTL
	for _, result := range allSuccessResults {
		if result.TTL < minTTL {
			minTTL = result.TTL
		}
	}

	log.Printf("[queryParallel] 汇总完成: 从 %d 个服务器收集到 %d 个唯一IP (原始第一响应: %d 个IP, 汇总后: %d 个IP), CNAME=%s, TTL=%d秒\n",
		successCount, len(mergedIPs), len(firstSuccessResult.IPs), len(mergedIPs), firstSuccessResult.CNAME, minTTL)
	log.Printf("[queryParallel] 完整IP池: %v\n", mergedIPs)

	// 返回汇总后的完整IP池
	return &QueryResultWithTTL{
		IPs:   mergedIPs,
		CNAME: firstSuccessResult.CNAME,
		TTL:   minTTL,
	}, nil
}

// mergeAndDeduplicateIPs 汇总并去重多个查询结果中的IP地址
func (u *Manager) mergeAndDeduplicateIPs(results []*QueryResult) []string {
	ipSet := make(map[string]bool)
	var mergedIPs []string

	for _, result := range results {
		for _, ip := range result.IPs {
			if !ipSet[ip] {
				ipSet[ip] = true
				mergedIPs = append(mergedIPs, ip)
			}
		}
	}

	return mergedIPs
}

// queryRandom 随机选择一个上游 DNS 服务器进行查询
func (u *Manager) queryRandom(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	// 随机选择一个服务器
	server := u.servers[rand.Intn(len(u.servers))]

	log.Printf("[queryRandom] 随机选择服务器 %s 查询 %s (type=%s)\n", server.Address(), domain, dns.TypeToString[qtype])

	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), qtype)

	reply, err := server.Exchange(ctx, msg)
	if err != nil {
		if u.stats != nil {
			u.stats.IncUpstreamFailure(server.Address())
		}
		return nil, err
	}

	if reply.Rcode != dns.RcodeSuccess {
		if reply.Rcode != dns.RcodeNameError {
			if u.stats != nil {
				u.stats.IncUpstreamFailure(server.Address())
			}
		}
		return nil, fmt.Errorf("dns query failed: rcode=%d", reply.Rcode)
	}

	if u.stats != nil {
		u.stats.IncUpstreamSuccess(server.Address())
	}

	ips, cname, ttl := extractIPs(reply)
	log.Printf("[queryRandom] 查询成功，返回 %d 个IP, CNAME=%s (TTL=%d秒): %v\n", len(ips), cname, ttl, ips)
	return &QueryResultWithTTL{IPs: ips, CNAME: cname, TTL: ttl}, nil
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
