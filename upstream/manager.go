package upstream

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"smartdnssort/logger"
	"smartdnssort/stats"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// QueryResult 查询结果
type QueryResult struct {
	IPs               []string
	CNAMEs            []string // 支持多 CNAME 记录
	TTL               uint32   // 上游 DNS 返回的 TTL（对所有 IP 取最小值）
	Error             error
	Server            string // 添加服务器字段
	Rcode             int    // DNS 响应代码
	AuthenticatedData bool   // DNSSEC 验证标记 (AD flag)
}

// QueryResultWithTTL 带 TTL 信息的查询结果
type QueryResultWithTTL struct {
	IPs               []string
	CNAMEs            []string // 支持多 CNAME 记录
	TTL               uint32   // 上游 DNS 返回的 TTL
	AuthenticatedData bool     // DNSSEC 验证标记 (AD flag)
}

// Manager 上游 DNS 查询管理器
type Manager struct {
	servers     []*HealthAwareUpstream // 带健康检查的上游服务器列表
	strategy    string                 // parallel, random
	timeoutMs   int
	concurrency int // 并行查询时的并发数
	stats       *stats.Stats
	// 缓存更新回调函数，用于在 parallel 模式下后台收集完所有响应后更新缓存
	// 缓存更新回调函数，用于在 parallel 模式下后台收集完所有响应后更新缓存
	cacheUpdateCallback func(domain string, qtype uint16, ips []string, cnames []string, ttl uint32)
}

// NewManager 创建上游 DNS 管理器
func NewManager(servers []Upstream, strategy string, timeoutMs int, concurrency int, s *stats.Stats, healthConfig *HealthCheckConfig) *Manager {
	if strategy == "" {
		strategy = "random"
	}
	if timeoutMs <= 0 {
		timeoutMs = 300
	}
	if concurrency <= 0 {
		concurrency = 3
	}

	// 将普通 Upstream 包装为 HealthAwareUpstream
	healthAwareServers := make([]*HealthAwareUpstream, len(servers))
	for i, server := range servers {
		healthAwareServers[i] = NewHealthAwareUpstream(server, healthConfig)
	}

	return &Manager{
		servers:     healthAwareServers,
		strategy:    strategy,
		timeoutMs:   timeoutMs,
		concurrency: concurrency,
		stats:       s,
	}
}

// SetCacheUpdateCallback 设置缓存更新回调函数
// 用于在 parallel 模式下后台收集完所有响应后更新缓存
// SetCacheUpdateCallback 设置缓存更新回调函数
// 用于在 parallel 模式下后台收集完所有响应后更新缓存
func (u *Manager) SetCacheUpdateCallback(callback func(domain string, qtype uint16, ips []string, cnames []string, ttl uint32)) {
	u.cacheUpdateCallback = callback
}

// GetServers 返回所有上游服务器列表
func (u *Manager) GetServers() []Upstream {
	result := make([]Upstream, len(u.servers))
	for i, server := range u.servers {
		result[i] = server
	}
	return result
}

// GetHealthyServerCount 返回当前健康的服务器数量
// 用于计算动态超时时间
func (u *Manager) GetHealthyServerCount() int {
	count := 0
	for _, server := range u.servers {
		if !server.ShouldSkipTemporarily() {
			count++
		}
	}
	return count
}

// GetTotalServerCount 返回总服务器数量
func (u *Manager) GetTotalServerCount() int {
	return len(u.servers)
}

// Query 查询域名，返回 IP 列表和 TTL
func (u *Manager) Query(ctx context.Context, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	if len(r.Question) == 0 {
		return nil, errors.New("query message has no questions")
	}
	question := r.Question[0]
	domain := strings.TrimRight(question.Name, ".")
	qtype := question.Qtype

	switch u.strategy {
	case "parallel":
		return u.queryParallel(ctx, domain, qtype, r, dnssec)
	case "sequential":
		return u.querySequential(ctx, domain, qtype, r, dnssec)
	case "racing":
		return u.queryRacing(ctx, domain, qtype, r, dnssec)
	default:
		return u.queryRandom(ctx, domain, qtype, r, dnssec)
	}
}

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

// queryRandom 随机选择上游 DNS 服务器进行查询,带完整容错机制
// 会按随机顺序尝试所有服务器,直到找到一个成功的响应
func (u *Manager) queryRandom(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
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

	logger.Debugf("[queryRandom] 开始随机容错查询 %s (type=%s), 共 %d 个候选服务器",
		domain, dns.TypeToString[qtype], len(u.servers))

	var lastResult *QueryResultWithTTL
	var lastErr error
	successCount := 0
	failureCount := 0

	// 按随机顺序尝试所有服务器
	for attemptNum, idx := range indices {
		server := u.servers[idx]

		// 健康检查：跳过临时不可用的服务器（熔断状态）
		if server.ShouldSkipTemporarily() {
			logger.Warnf("[queryRandom] ⚠️  跳过临时不可用的服务器: %s (熔断状态)",
				server.Address())
			continue
		}

		// 检查上下文是否已超时或取消
		select {
		case <-ctx.Done():
			logger.Warnf("[queryRandom] ⏱️  上下文已取消/超时,停止尝试 (已尝试 %d/%d 个服务器)",
				attemptNum, len(u.servers))
			if lastErr == nil {
				lastErr = ctx.Err()
			}
			return lastResult, lastErr
		default:
		}

		logger.Debugf("[queryRandom] 第 %d/%d 次尝试: 服务器 %s",
			attemptNum+1, len(u.servers), server.Address())

		// 为单个服务器查询创建独立的超时上下文
		queryCtx, cancel := context.WithTimeout(ctx, time.Duration(u.timeoutMs)*time.Millisecond)

		// 执行查询
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(domain), qtype)
		if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
			msg.SetEdns0(4096, true)
		}

		reply, err := server.Exchange(queryCtx, msg)
		cancel() // 立即释放资源

		// 处理查询错误
		if err != nil {
			failureCount++
			lastErr = err
			if u.stats != nil {
				u.stats.IncUpstreamFailure(server.Address())
			}
			logger.Warnf("[queryRandom] ❌ 第 %d 次尝试失败: %s, 错误: %v",
				attemptNum+1, server.Address(), err)
			continue
		}

		// 处理 NXDOMAIN - 域名不存在，直接返回
		if reply.Rcode == dns.RcodeNameError {
			// 从 SOA 记录中提取 TTL，或使用默认值
			ttl := extractNegativeTTL(reply)
			if u.stats != nil {
				u.stats.IncUpstreamSuccess(server.Address())
			}
			logger.Debugf("[queryRandom] ℹ️  第 %d 次尝试: %s 返回 NXDOMAIN (域名不存在), TTL=%d秒",
				attemptNum+1, server.Address(), ttl)
			return &QueryResultWithTTL{IPs: nil, CNAMEs: nil, TTL: ttl}, nil
		}

		// 处理其他 DNS 错误响应码
		if reply.Rcode != dns.RcodeSuccess {
			failureCount++
			lastErr = fmt.Errorf("dns query failed: rcode=%d", reply.Rcode)
			if u.stats != nil {
				u.stats.IncUpstreamFailure(server.Address())
			}
			logger.Warnf("[queryRandom] ❌ 第 %d 次尝试失败: %s, Rcode=%d (%s)",
				attemptNum+1, server.Address(), reply.Rcode, dns.RcodeToString[reply.Rcode])
			continue
		}

		// 提取结果
		ips, cnames, ttl := extractIPs(reply)

		// 验证结果是否有效
		if len(ips) == 0 && len(cnames) == 0 {
			failureCount++
			lastErr = fmt.Errorf("empty response: no IPs or CNAME found")
			logger.Warnf("[queryRandom] ⚠️  第 %d 次尝试: %s 返回空结果",
				attemptNum+1, server.Address())
			// 保存这个空结果,但继续尝试其他服务器
			lastResult = &QueryResultWithTTL{IPs: ips, CNAMEs: cnames, TTL: ttl}
			continue
		}

		// 成功!
		successCount++
		if u.stats != nil {
			u.stats.IncUpstreamSuccess(server.Address())
		}

		logger.Debugf("[queryRandom] ✅ 第 %d 次尝试成功: %s, 返回 %d 个IP, CNAMEs=%v (TTL=%d秒): %v",
			attemptNum+1, server.Address(), len(ips), cnames, ttl, ips)

		return &QueryResultWithTTL{IPs: ips, CNAMEs: cnames, TTL: ttl, AuthenticatedData: reply.AuthenticatedData}, nil
	}

	// 所有服务器都失败了
	logger.Errorf("[queryRandom] ❌ 所有服务器都失败: 成功=%d, 失败=%d, 最后错误: %v",
		successCount, failureCount, lastErr)

	// 返回最后一次的结果(即使是空的),这比返回 nil 更友好
	if lastResult != nil {
		logger.Warnf("[queryRandom] 返回最后一次的结果 (可能为空): %d 个IP, CNAMEs=%v",
			len(lastResult.IPs), lastResult.CNAMEs)
	}

	return lastResult, lastErr
}

// extractIPs 从 DNS 响应中提取 IP 地址、CNAMEs 和最小 TTL
// 返回值：IP 列表、CNAME 列表、最小 TTL（秒）
func extractIPs(msg *dns.Msg) ([]string, []string, uint32) {
	var ips []string
	var cnames []string
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
			cnames = append(cnames, rr.Target)
			if minTTL == 0 || rr.Hdr.Ttl < minTTL {
				minTTL = rr.Hdr.Ttl
			}
		}
	}

	// 如果没有找到任何记录，使用默认 TTL（60 秒）
	if minTTL == 0 {
		minTTL = 60
	}

	return ips, cnames, minTTL
}

// extractNegativeTTL 从 NXDOMAIN 响应的 SOA 记录中提取否定缓存 TTL
// 返回值：TTL（秒）
func extractNegativeTTL(msg *dns.Msg) uint32 {
	// 尝试从 Ns (Authority) 部分提取 SOA 记录的 TTL
	for _, ns := range msg.Ns {
		if soa, ok := ns.(*dns.SOA); ok {
			// SOA 记录的 Minimum 字段表示否定缓存的 TTL
			// 同时也要考虑 SOA 记录本身的 TTL
			ttl := soa.Hdr.Ttl
			if soa.Minttl < ttl {
				ttl = soa.Minttl
			}
			return ttl
		}
	}

	// 如果没有找到 SOA 记录，使用默认的否定缓存 TTL（300 秒 = 5 分钟）
	return 300
}

// querySequential 顺序查询策略：从健康度最好的服务器开始依次尝试
func (u *Manager) querySequential(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	logger.Debugf("[querySequential] 开始顺序查询 %s (type=%s)，可用服务器数=%d",
		domain, dns.TypeToString[qtype], len(u.servers))

	// 获取单次尝试的超时时间（默认 300ms）
	attemptTimeout := time.Duration(u.timeoutMs) * time.Millisecond
	if u.timeoutMs <= 0 {
		attemptTimeout = 300 * time.Millisecond
	}

	var primaryError error
	var lastDNSError error

	// 按健康度排序服务器（优先使用健康度最好的）
	sortedServers := u.getSortedHealthyServers()
	if len(sortedServers) == 0 {
		sortedServers = u.servers // 降级使用全部服务器
	}

	for i, server := range sortedServers {
		// 检查总体上下文是否已超时
		select {
		case <-ctx.Done():
			logger.Warnf("[querySequential] 总体超时，停止尝试 (已尝试 %d/%d 个服务器)",
				i, len(sortedServers))
			if primaryError == nil {
				primaryError = ctx.Err()
			}
			if lastDNSError != nil {
				return nil, lastDNSError
			}
			return nil, primaryError
		default:
		}

		// 跳过临时不可用的服务器
		if server.ShouldSkipTemporarily() {
			logger.Debugf("[querySequential] 跳过熔断状态的服务器: %s", server.Address())
			continue
		}

		logger.Debugf("[querySequential] 第 %d 次尝试: %s，超时=%v", i+1, server.Address(), attemptTimeout)

		// 为本次尝试创建短超时的上下文
		attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)

		// 执行查询
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(domain), qtype)
		if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
			msg.SetEdns0(4096, true)
		}

		reply, err := server.Exchange(attemptCtx, msg)
		cancel() // 立即释放资源

		// 处理查询错误
		if err != nil {
			if primaryError == nil {
				primaryError = err
			}

			// 区分错误类型
			if errors.Is(err, context.DeadlineExceeded) {
				// 网络超时（疑似丢包或服务器响应慢）
				logger.Debugf("[querySequential] 服务器 %s 超时，尝试下一个", server.Address())
				server.RecordTimeout()
				if u.stats != nil {
					u.stats.IncUpstreamFailure(server.Address())
				}
				continue
			} else {
				// 网络层错误，记录并继续
				logger.Debugf("[querySequential] 服务器 %s 错误: %v，尝试下一个", server.Address(), err)
				server.RecordError()
				if u.stats != nil {
					u.stats.IncUpstreamFailure(server.Address())
				}
				continue
			}
		}

		// 处理 NXDOMAIN - 这是确定性错误，直接返回
		if reply.Rcode == dns.RcodeNameError {
			ttl := extractNegativeTTL(reply)
			if u.stats != nil {
				u.stats.IncUpstreamSuccess(server.Address())
			}
			logger.Debugf("[querySequential] 服务器 %s 返回 NXDOMAIN，立即返回", server.Address())
			server.RecordSuccess()
			return &QueryResultWithTTL{IPs: nil, CNAMEs: nil, TTL: ttl}, nil
		}

		// 处理其他 DNS 错误响应码
		if reply.Rcode != dns.RcodeSuccess {
			lastDNSError = fmt.Errorf("dns query failed: rcode=%d", reply.Rcode)
			logger.Debugf("[querySequential] 服务器 %s 返回错误码 %d，尝试下一个",
				server.Address(), reply.Rcode)
			server.RecordError()
			if u.stats != nil {
				u.stats.IncUpstreamFailure(server.Address())
			}
			continue
		}

		// 提取结果
		ips, cnames, ttl := extractIPs(reply)

		// 验证结果
		if len(ips) == 0 && len(cnames) == 0 {
			logger.Debugf("[querySequential] 服务器 %s 返回空结果，尝试下一个",
				server.Address())
			server.RecordError()
			if u.stats != nil {
				u.stats.IncUpstreamFailure(server.Address())
			}
			continue
		}

		// 成功!
		if u.stats != nil {
			u.stats.IncUpstreamSuccess(server.Address())
		}
		logger.Debugf("[querySequential] ✅ 服务器 %s 成功，返回 %d 个IP: %v",
			server.Address(), len(ips), ips)
		server.RecordSuccess()

		return &QueryResultWithTTL{IPs: ips, CNAMEs: cnames, TTL: ttl, AuthenticatedData: reply.AuthenticatedData}, nil
	}

	// 所有服务器都尝试失败
	logger.Errorf("[querySequential] 所有服务器都失败")
	if lastDNSError != nil {
		return nil, lastDNSError
	}
	if primaryError != nil {
		return nil, primaryError
	}
	return nil, fmt.Errorf("all upstream servers failed")
}

// queryRacing 竞争查询策略：通过微小延迟为第一个服务器争取时间，同时为可靠性保留备选方案
func (u *Manager) queryRacing(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	logger.Debugf("[queryRacing] 开始竞争查询 %s (type=%s)，可用服务器数=%d",
		domain, dns.TypeToString[qtype], len(u.servers))

	// 获取参数
	raceDelay := time.Duration(100) * time.Millisecond // 默认 100ms
	maxConcurrent := 2                                 // 默认 2

	// 从配置中获取参数（如果在 Manager 结构体中添加了这些字段）
	// 这里假设会在后续的改进中添加

	sortedServers := u.getSortedHealthyServers()
	if len(sortedServers) == 0 {
		sortedServers = u.servers // 降级使用全部服务器
	}

	if len(sortedServers) > maxConcurrent {
		sortedServers = sortedServers[:maxConcurrent]
	}

	// 创建用于接收结果的通道
	resultChan := make(chan *QueryResultWithTTL, 1)
	errorChan := make(chan error, maxConcurrent)

	// 创建可取消的上下文
	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var activeTasks int
	var mu sync.Mutex

	// 1. 立即向最佳的上游服务器发起查询
	activeTasks = 1
	go func(server *HealthAwareUpstream, index int) {
		logger.Debugf("[queryRacing] 主请求发起: 服务器 %d (%s)", index, server.Address())
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(domain), dns.StringToType[dns.TypeToString[qtype]])
		if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
			msg.SetEdns0(4096, true)
		}

		reply, err := server.Exchange(raceCtx, msg)

		if err != nil {
			if u.stats != nil {
				u.stats.IncUpstreamFailure(server.Address())
			}
			select {
			case errorChan <- err:
			case <-raceCtx.Done():
			}
			return
		}

		// 处理查询成功
		if reply.Rcode == dns.RcodeSuccess {
			ips, cnames, ttl := extractIPs(reply)
			result := &QueryResultWithTTL{IPs: ips, CNAMEs: cnames, TTL: ttl, AuthenticatedData: reply.AuthenticatedData}
			select {
			case resultChan <- result:
				logger.Debugf("[queryRacing] 主请求成功: %s", server.Address())
				server.RecordSuccess()
				if u.stats != nil {
					u.stats.IncUpstreamSuccess(server.Address())
				}
			case <-raceCtx.Done():
			}
			return
		}

		// 处理 NXDOMAIN - 确定性错误，立即返回
		if reply.Rcode == dns.RcodeNameError {
			ttl := extractNegativeTTL(reply)
			result := &QueryResultWithTTL{IPs: nil, CNAMEs: nil, TTL: ttl}
			select {
			case resultChan <- result:
				server.RecordSuccess()
				if u.stats != nil {
					u.stats.IncUpstreamSuccess(server.Address())
				}
			case <-raceCtx.Done():
			}
			return
		}

		// 其他错误
		err = fmt.Errorf("dns query failed: rcode=%d", reply.Rcode)
		select {
		case errorChan <- err:
		case <-raceCtx.Done():
		}
		server.RecordError()
		if u.stats != nil {
			u.stats.IncUpstreamFailure(server.Address())
		}
	}(sortedServers[0], 0)

	// 2. 设置延迟计时器
	timer := time.NewTimer(raceDelay)

	select {
	case result := <-resultChan:
		// 主请求在延迟内返回了结果
		timer.Stop()
		logger.Debugf("[queryRacing] 主请求在延迟内返回结果")
		return result, nil

	case err := <-errorChan:
		// 主请求在延迟内返回了错误
		if isDNSError(err) && isDNSNXDomain(err) {
			// NXDOMAIN 是确定性错误，直接返回
			timer.Stop()
			return nil, err
		}
		// 其他错误，记录但继续等待备选方案
		logger.Debugf("[queryRacing] 主请求出错，等待备选方案")

	case <-timer.C:
		// 延迟超时，主请求尚未返回，立即发起竞争请求
		logger.Debugf("[queryRacing] 主请求延迟超时，发起备选竞争请求")

	case <-raceCtx.Done():
		// 总查询超时
		timer.Stop()
		return nil, raceCtx.Err()
	}

	// 3. 延迟后，发起备选竞争请求
	for i := 1; i < len(sortedServers) && i < maxConcurrent; i++ {
		mu.Lock()
		if activeTasks >= maxConcurrent {
			mu.Unlock()
			break
		}
		activeTasks++
		mu.Unlock()

		idx := i
		go func(server *HealthAwareUpstream, index int) {
			logger.Debugf("[queryRacing] 备选请求发起: 服务器 %d (%s)", index, server.Address())
			msg := new(dns.Msg)
			msg.SetQuestion(dns.Fqdn(domain), dns.StringToType[dns.TypeToString[qtype]])
			if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
				msg.SetEdns0(4096, true)
			}

			reply, err := server.Exchange(raceCtx, msg)

			if err != nil {
				if u.stats != nil {
					u.stats.IncUpstreamFailure(server.Address())
				}
				select {
				case errorChan <- err:
				case <-raceCtx.Done():
				}
				return
			}

			if reply.Rcode == dns.RcodeSuccess {
				ips, cnames, ttl := extractIPs(reply)
				result := &QueryResultWithTTL{IPs: ips, CNAMEs: cnames, TTL: ttl, AuthenticatedData: reply.AuthenticatedData}
				select {
				case resultChan <- result:
					logger.Debugf("[queryRacing] 备选请求成功: %s", server.Address())
					server.RecordSuccess()
					if u.stats != nil {
						u.stats.IncUpstreamSuccess(server.Address())
					}
				default:
				}
				return
			}

			if reply.Rcode == dns.RcodeNameError {
				ttl := extractNegativeTTL(reply)
				result := &QueryResultWithTTL{IPs: nil, CNAMEs: nil, TTL: ttl}
				select {
				case resultChan <- result:
					server.RecordSuccess()
					if u.stats != nil {
						u.stats.IncUpstreamSuccess(server.Address())
					}
				default:
				}
				return
			}

			err = fmt.Errorf("dns query failed: rcode=%d", reply.Rcode)
			select {
			case errorChan <- err:
			case <-raceCtx.Done():
			}
			server.RecordError()
			if u.stats != nil {
				u.stats.IncUpstreamFailure(server.Address())
			}
		}(sortedServers[idx], idx)
	}

	// 4. 等待最先到达的有效结果，或所有请求都失败
	successCount := 0
	errCount := 0
	var lastErr error

	for successCount == 0 && errCount < activeTasks {
		select {
		case result := <-resultChan:
			// 收到了一个有效结果
			logger.Debugf("[queryRacing] ✅ 收到结果")
			return result, nil

		case err := <-errorChan:
			errCount++
			lastErr = err

			// 检查是否是确定性错误
			if isDNSError(err) && isDNSNXDomain(err) {
				logger.Debugf("[queryRacing] 得到 NXDOMAIN，立即返回")
				return nil, err
			}

			logger.Debugf("[queryRacing] 备选错误 %d/%d: %v", errCount, activeTasks, err)
			// 继续等待其他请求

		case <-raceCtx.Done():
			// 总查询超时
			logger.Debugf("[queryRacing] 总体超时")
			return nil, raceCtx.Err()
		}
	}

	// 所有任务都返回了错误
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("racing query failed: all upstream servers returned errors")
}

// getSortedHealthyServers 按健康度排序服务器
func (u *Manager) getSortedHealthyServers() []*HealthAwareUpstream {
	// 简单实现：优先使用未熔断的服务器，然后按健康度排序
	// 更复杂的实现可以基于响应时间、成功率等因素
	healthy := make([]*HealthAwareUpstream, 0, len(u.servers))
	unhealthy := make([]*HealthAwareUpstream, 0)

	for _, server := range u.servers {
		if !server.ShouldSkipTemporarily() {
			healthy = append(healthy, server)
		} else {
			unhealthy = append(unhealthy, server)
		}
	}

	// 健康的服务器优先，然后是不健康的
	return append(healthy, unhealthy...)
}

// isDNSError 检查是否是 DNS 错误
func isDNSError(err error) bool {
	if err == nil {
		return false
	}
	// 简单的检查：DNS 错误通常包含 "dns" 字样或是特定的 DNS 库错误类型
	return strings.Contains(err.Error(), "dns") || strings.Contains(err.Error(), "rcode")
}

// isDNSNXDomain 检查是否是 NXDOMAIN 错误
func isDNSNXDomain(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "rcode=3") || strings.Contains(err.Error(), "NXDOMAIN")
}
