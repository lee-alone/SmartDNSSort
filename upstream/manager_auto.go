package upstream

import (
	"math"
	"smartdnssort/config"
	"smartdnssort/logger"
	"sync"
	"time"
)

// selectInitialStrategy 根据服务器数量和配置选择初始策略
func selectInitialStrategy(cfg *config.UpstreamConfig, numServers int) string {
	strategy := cfg.Strategy
	if strategy == "" || strategy == "auto" {
		switch {
		case numServers <= 1:
			strategy = "sequential"
		case numServers <= 3:
			strategy = "racing"
		default:
			strategy = "parallel"
		}
		logger.Infof("[Manager] Auto-selected strategy: %s (based on %d servers)", strategy, numServers)
	}
	return strategy
}

// DynamicParamOptimization 动态参数优化
type DynamicParamOptimization struct {
	mu sync.RWMutex

	// EWMA 平滑因子
	ewmaAlpha float64

	// 最大步长（毫秒）
	maxStepMs int

	// 平均延迟（EWMA）
	avgLatency time.Duration

	// 延迟方差（用于计算标准差）
	// 使用 Welford's algorithm 在线计算
	latencyMean float64
	latencyM2   float64
	latencyN    int64

	// 上次调整时间
	lastAdjustTime time.Time

	// 调整历史
	adjustmentCount int
}

// StrategyMetrics 策略性能指标
type StrategyMetrics struct {
	mu sync.RWMutex

	// 策略性能统计
	strategyStats map[string]*StrategyStats

	// 上次评估时间
	lastEvalTime time.Time
}

// StrategyStats 策略统计信息
type StrategyStats struct {
	// 响应时间统计
	totalLatency time.Duration
	requestCount int64
	avgLatency   time.Duration

	// 错误统计
	errorCount int64
	errorRate  float64

	// 吞吐量统计
	successCount int64
	successRate  float64

	// 方差计算 (Welford's algorithm)
	mean float64
	m2   float64
}

// PerformanceMetrics 性能指标
type PerformanceMetrics struct {
	mu sync.RWMutex

	// 响应时间分布
	latencyHistogram map[string]int64 // 延迟分布（毫秒）

	// 吞吐量统计
	throughputCounter int64

	// 错误统计
	errorCounter int64

	// 可用性统计
	availabilityCounter int64

	// 时间戳
	lastResetTime time.Time
}

// RecordQueryLatency 记录查询延迟，用于动态参数优化
// 同时更新平均延迟和方差（使用 Welford's algorithm）
func (u *Manager) RecordQueryLatency(latency time.Duration) {
	if u.dynamicParamOptimization == nil {
		return
	}

	u.dynamicParamOptimization.mu.Lock()
	defer u.dynamicParamOptimization.mu.Unlock()

	// 使用 EWMA 更新平均延迟
	if u.dynamicParamOptimization.avgLatency == 0 {
		u.dynamicParamOptimization.avgLatency = latency
	} else {
		alpha := u.dynamicParamOptimization.ewmaAlpha
		newAvg := time.Duration(alpha*float64(latency) + (1.0-alpha)*float64(u.dynamicParamOptimization.avgLatency))
		u.dynamicParamOptimization.avgLatency = newAvg
	}

	// 使用 Welford's algorithm 在线计算方差
	latencyMs := float64(latency.Milliseconds())
	u.dynamicParamOptimization.latencyN++

	if u.dynamicParamOptimization.latencyN == 1 {
		u.dynamicParamOptimization.latencyMean = latencyMs
		u.dynamicParamOptimization.latencyM2 = 0
	} else {
		delta := latencyMs - u.dynamicParamOptimization.latencyMean
		u.dynamicParamOptimization.latencyMean += delta / float64(u.dynamicParamOptimization.latencyN)
		delta2 := latencyMs - u.dynamicParamOptimization.latencyMean
		u.dynamicParamOptimization.latencyM2 += delta * delta2
	}

	// 衰减旧数据：如果样本过多，减半处理（实现类似滑动窗口的效果）
	if u.dynamicParamOptimization.latencyN > 500 {
		u.dynamicParamOptimization.latencyN /= 2
		u.dynamicParamOptimization.latencyM2 /= 2
	}
}

// GetAverageLatency 获取平均延迟
func (u *Manager) GetAverageLatency() time.Duration {
	if u.dynamicParamOptimization == nil {
		return 200 * time.Millisecond
	}

	u.dynamicParamOptimization.mu.RLock()
	defer u.dynamicParamOptimization.mu.RUnlock()

	return u.dynamicParamOptimization.avgLatency
}

// GetLatencyStdDev 获取延迟的标准差（毫秒）
// 用于计算自适应竞速延迟
func (u *Manager) GetLatencyStdDev() time.Duration {
	if u.dynamicParamOptimization == nil {
		return 0
	}

	u.dynamicParamOptimization.mu.RLock()
	defer u.dynamicParamOptimization.mu.RUnlock()

	if u.dynamicParamOptimization.latencyN < 2 {
		return 0
	}

	// 方差 = M2 / N
	variance := u.dynamicParamOptimization.latencyM2 / float64(u.dynamicParamOptimization.latencyN)

	// 标准差 = sqrt(方差)
	stdDev := time.Duration(int64(math.Sqrt(variance))) * time.Millisecond

	return stdDev
}

// GetAdaptiveRacingDelay 获取自适应竞速延迟（方差抖动适配）
// 核心思想：从"固定百分比"演进到"方差抖动适配"
// - 网络稳定（方差小）：给第一个服务器更多时间（100ms+）
// - 网络抖动（方差大）：尽早启动竞速（20ms-50ms）
// 公式：RacingDelay = Max(MinDelay, avgLatency - K * StdDev)
func (u *Manager) GetAdaptiveRacingDelay() time.Duration {
	avgLatency := u.GetAverageLatency()
	stdDev := u.GetLatencyStdDev()

	// K 系数：控制方差对延迟的影响程度
	// K=0.5 表示标准差每增加 1ms，延迟就减少 0.5ms
	const K = 0.5

	// 基础公式：avgLatency - K * stdDev
	// 这意味着网络越乱，我们越早发起"群殴"
	baseDelay := avgLatency - time.Duration(K*float64(stdDev))

	// 限制范围：20-200ms
	// - 最小 20ms：即使网络极度不稳定，也要给第一个服务器最少的机会
	// - 最大 200ms：即使网络极度稳定，也不能让第一个服务器等太久
	return max(20*time.Millisecond, min(baseDelay, 200*time.Millisecond))
}

// GetAdaptiveSequentialTimeout 获取自适应顺序查询超时
func (u *Manager) GetAdaptiveSequentialTimeout() time.Duration {
	avgLatency := u.GetAverageLatency()

	// 顺序查询超时 = 平均延迟 * 1.5
	timeout := time.Duration(float64(avgLatency) * 1.5)

	// 使用 Go 1.21+ 的内置 max/min 限制范围：500ms-2s
	return max(500*time.Millisecond, min(timeout, 2*time.Second))
}

// GetDynamicParamStats 获取动态参数优化的统计信息
func (u *Manager) GetDynamicParamStats() map[string]any {
	if u.dynamicParamOptimization == nil {
		return map[string]any{}
	}

	u.dynamicParamOptimization.mu.RLock()
	defer u.dynamicParamOptimization.mu.RUnlock()

	return map[string]interface{}{
		"avg_latency_ms":        float64(u.dynamicParamOptimization.avgLatency.Microseconds()) / 1000.0,
		"ewma_alpha":            u.dynamicParamOptimization.ewmaAlpha,
		"max_step_ms":           u.dynamicParamOptimization.maxStepMs,
		"adjustment_count":      u.dynamicParamOptimization.adjustmentCount,
		"racing_delay_ms":       float64(u.GetAdaptiveRacingDelay().Microseconds()) / 1000.0,
		"sequential_timeout_ms": float64(u.GetAdaptiveSequentialTimeout().Microseconds()) / 1000.0,
	}
}

// RecordStrategyResult 记录查询结果用于策略评估
func (u *Manager) RecordStrategyResult(strategy string, latency time.Duration, success bool) {
	if u.strategyMetrics == nil {
		return
	}

	u.strategyMetrics.mu.Lock()
	defer u.strategyMetrics.mu.Unlock()

	stats, ok := u.strategyMetrics.strategyStats[strategy]
	if !ok {
		stats = &StrategyStats{}
		u.strategyMetrics.strategyStats[strategy] = stats
	}

	// 衰减旧数据 (增加新样本的权重，约 100 个请求后旧数据权重下降)
	// 如果样本太多，减半处理，实现类似滑动窗口的效果
	if stats.requestCount > 200 {
		stats.totalLatency /= 2
		stats.m2 /= 2
		stats.requestCount /= 2
		stats.successCount /= 2
		stats.errorCount /= 2
	}

	stats.totalLatency += latency
	stats.requestCount++

	// 更新方差 (Welford's algorithm)
	latencyMs := float64(latency.Milliseconds())
	if stats.requestCount == 1 {
		stats.mean = latencyMs
		stats.m2 = 0
	} else {
		delta := latencyMs - stats.mean
		stats.mean += delta / float64(stats.requestCount)
		delta2 := latencyMs - stats.mean
		stats.m2 += delta * delta2
	}

	if success {
		stats.successCount++
	} else {
		stats.errorCount++
	}
}

// SelectOptimalStrategy 基于各策略的具体表现进行决策
func (u *Manager) SelectOptimalStrategy() string {
	if u.strategyMetrics == nil {
		return u.strategy
	}

	// 只有当全局策略设置为 "auto" 时才执行动态切换
	if u.strategy != "auto" {
		return u.strategy
	}

	u.strategyMetrics.mu.RLock()
	defer u.strategyMetrics.mu.RUnlock()

	// 1. 寻找当前表现最好和最差的指标
	var maxVariance float64
	var globalAvgLatency float64
	var globalSuccessRate float64
	var validStrategies int

	// 记录各个策略的情况
	for _, stats := range u.strategyMetrics.strategyStats {
		if stats.requestCount < 10 {
			continue
		}

		variance := stats.m2 / float64(stats.requestCount)
		if variance > maxVariance {
			maxVariance = variance // 取最差的情况，作为网络抖动的预警
		}

		globalAvgLatency += stats.mean
		globalSuccessRate += float64(stats.successCount) / float64(stats.requestCount)
		validStrategies++
	}

	// 如果样本不足，使用基于服务器数量的初始策略
	if validStrategies == 0 {
		return selectInitialStrategy(&config.UpstreamConfig{Strategy: "auto"}, len(u.servers))
	}

	avgLatencyMs := globalAvgLatency / float64(validStrategies)
	successRate := globalSuccessRate / float64(validStrategies)

	// 2. 核心决策逻辑：分布式感知

	// 容错优先：成功率低于阈值，强制 Parallel
	if successRate < 0.88 {
		return "parallel"
	}

	// 抖动感知：maxVariance 为各策略之中的最高方差
	// 如果最高方差 > 2500 (即标准差 > 50ms)，说明网络环境中存在不稳定的策略路径
	isJittery := maxVariance > 2500

	switch {
	case !isJittery && avgLatencyMs < 150:
		// 网络非常稳定且延迟低：Sequential 最优
		return "sequential"
	case maxVariance > 5000:
		// 网络极度不稳定 (标准差 > 70ms)：强制 Parallel 冗余以对冲丢包风险
		return "parallel"
	case isJittery || avgLatencyMs > 400:
		// 网络有抖动或延迟较高：Parallel 容错
		return "parallel"
	default:
		// 中等波动情况：根据服务器多寡选择 Racing 或进入 Parallel 缓冲
		if len(u.servers) > 3 {
			return "parallel"
		}
		return "racing"
	}
}

// EvaluateStrategyPerformance 定期评估策略性能
func (u *Manager) EvaluateStrategyPerformance() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if u.strategyMetrics == nil {
			continue
		}

		u.strategyMetrics.mu.Lock()

		// 计算每个策略的平均延迟和成功率
		for _, stats := range u.strategyMetrics.strategyStats {
			if stats.requestCount > 0 {
				stats.avgLatency = time.Duration(stats.totalLatency.Nanoseconds() / stats.requestCount)
				stats.errorRate = float64(stats.errorCount) / float64(stats.requestCount)
				stats.successRate = 1.0 - stats.errorRate
			}
		}

		u.strategyMetrics.lastEvalTime = time.Now()
		u.strategyMetrics.mu.Unlock()

		logger.Debugf("[Manager] 策略性能评估完成")
	}
}

// GetStrategyMetrics 获取策略性能指标
func (u *Manager) GetStrategyMetrics() map[string]any {
	if u.strategyMetrics == nil {
		return map[string]any{}
	}

	u.strategyMetrics.mu.RLock()
	defer u.strategyMetrics.mu.RUnlock()

	metrics := make(map[string]any)

	for strategy, stats := range u.strategyMetrics.strategyStats {
		if stats.requestCount == 0 {
			continue
		}

		avgLatency := float64(stats.totalLatency.Milliseconds()) / float64(stats.requestCount)
		successRate := float64(stats.successCount) / float64(stats.requestCount)

		metrics[strategy] = map[string]any{
			"request_count":  stats.requestCount,
			"success_count":  stats.successCount,
			"error_count":    stats.errorCount,
			"avg_latency_ms": avgLatency,
			"success_rate":   successRate,
			"error_rate":     stats.errorRate,
		}
	}

	return metrics
}

// GetPerformanceMetrics 获取性能指标
func (u *Manager) GetPerformanceMetrics() map[string]any {
	if u.strategyMetrics == nil {
		return map[string]any{}
	}

	u.strategyMetrics.mu.RLock()
	defer u.strategyMetrics.mu.RUnlock()

	totalRequests := int64(0)
	totalErrors := int64(0)
	totalLatency := time.Duration(0)

	for _, stats := range u.strategyMetrics.strategyStats {
		totalRequests += stats.requestCount
		totalErrors += stats.errorCount
		totalLatency += stats.totalLatency
	}

	var avgLatency float64
	if totalRequests > 0 {
		avgLatency = float64(totalLatency.Milliseconds()) / float64(totalRequests)
	}

	var errorRate float64
	if totalRequests > 0 {
		errorRate = float64(totalErrors) / float64(totalRequests) * 100
	}

	availability := 100.0 - errorRate

	return map[string]any{
		"total_requests": totalRequests,
		"total_errors":   totalErrors,
		"avg_latency_ms": avgLatency,
		"error_rate":     errorRate,
		"availability":   availability,
	}
}
