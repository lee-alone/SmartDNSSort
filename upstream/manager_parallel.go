package upstream

import (
	"context"
	"fmt"
	"smartdnssort/logger"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// queryParallel 实现了“二阶段分层步进式并行查询”
// 第一阶段（Active Tier）：并发查询最优的 N 个服务器，追求极速响应
// 第二阶段（Staggered Tier）：按节奏（Batch & Delay）启动剩余服务器，追求完整性且不冲击上游
func (u *Manager) queryParallel(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	sortedServers := u.getSortedHealthyServers()
	if len(sortedServers) == 0 {
		return nil, fmt.Errorf("no healthy upstream servers configured")
	}

	logger.Debugf("[queryParallel] 开始分层查询 %d 个服务器: %s (type=%s)", len(sortedServers), domain, dns.TypeToString[qtype])

	// 为这个查询创建唯一的版本号，用于防止旧的后台补全覆盖新的缓存
	queryVersion := time.Now().UnixNano()

	queryStartTime := time.Now()
	resultChan := make(chan *QueryResult, len(sortedServers))
	fastResponseChan := make(chan *QueryResult, 1)

	// queryCtx 用于控制所有上游查询的硬超时（由 totalCollectTimeout 决定）
	queryCtx, cancelAll := context.WithTimeout(context.Background(), u.totalCollectTimeout)
	defer cancelAll()

	var wg sync.WaitGroup
	var fastResponseSent sync.Once

	// 辅助函数：执行具体的服务器查询
	doQuery := func(srv Upstream) {
		defer wg.Done()

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
					Error:  fmt.Errorf("dns error rcode=%d", reply.Rcode),
					Server: srv.Address(),
					Rcode:  reply.Rcode,
				}
			} else {
				records, cnames, ttl := extractRecords(reply)
				var ips []string
				for _, rec := range records {
					switch rr := rec.(type) {
					case *dns.A:
						ips = append(ips, rr.A.String())
					case *dns.AAAA:
						ips = append(ips, rr.AAAA.String())
					}
				}
				result = &QueryResult{
					Records:           records,
					IPs:               ips,
					CNAMEs:            cnames,
					TTL:               ttl,
					Server:            srv.Address(),
					Rcode:             reply.Rcode,
					AuthenticatedData: reply.AuthenticatedData,
					DnsMsg:            reply.Copy(),
				}
			}
		}

		// 收集结果
		select {
		case resultChan <- result:
		case <-queryCtx.Done():
			return
		}

		// 第一个成功的有效响应（带IP或CNAME）触发快速返回
		if result.Error == nil && (len(result.IPs) > 0 || len(result.CNAMEs) > 0) {
			fastResponseSent.Do(func() {
				select {
				case fastResponseChan <- result:
					logger.Debugf("[queryParallel] 🚀 冲锋队成功响应: %s", srv.Address())
				default:
				}
			})
		}
	}

	// 分配梯队
	activeTier := sortedServers
	var backgroundTier []*HealthAwareUpstream
	if len(sortedServers) > u.activeTierSize {
		activeTier = sortedServers[:u.activeTierSize]
		backgroundTier = sortedServers[u.activeTierSize:]
	}

	// --- 启动第一梯队（Active Tier） ---
	for _, srv := range activeTier {
		wg.Add(1)
		go doQuery(srv)
	}

	// 等待信号：或者是收到快速结果，或者是触发了后台补全延迟
	fallbackTimer := time.NewTimer(u.fallbackTimeout)
	defer fallbackTimer.Stop()

	// 启动后台梯队的分组逻辑
	startBackgroundTier := func() {
		if len(backgroundTier) == 0 {
			return
		}
		logger.Debugf("[queryParallel] 🔄 启动第二阶段后台补全，剩余服务器数: %d", len(backgroundTier))
		go func() {
			for i := 0; i < len(backgroundTier); i += u.batchSize {
				end := i + u.batchSize
				if end > len(backgroundTier) {
					end = len(backgroundTier)
				}

				// 启动当前批次
				for _, srv := range backgroundTier[i:end] {
					wg.Add(1)
					go doQuery(srv)
				}

				// 每批次之间按照比例或固定时间延迟
				if end < len(backgroundTier) {
					select {
					case <-time.After(u.staggerDelay):
					case <-queryCtx.Done():
						return
					}
				}
			}
		}()
	}

	// 监听逻辑：决定何时开启后台补全
	var fastResponse *QueryResult
	select {
	case fr := <-fastResponseChan:
		fastResponse = fr
		// 拿到最快结果后，依然要启动后台补全以保证“完整性”
		go startBackgroundTier()
	case <-fallbackTimer.C:
		// 冲锋队慢了，主动开启补全
		startBackgroundTier()
		// 继续等待直到拿到第一个结果或 ctx 超时
		select {
		case fr := <-fastResponseChan:
			fastResponse = fr
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-queryCtx.Done():
			// 如果连后台总超时都到了还是没结果
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// 如果最终仍然没有成功结果，等待所有请求结束看是否有错误
	if fastResponse == nil {
		go func() {
			wg.Wait()
			close(resultChan)
			close(fastResponseChan)
		}()

		var firstError error
		for res := range resultChan {
			if res.Error != nil && firstError == nil {
				firstError = res.Error
			}
		}
		if firstError != nil {
			return nil, firstError
		}
		return nil, fmt.Errorf("all parallel tiers failed to provide valid response")
	}

	// 记录性能数据
	u.RecordQueryLatency(time.Since(queryStartTime))
	if u.stats != nil {
		u.stats.IncUpstreamSuccess(fastResponse.Server)
	}

	// 启动结果汇总逻辑
	go u.collectRemainingResponses(domain, qtype, queryVersion, fastResponse, resultChan, &wg)

	// 构造返回对象
	return &QueryResultWithTTL{
		Records:           fastResponse.Records,
		IPs:               fastResponse.IPs,
		CNAMEs:            fastResponse.CNAMEs,
		TTL:               fastResponse.TTL,
		AuthenticatedData: fastResponse.AuthenticatedData,
		DnsMsg:            fastResponse.DnsMsg,
	}, nil
}

// collectRemainingResponses 负责在后台静默收集所有结果并更新缓存
// queryVersion 用于防止旧的后台补全覆盖新的缓存
func (u *Manager) collectRemainingResponses(domain string, qtype uint16, queryVersion int64, fastResponse *QueryResult, resultChan chan *QueryResult, wg *sync.WaitGroup) {
	// 等待所有在途请求完成（或者 queryCtx 到期）
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	allSuccessResults := []*QueryResult{fastResponse}

	// 在本函数独立的超时控制内收集
	timeout := time.After(u.totalCollectTimeout)

loop:
	for {
		select {
		case res, ok := <-resultChan:
			if !ok {
				break loop
			}
			if res.Error == nil && res != fastResponse {
				allSuccessResults = append(allSuccessResults, res)
				if u.stats != nil {
					u.stats.IncUpstreamSuccess(res.Server)
				}
			} else if res.Error != nil && u.stats != nil {
				if res.Rcode != dns.RcodeNameError {
					u.stats.IncUpstreamFailure(res.Server)
				}
			}
		case <-timeout:
			logger.Warnf("[collectRemainingResponses] 补全任务硬超时退出: %s (version=%d)", domain, queryVersion)
			break loop
		}
	}

	if len(allSuccessResults) <= 1 {
		return // 没有更多结果需要合并
	}

	mergedRecords := u.mergeAndDeduplicateRecords(allSuccessResults)

	// 选取最小 TTL
	minTTL := fastResponse.TTL
	for _, res := range allSuccessResults {
		if res.TTL < minTTL {
			minTTL = res.TTL
		}
	}

	if u.cacheUpdateCallback != nil {
		logger.Debugf("[collectRemainingResponses] ✅ 汇总完成，从 %d 个结果中更新全量 IP 池 (version=%d)", len(allSuccessResults), queryVersion)
		u.cacheUpdateCallback(domain, qtype, mergedRecords, fastResponse.CNAMEs, minTTL, queryVersion)
	}
}

// mergeAndDeduplicateRecords 合并并去重多个查询结果中的记录
// 策略：
// 1. IP记录（A/AAAA）：基于IP地址去重
// 2. CNAME记录：基于Target去重
// 3. 其他记录：仅保留第一个收到的记录，避免完全重复
func (u *Manager) mergeAndDeduplicateRecords(results []*QueryResult) []dns.RR {
	ipSet := make(map[string]bool)
	cnameSet := make(map[string]bool)
	otherRecordSet := make(map[string]bool)
	var mergedRecords []dns.RR

	for _, result := range results {
		for _, rr := range result.Records {
			switch rec := rr.(type) {
			case *dns.A:
				ipStr := rec.A.String()
				if !ipSet[ipStr] {
					ipSet[ipStr] = true
					mergedRecords = append(mergedRecords, rr)
				}
			case *dns.AAAA:
				ipStr := rec.AAAA.String()
				if !ipSet[ipStr] {
					ipSet[ipStr] = true
					mergedRecords = append(mergedRecords, rr)
				}
			case *dns.CNAME:
				cnameStr := rec.Target
				if !cnameSet[cnameStr] {
					cnameSet[cnameStr] = true
					mergedRecords = append(mergedRecords, rr)
				}
			default:
				// 其他记录（SOA、NS等）：仅保留第一个收到的记录
				recordKey := rr.String()
				if !otherRecordSet[recordKey] {
					otherRecordSet[recordKey] = true
					mergedRecords = append(mergedRecords, rr)
				}
			}
		}
	}

	return mergedRecords
}
