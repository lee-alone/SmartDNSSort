package upstream

import (
	"context"
	"fmt"
	"net"
	"smartdnssort/logger"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

// RacingStats 竞速策略的统计信息
type RacingStats struct {
	mu sync.RWMutex

	// 总查询数
	totalQueries int64
	// 成功查询数
	successQueries int64
	// 错误抢跑触发次数
	earlyTriggerCount int64
	// 错误抢跑节省的总时间
	earlyTriggerTimeSaved time.Duration
}

// queryRacing 竞争查询策略：通过微小延迟为第一个服务器争取时间，
// 并根据网络方差动态调整延迟，支持"错误抢跑"和"分梯队启动"以平衡性能与资源。
func (u *Manager) queryRacing(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	sortedServers := u.getSortedHealthyServers()
	if len(sortedServers) == 0 {
		return nil, fmt.Errorf("no healthy upstream servers configured")
	}

	// 1. 获取核心参数
	raceDelay := u.GetAdaptiveRacingDelay()
	stdDev := u.GetLatencyStdDev()
	maxConcurrent := u.racingMaxConcurrent
	queryStartTime := time.Now()

	logger.Debugf("[queryRacing] 开始竞争查询: %s (延迟=%v, 标准差=%v, 最大并发=%d)",
		domain, raceDelay, stdDev, maxConcurrent)

	// 2. 通道与状态控制
	resultChan := make(chan *QueryResultWithTTL, 1)
	errorChan := make(chan error, maxConcurrent)
	cancelDelayChan := make(chan struct{}) // 用于"错误抢跑"：一旦主服务器报错，立即开始后续梯队
	earlyTriggerOnce := sync.Once{}        // 确保只关闭一次 cancelDelayChan

	raceCtx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()

	var wg sync.WaitGroup
	var once sync.Once
	var earlyTriggerCount atomic.Int32 // 统计错误抢跑触发次数

	// 辅助逻辑：执行单个查询
	doQuery := func(srv *HealthAwareUpstream, isPrimary bool) {
		defer wg.Done()

		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(domain), qtype)
		if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
			msg.SetEdns0(4096, true)
		}

		reply, err := srv.Exchange(raceCtx, msg)
		if err != nil {
			if isPrimary && isNetworkError(err) {
				// 主请求报网络错误，立即触发抢跑
				earlyTriggerOnce.Do(func() {
					close(cancelDelayChan)
					earlyTriggerCount.Add(1)
					logger.Debugf("[queryRacing] 主请求网络错误，触发错误抢跑: %v", err)
				})
			}
			select {
			case errorChan <- err:
			case <-raceCtx.Done():
			}
			return
		}

		// 只有成功结果（或确定性 NXDOMAIN）才触发返回
		if reply.Rcode == dns.RcodeSuccess || reply.Rcode == dns.RcodeNameError {
			var records []dns.RR
			var cnames []string
			var ttl uint32
			var ips []string

			if reply.Rcode == dns.RcodeSuccess {
				records, cnames, ttl = extractRecords(reply)
				for _, rec := range records {
					switch rr := rec.(type) {
					case *dns.A:
						ips = append(ips, rr.A.String())
					case *dns.AAAA:
						ips = append(ips, rr.AAAA.String())
					}
				}
			} else {
				ttl = extractNegativeTTL(reply)
			}

			result := &QueryResultWithTTL{
				Records:           records,
				IPs:               ips,
				CNAMEs:            cnames,
				TTL:               ttl,
				AuthenticatedData: reply.AuthenticatedData,
				DnsMsg:            reply.Copy(),
			}

			once.Do(func() {
				select {
				case resultChan <- result:
					logger.Debugf("[queryRacing] 竞速获胜者: %s (耗时: %v)", srv.Address(), time.Since(queryStartTime))
				default:
				}
				cancelAll() // 获胜后取消其他在途请求
			})
		} else {
			// 非 Success 和 NXDOMAIN 视为失败
			if isPrimary {
				// 主请求应用层错误（如 SERVFAIL）不触发抢跑，但记录
				logger.Debugf("[queryRacing] 主请求应用层错误: rcode=%d", reply.Rcode)
			}
			select {
			case errorChan <- fmt.Errorf("dns rcode=%d", reply.Rcode):
			case <-raceCtx.Done():
			}
		}
	}

	// 3. 启动主请求
	wg.Add(1)
	go doQuery(sortedServers[0], true)

	// 4. 节奏控制：等待延迟到期或主请求异常
	go func() {
		timer := time.NewTimer(raceDelay)
		defer timer.Stop()

		select {
		case <-timer.C:
			// 延迟正常到期
		case <-cancelDelayChan:
			// 错误抢跑：主服务器已报错，不等了
			logger.Debugf("[queryRacing] 主请求异常，触发错误抢跑逻辑")
		case <-raceCtx.Done():
			return
		}

		// 发起备选阶梯请求
		remaining := sortedServers[1:]
		if len(remaining) == 0 {
			return
		}

		// 计算动态批次大小和间隔
		batchSize, stagger := u.calculateRacingBatchParams(len(remaining), stdDev)

		logger.Debugf("[queryRacing] 启动备选梯队: 批次大小=%d, 间隔=%v", batchSize, stagger)

		for i := 0; i < len(remaining) && i < maxConcurrent-1; i += batchSize {
			end := min(i+batchSize, len(remaining), maxConcurrent-1)

			for _, srv := range remaining[i:end] {
				// 检查服务器健康状态，决定是否跳过或延后
				if shouldSkipServerInRacing(srv) {
					logger.Debugf("[queryRacing] 跳过不健康的服务器: %s (状态=%v)",
						srv.Address(), srv.GetHealth().GetStatus())
					continue
				}

				wg.Add(1)
				go doQuery(srv, false)
			}

			if end < len(remaining) && end < maxConcurrent-1 {
				select {
				case <-time.After(stagger):
				case <-raceCtx.Done():
					return
				}
			}
		}
	}()

	// 5. 等待最终结果
	select {
	case res := <-resultChan:
		u.RecordQueryLatency(time.Since(queryStartTime))
		u.recordRacingStats(true, earlyTriggerCount.Load() > 0)
		return res, nil
	case <-raceCtx.Done():
		// 如果是因为所有请求都结束了但没有结果
		go func() {
			wg.Wait()
			close(errorChan)
		}()

		// 收集最后一个错误返回
		var lastErr error
		for err := range errorChan {
			lastErr = err
		}
		u.recordRacingStats(false, earlyTriggerCount.Load() > 0)
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, ctx.Err()
	}
}

// isNetworkError 判断是否是网络层错误（应该触发抢跑）
// 网络错误包括：连接拒绝、超时、连接重置等
// 应用层错误（如 SERVFAIL）不触发抢跑
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}

	// 检查是否是网络错误
	if netErr, ok := err.(net.Error); ok {
		// Timeout 和 Temporary 都视为网络错误
		return netErr.Timeout() || netErr.Temporary()
	}

	// 检查错误字符串中的关键词
	errStr := err.Error()
	networkKeywords := []string{
		"connection refused",
		"connection reset",
		"connection timeout",
		"i/o timeout",
		"no such host",
		"network unreachable",
		"host unreachable",
		"broken pipe",
	}

	for _, keyword := range networkKeywords {
		if contains(errStr, keyword) {
			return true
		}
	}

	return false
}

// contains 检查字符串是否包含子串（不区分大小写）
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// toLower 将字符转换为小写
func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

// shouldSkipServerInRacing 判断是否应该在竞速中跳过某个服务器
// 跳过条件：服务器处于 Unhealthy 状态
func shouldSkipServerInRacing(srv *HealthAwareUpstream) bool {
	if srv == nil {
		return true
	}

	health := srv.GetHealth()
	if health == nil {
		return false
	}

	// 只跳过 Unhealthy 状态的服务器
	// Degraded 状态的服务器仍然可以尝试
	return health.GetStatus() == HealthStatusUnhealthy
}

// calculateRacingBatchParams 计算竞速的动态批次大小和间隔
// 根据服务器数量和网络标准差动态调整
func (u *Manager) calculateRacingBatchParams(remainingCount int, stdDev time.Duration) (batchSize int, stagger time.Duration) {
	// 基础批次大小
	batchSize = 2

	// 基础间隔
	stagger = 20 * time.Millisecond

	// 如果网络抖动较大（标准差 > 50ms），更激进地启动
	if stdDev > 50*time.Millisecond {
		batchSize = 3
		stagger = 15 * time.Millisecond
		logger.Debugf("[queryRacing] 网络抖动较大，调整批次参数: batchSize=%d, stagger=%v", batchSize, stagger)
	}

	// 如果剩余服务器很多（> 5个），增加批次大小以加快启动
	if remainingCount > 5 {
		batchSize = min(batchSize+1, 4)
		logger.Debugf("[queryRacing] 服务器较多，调整批次大小: batchSize=%d", batchSize)
	}

	return batchSize, stagger
}

// recordRacingStats 记录竞速策略的统计信息
func (u *Manager) recordRacingStats(success bool, earlyTriggered bool) {
	// 这里可以集成到全局统计系统
	// 目前只记录日志
	if earlyTriggered {
		logger.Debugf("[queryRacing] 查询完成: success=%v, 触发了错误抢跑", success)
	}
}
