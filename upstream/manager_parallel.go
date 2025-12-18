package upstream

import (
	"context"
	"fmt"
	"smartdnssort/logger"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// queryParallel 并行查询多个上游 DNS 服务器
// 实现快速响应机制：第一个成功的响应立即返回，后台继续收集其他响应并更新缓存
func (u *Manager) queryParallel(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	logger.Debugf("[queryParallel] 并行查询 %d 个服务器,查询 %s (type=%s),并发数=%d",
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
			if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
				msg.SetEdns0(4096, true)
			}

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
					ips, cnames, ttl := extractIPs(reply)
					result = &QueryResult{
						IPs:               ips,
						CNAMEs:            cnames,
						TTL:               ttl,
						Server:            srv.Address(),
						Rcode:             reply.Rcode,
						AuthenticatedData: reply.AuthenticatedData,
						DnsMsg:            reply.Copy(), // 保存原始DNS消息的副本
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
						logger.Debugf("[queryParallel] 🚀 快速响应: 服务器 %s 第一个返回成功结果，立即响应用户", srv.Address())
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
			logger.Debugf("[queryParallel] ✅ 收到快速响应: 服务器 %s 返回 %d 个IP, CNAMEs=%v (TTL=%d秒): %v",
				fastResponse.Server, len(fastResponse.IPs), fastResponse.CNAMEs, fastResponse.TTL, fastResponse.IPs)
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
		IPs:               fastResponse.IPs,
		CNAMEs:            fastResponse.CNAMEs,
		TTL:               fastResponse.TTL,
		AuthenticatedData: fastResponse.AuthenticatedData,
		DnsMsg:            fastResponse.DnsMsg,
	}, nil
}

// collectRemainingResponses 在后台收集剩余的响应并更新缓存
func (u *Manager) collectRemainingResponses(domain string, qtype uint16, fastResponse *QueryResult, resultChan chan *QueryResult) {
	logger.Debugf("[collectRemainingResponses] 🔄 开始后台收集剩余响应: %s (type=%s)", domain, dns.TypeToString[qtype])

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
			logger.Warnf("[collectRemainingResponses] 服务器 %s 查询失败: %v", result.Server, result.Error)
			continue
		}

		// 记录成功的响应
		successCount++
		if u.stats != nil {
			u.stats.IncUpstreamSuccess(result.Server)
		}
		logger.Debugf("[collectRemainingResponses] 服务器 %s 查询成功(第%d个成功),返回 %d 个IP, CNAMEs=%v (TTL=%d秒): %v",
			result.Server, successCount, len(result.IPs), result.CNAMEs, result.TTL, result.IPs)

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

	logger.Debugf("[collectRemainingResponses] ✅ 后台收集完成: 从 %d 个服务器收集到 %d 个唯一IP (快速响应: %d 个IP, 汇总后: %d 个IP), CNAMEs=%v, TTL=%d秒",
		successCount, len(mergedIPs), len(fastResponse.IPs), len(mergedIPs), fastResponse.CNAMEs, minTTL)
	logger.Debugf("[collectRemainingResponses] 完整IP池: %v", mergedIPs)

	// 如果设置了缓存更新回调，则调用它来更新缓存
	if u.cacheUpdateCallback != nil {
		logger.Debugf("[collectRemainingResponses] 📝 调用缓存更新回调，更新完整IP池到缓存")
		u.cacheUpdateCallback(domain, qtype, mergedIPs, fastResponse.CNAMEs, minTTL)
	} else {
		logger.Warnf("[collectRemainingResponses] ⚠️  警告: 未设置缓存更新回调，无法更新缓存")
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
