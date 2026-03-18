package ping

import (
	"context"
	"smartdnssort/logger"
	"sort"
	"sync"
	"time"
)

// IPMonitorConfig IP 监控器配置
type IPMonitorConfig struct {
	// T0 核心池刷新间隔（秒）
	T0RefreshInterval int
	// T1 活跃池刷新间隔（秒）
	T1RefreshInterval int
	// T2 淘汰池刷新间隔（秒）
	T2RefreshInterval int
	// 权重计算参数：引用计数权重
	RefCountWeight float64
	// 权重计算参数：访问热度权重
	AccessHeatWeight float64
	// 每次刷新的最大 IP 数量
	MaxRefreshPerCycle int
	// 并发测速数量
	RefreshConcurrency int
	// 是否启用监控
	Enabled bool
	// IP 池清理间隔（秒），默认 12 小时
	CleanupInterval int

	// === 优化配置：探测冷却时间（Cooldown / TTL Padding） ===
	// 启用探测冷却时间：如果缓存剩余 TTL 超过刷新周期的此比例，则跳过探测
	// 例如：T0 周期 120s，比例 0.5，则剩余 TTL > 60s 时跳过探测
	EnableCooldown bool
	CooldownRatio  float64 // 默认 0.5（50%）

	// === 优化配置：稳定性退避策略（Stability Backoff） ===
	// 启用稳定性退避：连续稳定的 IP 降级到低频池
	EnableStabilityBackoff bool
	StabilityThreshold     int     // 连续稳定次数阈值，默认 10
	StabilityRTTVariance   float64 // RTT 波动阈值（百分比），默认 0.05（5%）

	// === 优化配置：滑动窗口式巡检 ===
	// 启用滑动窗口：使用 "权重 + 时间" 单调增量逻辑选择 IP
	EnableSlidingWindow bool

	// === 优化配置：全局熔断与配额（Global Quota） ===
	// 每小时最大探测次数限制
	MaxPingsPerHour int
}

// DefaultIPMonitorConfig 默认配置
func DefaultIPMonitorConfig() IPMonitorConfig {
	return IPMonitorConfig{
		T0RefreshInterval:  120,  // 2 分钟
		T1RefreshInterval:  900,  // 15 分钟
		T2RefreshInterval:  3600, // 1 小时
		RefCountWeight:     1.0,
		AccessHeatWeight:   0.5,
		MaxRefreshPerCycle: 50,
		RefreshConcurrency: 10, // 并发测速数量
		Enabled:            true,
		CleanupInterval:    43200, // 12 小时

		// 优化配置默认值
		EnableCooldown:         true, // 启用探测冷却时间
		CooldownRatio:          0.5,  // 50% 剩余 TTL 时跳过
		EnableStabilityBackoff: true, // 启用稳定性退避
		StabilityThreshold:     10,   // 连续 10 次稳定
		StabilityRTTVariance:   0.05, // 5% RTT 波动阈值
		EnableSlidingWindow:    true, // 启用滑动窗口
		MaxPingsPerHour:        5000, // 每小时最大 5000 次探测
	}
}

// IPMonitorStats 监控器统计信息
type IPMonitorStats struct {
	TotalRefreshes    int64 // 扫描周期数（原来的）
	TotalPlannedPings int64 // 计划测速总数（原来的 TotalIPsRefreshed）
	TotalActualPings  int64 // 真正发出的 ICMP 包数量（新）
	TotalSkippedPings int64 // 被探测冷却/策略拦截的数量（新）
	LastRefreshTime   time.Time

	T0PoolSize int
	T1PoolSize int
	T2PoolSize int

	// 动态指标
	DowngradedIPs    int // 当前处于"稳定性降级"状态的 IP 总数（新）
	HourlyQuotaUsed  int // 本小时配额已使用量（新）
	HourlyQuotaLimit int // 本小时配额上限（新）
}

// weightedIP 带权重的 IP 结构体
type weightedIP struct {
	ip     string
	weight float64
}

// IPStabilityRecord IP 稳定性记录（用于稳定性退避策略）
type IPStabilityRecord struct {
	StableCount  int       // 连续稳定次数
	LastCheck    time.Time // 最后检查时间
	LastRTT      int       // 最后一次 RTT 值
	IsDowngraded bool      // 是否已降级到低频池
}

// IPMonitor IP 主动巡检调度器
// 实现三级分步刷新机制，根据权重优先级调度 IP 测速
type IPMonitor struct {
	pinger *Pinger
	config IPMonitorConfig
	stats  IPMonitorStats
	mu     sync.RWMutex
	stopCh chan struct{}

	// 优化功能相关字段
	stabilityRecords map[string]*IPStabilityRecord // IP 稳定性记录
	hourlyPingCount  int64                         // 本小时探测次数
	hourlyResetTime  time.Time                     // 小时重置时间
}

// NewIPMonitor 创建新的 IP 监控器
func NewIPMonitor(pinger *Pinger, config IPMonitorConfig) *IPMonitor {
	if config.T0RefreshInterval <= 0 {
		config.T0RefreshInterval = 120
	}
	if config.T1RefreshInterval <= 0 {
		config.T1RefreshInterval = 900
	}
	if config.T2RefreshInterval <= 0 {
		config.T2RefreshInterval = 3600
	}
	if config.RefCountWeight <= 0 {
		config.RefCountWeight = 1.0
	}
	if config.AccessHeatWeight <= 0 {
		config.AccessHeatWeight = 0.5
	}
	if config.MaxRefreshPerCycle <= 0 {
		config.MaxRefreshPerCycle = 50
	}
	if config.RefreshConcurrency <= 0 {
		config.RefreshConcurrency = 10
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 43200 // 默认 12 小时
	}
	if config.CooldownRatio <= 0 {
		config.CooldownRatio = 0.5
	}
	if config.StabilityThreshold <= 0 {
		config.StabilityThreshold = 10
	}
	if config.StabilityRTTVariance <= 0 {
		config.StabilityRTTVariance = 0.05
	}
	if config.MaxPingsPerHour <= 0 {
		config.MaxPingsPerHour = 5000
	}

	return &IPMonitor{
		pinger:           pinger,
		config:           config,
		stopCh:           make(chan struct{}),
		stabilityRecords: make(map[string]*IPStabilityRecord),
		hourlyResetTime:  time.Now(),
	}
}

// Start 启动监控器
func (m *IPMonitor) Start() {
	if !m.config.Enabled {
		logger.Info("[IPMonitor] IP Monitor is disabled")
		return
	}

	logger.Info("[IPMonitor] Starting IP Monitor...")
	go m.run()
}

// Stop 停止监控器
func (m *IPMonitor) Stop() {
	logger.Info("[IPMonitor] Stopping IP Monitor...")
	close(m.stopCh)
}

// run 主循环
func (m *IPMonitor) run() {
	t0Ticker := time.NewTicker(time.Duration(m.config.T0RefreshInterval) * time.Second)
	t1Ticker := time.NewTicker(time.Duration(m.config.T1RefreshInterval) * time.Second)
	t2Ticker := time.NewTicker(time.Duration(m.config.T2RefreshInterval) * time.Second)
	cleanupTicker := time.NewTicker(time.Duration(m.config.CleanupInterval) * time.Second)
	defer t0Ticker.Stop()
	defer t1Ticker.Stop()
	defer t2Ticker.Stop()
	defer cleanupTicker.Stop()

	// 启动时立即执行一次刷新
	m.refreshAllPools()

	for {
		select {
		case <-m.stopCh:
			return
		case <-t0Ticker.C:
			// 静默隔离：在分发任务之前检查网络状态
			// 目的："性能损耗与误杀保护"。断网时巡检没有任何意义，
			// 只会消耗系统资源并产生大量的报错日志。
			if !m.pinger.IsNetworkOnline() {
				logger.Warn("[IPMonitor] Network abnormality detected, skipping T0 refresh cycle.")
				continue
			}
			m.refreshT0Pool()
		case <-t1Ticker.C:
			// 静默隔离：在分发任务之前检查网络状态
			if !m.pinger.IsNetworkOnline() {
				logger.Warn("[IPMonitor] Network abnormality detected, skipping T1 refresh cycle.")
				continue
			}
			m.refreshT1Pool()
		case <-t2Ticker.C:
			// 静默隔离：在分发任务之前检查网络状态
			if !m.pinger.IsNetworkOnline() {
				logger.Warn("[IPMonitor] Network abnormality detected, skipping T2 refresh cycle.")
				continue
			}
			m.refreshT2Pool()
		case <-cleanupTicker.C:
			// 网络异常期，暂停清理僵尸 IP
			// 防止在长时间断网期间，原本健康的 IP 因为没有访问热度更新而被判定为"僵尸 IP"并从池中删除
			if !m.pinger.IsNetworkOnline() {
				logger.Warn("[IPMonitor] Network abnormality detected, skipping stale IP cleanup cycle.")
				continue
			}
			// 定期清理 IP 池中的僵尸 IP
			m.cleanupStaleIPs()
		}
	}
}

// refreshAllPools 刷新所有池
func (m *IPMonitor) refreshAllPools() {
	logger.Info("[IPMonitor] Initial refresh of all IP pools")
	m.refreshT0Pool()
	m.refreshT1Pool()
	m.refreshT2Pool()
}

// refreshT0Pool 刷新 T0 核心池（最高优先级）
func (m *IPMonitor) refreshT0Pool() {
	logger.Info("[IPMonitor] Refreshing T0 core pool...")
	ips := m.selectT0IPs()
	m.refreshIPs(ips, "T0")
}

// refreshT1Pool 刷新 T1 活跃池
func (m *IPMonitor) refreshT1Pool() {
	logger.Info("[IPMonitor] Refreshing T1 active pool...")
	ips := m.selectT1IPs()
	m.refreshIPs(ips, "T1")
}

// refreshT2Pool 刷新 T2 淘汰池
func (m *IPMonitor) refreshT2Pool() {
	logger.Info("[IPMonitor] Refreshing T2淘汰池...")
	ips := m.selectT2IPs()
	m.refreshIPs(ips, "T2")
}

// selectSortedIPs 单次扫描计算并排序所有 IP 权重
// 优化版：使用 sort.Slice 实现 O(N log N) 排序，避免重复遍历
// 第四阶段优化：支持滑动窗口式巡检（权重 + 时间单调增量逻辑）
func (m *IPMonitor) selectSortedIPs() []weightedIP {
	if m.pinger.ipPool == nil {
		return nil
	}

	allIPs := m.pinger.ipPool.GetAllIPs()
	if len(allIPs) == 0 {
		return nil
	}

	weighted := make([]weightedIP, 0, len(allIPs))
	now := time.Now()

	// === 稳定性退避策略：对极度稳定的 IP 进行降级 ===
	// 在整个循环外部统一加锁，避免频繁的锁竞争
	// stabilityRecords 在这个过程中不应发生突变
	if m.config.EnableStabilityBackoff {
		m.mu.RLock()
		defer m.mu.RUnlock()
	}

	for _, info := range allIPs {
		w := m.calculateWeight(info)

		// === 稳定性退避策略：对极度稳定的 IP 进行降级 ===
		if m.config.EnableStabilityBackoff {
			if record, ok := m.stabilityRecords[info.IP]; ok && record.StableCount >= m.config.StabilityThreshold {
				// 对于已经极度稳定的 IP，将其原始权重减半，从而挤出 T0 核心池
				w = w * 0.5
			}
		}

		// === 滑动窗口式巡检：权重 + 时间单调增量逻辑 ===
		if m.config.EnableSlidingWindow {
			// 计算时间因子：距离上次监控的时间越长，时间因子越大
			// 使用 T0 周期作为基准时间间隔
			baseInterval := time.Duration(m.config.T0RefreshInterval) * time.Second

			var timeSinceLastMonitor time.Duration
			if info.LastMonitorTime.IsZero() {
				// 如果从未监控过，使用最大时间因子
				timeSinceLastMonitor = baseInterval * 10
			} else {
				timeSinceLastMonitor = now.Sub(info.LastMonitorTime)
			}

			// 时间因子 = 距离上次监控的时间 / 基准间隔
			// 这样刚测过的 IP 时间因子接近 0，很久没测的 IP 时间因子较大
			timeFactor := float64(timeSinceLastMonitor) / float64(baseInterval)

			// 最终权重 = 原始权重 * (1 + 时间因子)
			// 这样权重高但刚测过的 IP 优先级会降低
			w = w * (1.0 + timeFactor)
		}

		weighted = append(weighted, weightedIP{ip: info.IP, weight: w})
	}

	// 使用 sort.Slice 进行排序，时间复杂度 O(N log N)
	sort.Slice(weighted, func(i, j int) bool {
		return weighted[i].weight > weighted[j].weight
	})

	return weighted
}

// selectT0IPs 选择 T0 核心池的 IP
// T0 核心池：引用计数高、访问热度高的 IP
// 优化版：直接从 selectSortedIPs 的结果中分段取值
func (m *IPMonitor) selectT0IPs() []string {
	weighted := m.selectSortedIPs()
	if len(weighted) == 0 {
		return nil
	}

	// 选择前 N 个最高权重的 IP
	maxIPs := m.config.MaxRefreshPerCycle
	if maxIPs > len(weighted) {
		maxIPs = len(weighted)
	}

	result := make([]string, 0, maxIPs)
	for i := 0; i < maxIPs; i++ {
		result = append(result, weighted[i].ip)
	}

	m.mu.Lock()
	m.stats.T0PoolSize = maxIPs
	m.mu.Unlock()

	return result
}

// selectT1IPs 选择 T1 活跃池的 IP
// T1 活跃池：引用计数中等、访问热度中等的 IP
// 优化版：直接从 selectSortedIPs 的结果中分段取值
func (m *IPMonitor) selectT1IPs() []string {
	weighted := m.selectSortedIPs()
	if len(weighted) == 0 {
		return nil
	}

	// 跳过 T0 池的 IP，选择中等权重的 IP
	startIdx := m.config.MaxRefreshPerCycle
	if startIdx >= len(weighted) {
		return nil
	}

	maxIPs := m.config.MaxRefreshPerCycle
	endIdx := startIdx + maxIPs
	if endIdx > len(weighted) {
		endIdx = len(weighted)
	}

	result := make([]string, 0, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		result = append(result, weighted[i].ip)
	}

	m.mu.Lock()
	m.stats.T1PoolSize = endIdx - startIdx
	m.mu.Unlock()

	return result
}

// selectT2IPs 选择 T2 淘汰池的 IP
// T2 淘汰池：引用计数低、访问热度低的 IP
// 优化版：直接从 selectSortedIPs 的结果中分段取值，解决采样随机性问题
func (m *IPMonitor) selectT2IPs() []string {
	weighted := m.selectSortedIPs()
	if len(weighted) == 0 {
		return nil
	}

	// 跳过 T0 和 T1 池的 IP，选择低权重的 IP
	startIdx := m.config.MaxRefreshPerCycle * 2
	if startIdx >= len(weighted) {
		return nil
	}

	maxIPs := m.config.MaxRefreshPerCycle
	endIdx := startIdx + maxIPs
	if endIdx > len(weighted) {
		endIdx = len(weighted)
	}

	result := make([]string, 0, endIdx-startIdx)
	for i := startIdx; i < endIdx; i++ {
		result = append(result, weighted[i].ip)
	}

	m.mu.Lock()
	m.stats.T2PoolSize = endIdx - startIdx
	m.mu.Unlock()

	return result
}

// calculateWeight 计算 IP 的权重
// 权重 = RefCount * A + AccessHeat * B - 失败惩罚
func (m *IPMonitor) calculateWeight(info *IPInfo) float64 {
	weight := float64(info.RefCount)*m.config.RefCountWeight + float64(info.AccessHeat)*m.config.AccessHeatWeight

	// 获取 IP 失效权重，对连续失败的 IP 进行惩罚
	if m.pinger.failureWeightMgr != nil {
		failWeight := m.pinger.failureWeightMgr.GetWeight(info.IP)
		// 失败权重越高，惩罚越大
		// 假设失败权重范围是 0-10000，我们将其转换为 0-100 的惩罚
		penalty := float64(failWeight) / 100.0
		weight -= penalty
	}

	// 确保权重不为负
	if weight < 0 {
		weight = 0
	}

	return weight
}

// refreshIPs 刷新指定的 IP 列表（并发版本）
// 第三阶段优化：探测结果会写入全局 RTT 缓存，供 PingAndSort 使用
// 第四阶段优化：添加探测冷却时间、稳定性退避、滑动窗口、全局配额
func (m *IPMonitor) refreshIPs(ips []string, poolName string) {
	if len(ips) == 0 {
		logger.Infof("[IPMonitor] %s pool: No IPs to refresh", poolName)
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

					// 失败时重置稳定性记录
					m.resetStabilityRecord(ip)
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

	logger.Infof("[IPMonitor] %s pool: Refreshed %d IPs, %d successful, %d skipped (cooldown)",
		poolName, len(ips), successCount, skippedCount)
}

// GetStats 获取监控器统计信息
func (m *IPMonitor) GetStats() IPMonitorStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 计算降级 IP 数量
	downgradedCount := 0
	for _, record := range m.stabilityRecords {
		if record.IsDowngraded || record.StableCount >= m.config.StabilityThreshold {
			downgradedCount++
		}
	}

	stats := m.stats
	stats.DowngradedIPs = downgradedCount
	return stats
}

// GetIPPool 获取 IP 池实例
func (m *IPMonitor) GetIPPool() *IPPool {
	if m.pinger == nil {
		return nil
	}
	return m.pinger.GetIPPool()
}

// GetPinger 获取 Pinger 实例
func (m *IPMonitor) GetPinger() *Pinger {
	return m.pinger
}

// cleanupStaleIPs 清理 IP 池中的僵尸 IP
// 定期执行，清理长时间未访问且无引用的 IP，防止内存泄露
func (m *IPMonitor) cleanupStaleIPs() {
	if m.pinger.ipPool == nil {
		return
	}

	// 清理 24 小时未被访问且无引用的 IP
	cleanedCount := m.pinger.ipPool.CleanStaleIPs(24 * time.Hour)
	if cleanedCount > 0 {
		logger.Infof("[IPMonitor] Cleaned %d stale IPs from pool", cleanedCount)
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
		logger.Infof("[IPMonitor] Hourly quota reset, starting new hour")
	}
}

// updateStabilityRecord 更新 IP 稳定性记录
// 用于稳定性退避策略：连续稳定的 IP 可以降级到低频池
func (m *IPMonitor) updateStabilityRecord(ip string, rtt int, poolName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, exists := m.stabilityRecords[ip]
	if !exists {
		record = &IPStabilityRecord{
			LastCheck: time.Now(),
			LastRTT:   rtt,
		}
		m.stabilityRecords[ip] = record
		return
	}

	// 检查 RTT 波动是否在阈值范围内
	rttVariance := float64(abs(rtt-record.LastRTT)) / float64(record.LastRTT)
	if rttVariance <= m.config.StabilityRTTVariance {
		// RTT 稳定，增加稳定计数
		record.StableCount++
		record.LastCheck = time.Now()
		record.LastRTT = rtt

		// 如果达到稳定阈值且未降级，记录日志并标记为已降级
		if record.StableCount >= m.config.StabilityThreshold && !record.IsDowngraded {
			logger.Infof("[IPMonitor] IP %s in %s pool reached stability threshold (%d times), marking as downgraded",
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
}

// resetStabilityRecord 重置 IP 稳定性记录
// 当 IP 探测失败时调用
func (m *IPMonitor) resetStabilityRecord(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if record, exists := m.stabilityRecords[ip]; exists {
		record.StableCount = 0
		record.IsDowngraded = false
	}
}

// abs 返回整数的绝对值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
