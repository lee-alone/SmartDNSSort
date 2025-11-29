package upstream

import (
	"context"
	"fmt"
	"log"
	"math/rand"
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

// Manager 上游 DNS 查询管理器
type Manager struct {
	servers     []Upstream // 接口列表
	strategy    string     // parallel, random
	timeoutMs   int
	concurrency int // 并行查询时的并发数
	stats       *stats.Stats
	// 缓存更新回调函数，用于在 parallel 模式下后台收集完所有响应后更新缓存
	cacheUpdateCallback func(domain string, qtype uint16, ips []string, cname string, ttl uint32)
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

// SetCacheUpdateCallback 设置缓存更新回调函数
// 用于在 parallel 模式下后台收集完所有响应后更新缓存
func (u *Manager) SetCacheUpdateCallback(callback func(domain string, qtype uint16, ips []string, cname string, ttl uint32)) {
	u.cacheUpdateCallback = callback
}

// Query 查询域名，返回 IP 列表和 TTL
func (u *Manager) Query(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	if u.strategy == "parallel" {
		return u.queryParallel(ctx, domain, qtype)
	}
	return u.queryRandom(ctx, domain, qtype)
}

// queryParallel 并行查询多个上游 DNS 服务器
// 实现快速响应机制：第一个成功的响应立即返回，后台继续收集其他响应并更新缓存
func (u *Manager) queryParallel(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	log.Printf("[queryParallel] 并行查询 %d 个服务器,查询 %s (type=%s),并发数=%d\n",
		len(u.servers), domain, dns.TypeToString[qtype], u.concurrency)

	// 创建结果通道
	resultChan := make(chan *QueryResult, len(u.servers))

	// 创建一个用于快速响应的通道
	fastResponseChan := make(chan *QueryResult, 1)

	// 创建一个独立于请求上下文的 context，用于控制上游查询的超时
	// 这样即使主请求返回（ctx 被取消），后台查询也能继续进行
	queryCtx, cancel := context.WithTimeout(context.Background(), time.Duration(u.timeoutMs)*time.Millisecond)

	// 使用 semaphore 控制并发数
	sem := make(chan struct{}, u.concurrency)
	var wg sync.WaitGroup

	// 用于标记是否已经发送了快速响应
	var fastResponseSent sync.Once

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
			case <-queryCtx.Done():
				return
			default:
			}

			// Execute query using interface
			msg := new(dns.Msg)
			msg.SetQuestion(dns.Fqdn(domain), qtype)

			reply, err := srv.Exchange(queryCtx, msg)

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
			case <-queryCtx.Done():
				return
			}

			// 如果是第一个成功的响应，立即发送到快速响应通道
			if result.Error == nil && len(result.IPs) > 0 {
				fastResponseSent.Do(func() {
					select {
					case fastResponseChan <- result:
						log.Printf("[queryParallel] 🚀 快速响应: 服务器 %s 第一个返回成功结果，立即响应用户\n", srv.Address())
					default:
					}
				})
			}
		}(server)
	}

	// 启动一个 goroutine 等待所有查询完成后关闭通道
	go func() {
		wg.Wait()
		close(resultChan)
		close(fastResponseChan)
		cancel() // 释放 context 资源
	}()

	// 等待第一个成功的响应（快速响应）
	var fastResponse *QueryResult
	select {
	case fastResponse = <-fastResponseChan:
		if fastResponse != nil {
			log.Printf("[queryParallel] ✅ 收到快速响应: 服务器 %s 返回 %d 个IP, CNAME=%s (TTL=%d秒): %v\n",
				fastResponse.Server, len(fastResponse.IPs), fastResponse.CNAME, fastResponse.TTL, fastResponse.IPs)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// 如果没有收到快速响应，说明所有服务器都失败了
	if fastResponse == nil {
		// 等待所有结果收集完成，看是否有错误信息
		var firstError error
		for result := range resultChan {
			if result.Error != nil && firstError == nil {
				firstError = result.Error
			}
		}
		if firstError != nil {
			return nil, firstError
		}
		return nil, fmt.Errorf("all upstream servers failed")
	}

	// 记录快速响应的统计
	if u.stats != nil {
		u.stats.IncUpstreamSuccess(fastResponse.Server)
	}

	// 在后台继续收集其他服务器的响应并更新缓存
	go u.collectRemainingResponses(domain, qtype, fastResponse, resultChan)

	// 立即返回第一个成功的响应
	return &QueryResultWithTTL{
		IPs:   fastResponse.IPs,
		CNAME: fastResponse.CNAME,
		TTL:   fastResponse.TTL,
	}, nil
}

// collectRemainingResponses 在后台收集剩余的响应并更新缓存
func (u *Manager) collectRemainingResponses(domain string, qtype uint16, fastResponse *QueryResult, resultChan chan *QueryResult) {
	log.Printf("[collectRemainingResponses] 🔄 开始后台收集剩余响应: %s (type=%s)\n", domain, dns.TypeToString[qtype])

	allSuccessResults := []*QueryResult{fastResponse}
	successCount := 1
	failureCount := 0

	// 收集剩余的结果
	for result := range resultChan {
		// 跳过已经作为快速响应返回的结果
		if result == fastResponse {
			continue
		}

		if result.Error != nil {
			failureCount++
			if u.stats != nil {
				// 只有非 NXDOMAIN 的错误才计为上游失败
				if result.Rcode != dns.RcodeNameError {
					u.stats.IncUpstreamFailure(result.Server)
				}
			}
			log.Printf("[collectRemainingResponses] 服务器 %s 查询失败: %v\n", result.Server, result.Error)
			continue
		}

		// 记录成功的响应
		successCount++
		if u.stats != nil {
			u.stats.IncUpstreamSuccess(result.Server)
		}
		log.Printf("[collectRemainingResponses] 服务器 %s 查询成功(第%d个成功),返回 %d 个IP, CNAME=%s (TTL=%d秒): %v\n",
			result.Server, successCount, len(result.IPs), result.CNAME, result.TTL, result.IPs)

		// 收集所有成功的结果
		allSuccessResults = append(allSuccessResults, result)
	}

	// 汇总所有IP地址并去重
	mergedIPs := u.mergeAndDeduplicateIPs(allSuccessResults)

	// 选择最小的TTL(最保守的策略)
	minTTL := fastResponse.TTL
	for _, result := range allSuccessResults {
		if result.TTL < minTTL {
			minTTL = result.TTL
		}
	}

	log.Printf("[collectRemainingResponses] ✅ 后台收集完成: 从 %d 个服务器收集到 %d 个唯一IP (快速响应: %d 个IP, 汇总后: %d 个IP), CNAME=%s, TTL=%d秒\n",
		successCount, len(mergedIPs), len(fastResponse.IPs), len(mergedIPs), fastResponse.CNAME, minTTL)
	log.Printf("[collectRemainingResponses] 完整IP池: %v\n", mergedIPs)

	// 如果设置了缓存更新回调，则调用它来更新缓存
	if u.cacheUpdateCallback != nil {
		log.Printf("[collectRemainingResponses] 📝 调用缓存更新回调，更新完整IP池到缓存\n")
		u.cacheUpdateCallback(domain, qtype, mergedIPs, fastResponse.CNAME, minTTL)
	} else {
		log.Printf("[collectRemainingResponses] ⚠️  警告: 未设置缓存更新回调，无法更新缓存\n")
	}
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

// queryRandom 随机选择上游 DNS 服务器进行查询,带完整容错机制
// 会按随机顺序尝试所有服务器,直到找到一个成功的响应
func (u *Manager) queryRandom(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	// 创建服务器索引列表并随机打乱
	indices := make([]int, len(u.servers))
	for i := range indices {
		indices[i] = i
	}
	rand.Shuffle(len(indices), func(i, j int) {
		indices[i], indices[j] = indices[j], indices[i]
	})

	log.Printf("[queryRandom] 开始随机容错查询 %s (type=%s), 共 %d 个候选服务器\n",
		domain, dns.TypeToString[qtype], len(u.servers))

	var lastResult *QueryResultWithTTL
	var lastErr error
	successCount := 0
	failureCount := 0

	// 按随机顺序尝试所有服务器
	for attemptNum, idx := range indices {
		server := u.servers[idx]

		// 检查上下文是否已超时或取消
		select {
		case <-ctx.Done():
			log.Printf("[queryRandom] ⏱️  上下文已取消/超时,停止尝试 (已尝试 %d/%d 个服务器)\n",
				attemptNum, len(u.servers))
			if lastErr == nil {
				lastErr = ctx.Err()
			}
			return lastResult, lastErr
		default:
		}

		log.Printf("[queryRandom] 第 %d/%d 次尝试: 服务器 %s\n",
			attemptNum+1, len(u.servers), server.Address())

		// 执行查询
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(domain), qtype)

		reply, err := server.Exchange(ctx, msg)

		// 处理查询错误
		if err != nil {
			failureCount++
			lastErr = err
			if u.stats != nil {
				u.stats.IncUpstreamFailure(server.Address())
			}
			log.Printf("[queryRandom] ❌ 第 %d 次尝试失败: %s, 错误: %v\n",
				attemptNum+1, server.Address(), err)
			continue
		}

		// 处理 DNS 响应码
		if reply.Rcode != dns.RcodeSuccess {
			failureCount++
			lastErr = fmt.Errorf("dns query failed: rcode=%d", reply.Rcode)

			// NXDOMAIN 不计入失败统计(这是正常的"域名不存在"响应)
			if reply.Rcode != dns.RcodeNameError {
				if u.stats != nil {
					u.stats.IncUpstreamFailure(server.Address())
				}
				log.Printf("[queryRandom] ❌ 第 %d 次尝试失败: %s, Rcode=%d (%s)\n",
					attemptNum+1, server.Address(), reply.Rcode, dns.RcodeToString[reply.Rcode])
			} else {
				log.Printf("[queryRandom] ℹ️  第 %d 次尝试: %s 返回 NXDOMAIN (域名不存在)\n",
					attemptNum+1, server.Address())
			}
			continue
		}

		// 提取结果
		ips, cname, ttl := extractIPs(reply)

		// 验证结果是否有效
		if len(ips) == 0 && cname == "" {
			failureCount++
			lastErr = fmt.Errorf("empty response: no IPs or CNAME found")
			log.Printf("[queryRandom] ⚠️  第 %d 次尝试: %s 返回空结果\n",
				attemptNum+1, server.Address())
			// 保存这个空结果,但继续尝试其他服务器
			lastResult = &QueryResultWithTTL{IPs: ips, CNAME: cname, TTL: ttl}
			continue
		}

		// 成功!
		successCount++
		if u.stats != nil {
			u.stats.IncUpstreamSuccess(server.Address())
		}

		log.Printf("[queryRandom] ✅ 第 %d 次尝试成功: %s, 返回 %d 个IP, CNAME=%s (TTL=%d秒): %v\n",
			attemptNum+1, server.Address(), len(ips), cname, ttl, ips)

		return &QueryResultWithTTL{IPs: ips, CNAME: cname, TTL: ttl}, nil
	}

	// 所有服务器都失败了
	log.Printf("[queryRandom] ❌ 所有服务器都失败: 成功=%d, 失败=%d, 最后错误: %v\n",
		successCount, failureCount, lastErr)

	// 返回最后一次的结果(即使是空的),这比返回 nil 更友好
	if lastResult != nil {
		log.Printf("[queryRandom] 返回最后一次的结果 (可能为空): %d 个IP, CNAME=%s\n",
			len(lastResult.IPs), lastResult.CNAME)
	}

	return lastResult, lastErr
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
