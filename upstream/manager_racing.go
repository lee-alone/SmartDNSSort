package upstream

import (
	"context"
	"fmt"
	"smartdnssort/logger"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// queryRacing 竞争查询策略：通过微小延迟为第一个服务器争取时间，同时为可靠性保留备选方案
func (u *Manager) queryRacing(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	logger.Debugf("[queryRacing] 开始竞争查询 %s (type=%s)，可用服务器数=%d",
		domain, dns.TypeToString[qtype], len(u.servers))

	// 从 Manager 配置中获取参数
	raceDelay := time.Duration(u.racingDelayMs) * time.Millisecond
	maxConcurrent := u.racingMaxConcurrent

	logger.Debugf("[queryRacing] 竞速参数: 延迟=%v, 最大并发=%d", raceDelay, maxConcurrent)

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
			result := &QueryResultWithTTL{IPs: ips, CNAMEs: cnames, TTL: ttl, AuthenticatedData: reply.AuthenticatedData, DnsMsg: reply.Copy()}
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
			result := &QueryResultWithTTL{IPs: nil, CNAMEs: nil, TTL: ttl, DnsMsg: reply.Copy()}
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
				result := &QueryResultWithTTL{IPs: ips, CNAMEs: cnames, TTL: ttl, AuthenticatedData: reply.AuthenticatedData, DnsMsg: reply.Copy()}
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
				result := &QueryResultWithTTL{IPs: nil, CNAMEs: nil, TTL: ttl, DnsMsg: reply.Copy()}
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
