package upstream

import (
	"context"
	"fmt"
	"math/rand"
	"smartdnssort/logger"
	"time"

	"github.com/miekg/dns"
)

// queryRandom 随机选择上游 DNS 服务器进行查询,带完整容错机制
// 会按随机顺序尝试所有服务器,直到找到一个成功的响应
func (u *Manager) queryRandom(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	// 记录查询开始时间，用于计算延迟
	queryStartTime := time.Now()

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
			server.RecordError()
			logger.Warnf("[queryRandom] ❌ 第 %d 次尝试失败: %s, 错误: %v",
				attemptNum+1, server.Address(), err)
			continue
		}

		// 处理 NXDOMAIN - 域名不存在，直接返回
		if reply.Rcode == dns.RcodeNameError {
			// 从 SOA 记录中提取 TTL，或使用默认值
			ttl := extractNegativeTTL(reply)
			logger.Debugf("[queryRandom] ℹ️  第 %d 次尝试: %s 返回 NXDOMAIN (域名不存在), TTL=%d秒",
				attemptNum+1, server.Address(), ttl)
			server.RecordSuccess()
			queryLatency := time.Since(queryStartTime)
			u.RecordQueryLatency(queryLatency)
			return &QueryResultWithTTL{Records: nil, IPs: nil, CNAMEs: nil, TTL: ttl, DnsMsg: reply.Copy()}, nil
		}

		// 处理其他 DNS 错误响应码
		if reply.Rcode != dns.RcodeSuccess {
			failureCount++
			lastErr = fmt.Errorf("dns query failed: rcode=%d", reply.Rcode)
			server.RecordError()
			logger.Warnf("[queryRandom] ❌ 第 %d 次尝试失败: %s, Rcode=%d (%s)",
				attemptNum+1, server.Address(), reply.Rcode, dns.RcodeToString[reply.Rcode])
			continue
		}

		// 提取结果
		records, cnames, ttl := extractRecords(reply)

		// 从 records 中提取 IPs
		var ips []string
		for _, r := range records {
			switch rec := r.(type) {
			case *dns.A:
				ips = append(ips, rec.A.String())
			case *dns.AAAA:
				ips = append(ips, rec.AAAA.String())
			}
		}

		// 验证结果是否有效
		if len(records) == 0 {
			failureCount++
			lastErr = fmt.Errorf("empty response: no records found")
			server.RecordError()
			logger.Warnf("[queryRandom] ⚠️  第 %d 次尝试: %s 返回空结果",
				attemptNum+1, server.Address())
			// 保存这个空结果,但继续尝试其他服务器
			lastResult = &QueryResultWithTTL{Records: records, IPs: ips, CNAMEs: cnames, TTL: ttl, DnsMsg: reply.Copy()}
			continue
		}

		// 成功!
		successCount++
		logger.Debugf("[queryRandom] ✅ 第 %d 次尝试成功: %s, 返回 %d 条记录, CNAMEs=%v (TTL=%d秒)",
			attemptNum+1, server.Address(), len(records), cnames, ttl)

		server.RecordSuccess()
		queryLatency := time.Since(queryStartTime)
		u.RecordQueryLatency(queryLatency)

		return &QueryResultWithTTL{Records: records, IPs: ips, CNAMEs: cnames, TTL: ttl, AuthenticatedData: reply.AuthenticatedData, DnsMsg: reply.Copy()}, nil
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
