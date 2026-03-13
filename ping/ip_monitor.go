package ping

import (
	"context"
	"smartdnssort/logger"
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
	}
}

// IPMonitorStats 监控器统计信息
type IPMonitorStats struct {
	TotalRefreshes    int64     // 总刷新次数
	TotalIPsRefreshed int64     // 总刷新 IP 数
	LastRefreshTime   time.Time // 最后刷新时间
	T0PoolSize        int       // T0 核心池大小
	T1PoolSize        int       // T1 活跃池大小
	T2PoolSize        int       // T2 淘汰池大小
}

// IPMonitor IP 主动巡检调度器
// 实现三级分步刷新机制，根据权重优先级调度 IP 测速
type IPMonitor struct {
	pinger *Pinger
	config IPMonitorConfig
	stats  IPMonitorStats
	mu     sync.RWMutex
	stopCh chan struct{}
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

	return &IPMonitor{
		pinger: pinger,
		config: config,
		stopCh: make(chan struct{}),
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
	defer t0Ticker.Stop()
	defer t1Ticker.Stop()
	defer t2Ticker.Stop()

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

// selectT0IPs 选择 T0 核心池的 IP
// T0 核心池：引用计数高、访问热度高的 IP
func (m *IPMonitor) selectT0IPs() []string {
	if m.pinger.ipPool == nil {
		return nil
	}

	allIPs := m.pinger.ipPool.GetAllIPs()
	if len(allIPs) == 0 {
		return nil
	}

	// 计算权重并排序
	weighted := make([]struct {
		ip     string
		weight float64
	}, 0, len(allIPs))

	for _, info := range allIPs {
		weight := m.calculateWeight(info)
		weighted = append(weighted, struct {
			ip     string
			weight float64
		}{ip: info.IP, weight: weight})
	}

	// 按权重降序排序
	for i := 0; i < len(weighted); i++ {
		for j := i + 1; j < len(weighted); j++ {
			if weighted[j].weight > weighted[i].weight {
				weighted[i], weighted[j] = weighted[j], weighted[i]
			}
		}
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
func (m *IPMonitor) selectT1IPs() []string {
	if m.pinger.ipPool == nil {
		return nil
	}

	allIPs := m.pinger.ipPool.GetAllIPs()
	if len(allIPs) == 0 {
		return nil
	}

	// 计算权重并排序
	weighted := make([]struct {
		ip     string
		weight float64
	}, 0, len(allIPs))

	for _, info := range allIPs {
		weight := m.calculateWeight(info)
		weighted = append(weighted, struct {
			ip     string
			weight float64
		}{ip: info.IP, weight: weight})
	}

	// 按权重降序排序
	for i := 0; i < len(weighted); i++ {
		for j := i + 1; j < len(weighted); j++ {
			if weighted[j].weight > weighted[i].weight {
				weighted[i], weighted[j] = weighted[j], weighted[i]
			}
		}
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
// 使用采样策略避免全量排序的性能问题
func (m *IPMonitor) selectT2IPs() []string {
	if m.pinger.ipPool == nil {
		return nil
	}

	allIPs := m.pinger.ipPool.GetAllIPs()
	if len(allIPs) == 0 {
		return nil
	}

	// 对于 T2 池，使用采样策略而不是全量排序
	// 如果 IP 数量超过 1000，只采样 1000 个
	sampleSize := len(allIPs)
	if sampleSize > 1000 {
		sampleSize = 1000
	}

	// 简单采样：从后往前取（低权重 IP）
	startIdx := len(allIPs) - sampleSize
	if startIdx < 0 {
		startIdx = 0
	}

	// 计算权重并排序
	weighted := make([]struct {
		ip     string
		weight float64
	}, 0, sampleSize)

	for i := startIdx; i < len(allIPs); i++ {
		info := allIPs[i]
		weight := m.calculateWeight(info)
		weighted = append(weighted, struct {
			ip     string
			weight float64
		}{ip: info.IP, weight: weight})
	}

	// 按权重降序排序
	for i := 0; i < len(weighted); i++ {
		for j := i + 1; j < len(weighted); j++ {
			if weighted[j].weight > weighted[i].weight {
				weighted[i], weighted[j] = weighted[j], weighted[i]
			}
		}
	}

	// 选择前 N 个最高权重的 IP（从采样中）
	maxIPs := m.config.MaxRefreshPerCycle
	if maxIPs > len(weighted) {
		maxIPs = len(weighted)
	}

	result := make([]string, 0, maxIPs)
	for i := 0; i < maxIPs; i++ {
		result = append(result, weighted[i].ip)
	}

	m.mu.Lock()
	m.stats.T2PoolSize = maxIPs
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
func (m *IPMonitor) refreshIPs(ips []string, poolName string) {
	if len(ips) == 0 {
		logger.Infof("[IPMonitor] %s pool: No IPs to refresh", poolName)
		return
	}

	ctx := context.Background()
	successCount := 0
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
				// 获取代表性域名用于 SNI
				domain := ""
				if m.pinger.ipPool != nil {
					if repDomain, exists := m.pinger.ipPool.GetRepDomain(ip); exists {
						domain = repDomain
					}
				}

				// 执行测速（使用 smartPingWithMethod 获取探测方法）
				rtt, method := m.pinger.smartPingWithMethod(ctx, ip, domain)

				// 第三阶段修复：将探测结果写入全局 RTT 缓存
				// 这样 PingAndSort 就可以直接使用 IPMonitor 维护的数据
				if rtt >= 0 {
					m.pinger.UpdateIPCache(ip, rtt, 0, method)
					mu.Lock()
					successCount++
					mu.Unlock()
				} else {
					// 不可达的 IP 也需要缓存，避免频繁探测
					// 使用 100% 丢包率标记
					m.pinger.UpdateIPCache(ip, 999999, 100, method)
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
	m.stats.TotalIPsRefreshed += int64(len(ips))
	m.stats.LastRefreshTime = time.Now()
	m.mu.Unlock()

	logger.Infof("[IPMonitor] %s pool: Refreshed %d IPs, %d successful", poolName, len(ips), successCount)
}

// GetStats 获取监控器统计信息
func (m *IPMonitor) GetStats() IPMonitorStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stats
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
