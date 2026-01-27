package upstream

import (
	"context"
	"errors"
	"fmt"
	"smartdnssort/logger"
	"time"

	"github.com/miekg/dns"
)

// querySequential 顺序查询策略：从健康度最好的服务器开始依次尝试
func (u *Manager) querySequential(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	logger.Debugf("[querySequential] 开始顺序查询 %s (type=%s)，可用服务器数=%d",
		domain, dns.TypeToString[qtype], len(u.servers))

	// 记录查询开始时间，用于计算延迟
	queryStartTime := time.Now()

	// 获取自适应单次超时时间
	attemptTimeout := u.GetAdaptiveSequentialTimeout()

	logger.Debugf("[querySequential] 使用自适应超时: %v", attemptTimeout)

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
			return &QueryResultWithTTL{Records: nil, IPs: nil, CNAMEs: nil, TTL: ttl, DnsMsg: reply.Copy()}, nil
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

		// 验证结果
		if len(records) == 0 {
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
		logger.Debugf("[querySequential] ✅ 服务器 %s 成功，返回 %d 条记录",
			server.Address(), len(records))
		server.RecordSuccess()

		// 记录查询延迟，用于动态参数优化
		queryLatency := time.Since(queryStartTime)
		u.RecordQueryLatency(queryLatency)
		logger.Debugf("[querySequential] 记录查询延迟: %v (用于动态参数优化)", queryLatency)

		return &QueryResultWithTTL{Records: records, IPs: ips, CNAMEs: cnames, TTL: ttl, AuthenticatedData: reply.AuthenticatedData, DnsMsg: reply.Copy()}, nil
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
