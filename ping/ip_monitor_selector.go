package ping

import (
	"sort"
	"time"
)

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
