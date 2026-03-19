package ping

import (
	"smartdnssort/logger"
	"time"
)

// Start 启动监控器
func (m *IPMonitor) Start() {
	if !m.config.Enabled {
		logger.Debug("[IPMonitor] IP Monitor is disabled")
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
	logger.Debug("[IPMonitor] Initial refresh of all IP pools")
	m.refreshT0Pool()
	m.refreshT1Pool()
	m.refreshT2Pool()
}

// refreshT0Pool 刷新 T0 核心池（最高优先级）
func (m *IPMonitor) refreshT0Pool() {
	logger.Debug("[IPMonitor] Refreshing T0 core pool...")
	ips := m.selectT0IPs()
	m.refreshIPs(ips, "T0")
}

// refreshT1Pool 刷新 T1 活跃池
func (m *IPMonitor) refreshT1Pool() {
	logger.Debug("[IPMonitor] Refreshing T1 active pool...")
	ips := m.selectT1IPs()
	m.refreshIPs(ips, "T1")
}

// refreshT2Pool 刷新 T2 淘汰池
func (m *IPMonitor) refreshT2Pool() {
	logger.Debug("[IPMonitor] Refreshing T2 淘汰池...")
	ips := m.selectT2IPs()
	m.refreshIPs(ips, "T2")
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
// 同时清理 stabilityRecords 中的孤立记录，防止内存泄露
func (m *IPMonitor) cleanupStaleIPs() {
	if m.pinger.ipPool == nil {
		return
	}

	// 清理 24 小时未被访问且无引用的 IP
	cleanedCount := m.pinger.ipPool.CleanStaleIPs(24 * time.Hour)
	if cleanedCount > 0 {
		logger.Infof("[IPMonitor] Cleaned %d stale IPs from pool", cleanedCount)
	}

	// === 修复内存泄露：同步清理 stabilityRecords 中的孤立记录 ===
	// 获取 IP 池中所有有效的 IP
	validIPs := make(map[string]bool)
	if m.pinger.ipPool != nil {
		allIPs := m.pinger.ipPool.GetAllIPs()
		for _, info := range allIPs {
			validIPs[info.IP] = true
		}
	}

	// 遍历 stabilityRecords，删除不在 IP 池中的孤立记录
	m.mu.Lock()
	orphanCount := 0
	for ip := range m.stabilityRecords {
		if !validIPs[ip] {
			delete(m.stabilityRecords, ip)
			orphanCount++
		}
	}
	m.mu.Unlock()

	if orphanCount > 0 {
		logger.Debugf("[IPMonitor] Cleaned %d orphan stability records", orphanCount)
	}
}
