package ping

import (
	"context"
	"smartdnssort/logger"
	"sync"
	"time"
)

// refreshIPs 刷新指定的 IP 列表（并发版本）
// 第三阶段优化：探测结果会写入全局 RTT 缓存，供 PingAndSort 使用
// 第四阶段优化：添加探测冷却时间、稳定性退避、滑动窗口、全局配额
func (m *IPMonitor) refreshIPs(ips []string, poolName string) {
	if len(ips) == 0 {
		logger.Debugf("[IPMonitor] %s pool: No IPs to refresh", poolName)
		return
	}

	// === 全局熔断与配额检查 ===
	if m.config.MaxPingsPerHour > 0 {
		m.checkHourlyQuota()
		m.mu.Lock()
		if m.hourlyPingCount >= int64(m.config.MaxPingsPerHour) {
			m.mu.Unlock()
			logger.Warnf("[IPMonitor] Hourly quota exceeded (%d/%d), skipping %s refresh",
				m.hourlyPingCount, m.config.MaxPingsPerHour, poolName)
			return
		}
		m.mu.Unlock()
	}

	ctx := context.Background()
	successCount := 0
	skippedCount := 0
	var mu sync.Mutex

	// 使用 worker pool 模式进行并发测速
	workerCount := m.config.RefreshConcurrency
	if workerCount > len(ips) {
		workerCount = len(ips)
	}

	ipCh := make(chan string, len(ips))
	var wg sync.WaitGroup

	// 启动 worker goroutines
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ip := range ipCh {
				// === 探测冷却时间检查（Cooldown / TTL Padding） ===
				if m.config.EnableCooldown {
					remainingMs, isFresh := m.pinger.GetCacheTTLRemaining(ip)
					if isFresh {
						// 计算当前刷新周期的阈值（毫秒）
						var intervalMs int64
						switch poolName {
						case "T0":
							intervalMs = int64(m.config.T0RefreshInterval) * 1000
						case "T1":
							intervalMs = int64(m.config.T1RefreshInterval) * 1000
						case "T2":
							intervalMs = int64(m.config.T2RefreshInterval) * 1000
						default:
							intervalMs = 120000 // 默认 2 分钟
						}

						// 如果剩余 TTL 超过刷新周期的 CooldownRatio，跳过探测
						thresholdMs := int64(float64(intervalMs) * m.config.CooldownRatio)
						if remainingMs > thresholdMs {
							mu.Lock()
							skippedCount++
							mu.Unlock()
							continue
						}
					}
				}

				// 纯 ICMP 探测：不需要 SNI 域名
				// 执行测速（使用 smartPingWithMethod 获取探测方法）
				rtt, method, _ := m.pinger.smartPingWithMethod(ctx, ip, "")

				// 将探测结果写入全局 RTT 缓存
				// 这样 PingAndSort 就可以直接使用 IPMonitor 维护的数据
				if rtt >= 0 {
					m.pinger.UpdateIPCache(ip, rtt, 0, method)
					mu.Lock()
					successCount++
					mu.Unlock()

					// === 稳定性退避策略（Stability Backoff） ===
					if m.config.EnableStabilityBackoff {
						m.updateStabilityRecord(ip, rtt, poolName)
					}
				} else {
					// 不可达的 IP 也需要缓存，避免频繁探测
					// 使用 100% 丢包率标记
					m.pinger.UpdateIPCache(ip, LogicDeadRTT, 100, method)

					// 失败时重置稳定性记录（仅在网络在线时）
					// 静默隔离：如果网络离线，不应该重置稳定性记录
					// 原因：断网期间的探测失败不是 IP 本身的问题，不应该惩罚 IP
					if m.pinger.IsNetworkOnline() {
						m.resetStabilityRecord(ip)
					}
				}

				// === 更新最后监控时间（用于滑动窗口式巡检） ===
				if m.pinger.ipPool != nil {
					m.pinger.ipPool.UpdateMonitorTime(ip)
				}

				// === 更新全局配额计数 ===
				if m.config.MaxPingsPerHour > 0 {
					m.mu.Lock()
					m.hourlyPingCount++
					m.mu.Unlock()
				}
			}
		}()
	}

	// 分发任务
	for _, ip := range ips {
		ipCh <- ip
	}
	close(ipCh)

	// 等待所有 worker 完成
	wg.Wait()

	m.mu.Lock()
	m.stats.TotalRefreshes++
	// 真正的效率逻辑：
	m.stats.TotalPlannedPings += int64(len(ips))
	m.stats.TotalActualPings += int64(len(ips) - skippedCount) // 物理真实发包（含失败）
	m.stats.TotalSkippedPings += int64(skippedCount)           // 策略拦截（省下的负担）
	m.stats.HourlyQuotaUsed = int(m.hourlyPingCount)
	m.stats.HourlyQuotaLimit = m.config.MaxPingsPerHour
	m.stats.LastRefreshTime = time.Now()
	m.mu.Unlock()

	logger.Debugf("[IPMonitor] %s pool: Refreshed %d IPs, %d successful, %d skipped (cooldown)",
		poolName, len(ips), successCount, skippedCount)
}

// updateStabilityRecord 更新 IP 稳定性记录
// 用于稳定性退避策略：连续稳定的 IP 可以降级到低频池
// 修复 #6：使用 sync.Map 的 Load 和 Store 方法
func (m *IPMonitor) updateStabilityRecord(ip string, rtt int, poolName string) {
	// 使用 LoadOrStore 原子操作，避免竞态条件
	value, loaded := m.stabilityRecords.LoadOrStore(ip, &IPStabilityRecord{
		LastCheck: time.Now(),
		LastRTT:   rtt,
	})

	record := value.(*IPStabilityRecord)
	if loaded {
		// 记录已存在，更新逻辑
		// 检查 RTT 波动是否在阈值范围内
		if record.LastRTT > 0 {
			rttVariance := float64(abs(rtt-record.LastRTT)) / float64(record.LastRTT)
			if rttVariance <= m.config.StabilityRTTVariance {
				// RTT 稳定，增加稳定计数
				record.StableCount++
				record.LastCheck = time.Now()
				record.LastRTT = rtt

				// 如果达到稳定阈值且未降级，记录日志并标记为已降级
				if record.StableCount >= m.config.StabilityThreshold && !record.IsDowngraded {
					logger.Debugf("[IPMonitor] IP %s in %s pool reached stability threshold (%d times), marking as downgraded",
						ip, poolName, record.StableCount)
					record.IsDowngraded = true // 标记为已降级，防止日志刷屏并闭合逻辑
				}
			} else {
				// RTT 波动过大，重置稳定计数
				record.StableCount = 0
				record.LastCheck = time.Now()
				record.LastRTT = rtt
				record.IsDowngraded = false
			}
		} else {
			// LastRTT 为 0（新记录），直接更新
			record.LastCheck = time.Now()
			record.LastRTT = rtt
		}
	}
}

// resetStabilityRecord 重置 IP 稳定性记录
// 当 IP 探测失败时调用（仅在网络在线时）
// 注意：调用者应确保在网络在线时才调用此方法
// 修复 #6：使用 sync.Map 的 Load 方法
func (m *IPMonitor) resetStabilityRecord(ip string) {
	if v, ok := m.stabilityRecords.Load(ip); ok {
		record := v.(*IPStabilityRecord)
		record.StableCount = 0
		record.IsDowngraded = false
	}
}

// checkHourlyQuota 检查并重置每小时配额
func (m *IPMonitor) checkHourlyQuota() {
	now := time.Now()
	if now.Sub(m.hourlyResetTime) >= time.Hour {
		m.mu.Lock()
		m.hourlyPingCount = 0
		m.hourlyResetTime = now
		m.mu.Unlock()
		logger.Debugf("[IPMonitor] Hourly quota reset, starting new hour")
	}
}

// abs 返回整数的绝对值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
