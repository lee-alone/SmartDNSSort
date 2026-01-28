package upstream

import (
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

// GetAdaptiveRacingDelay 获取自适应竞速延迟
func (u *Manager) GetAdaptiveRacingDelay() time.Duration {
	avgLatency := u.GetAverageLatency()

	// 竞速延迟 = 平均延迟的 10%
	delay := avgLatency / 10

	// 限制范围：50-200ms
	if delay < 50*time.Millisecond {
		delay = 50 * time.Millisecond
	}
	if delay > 200*time.Millisecond {
		delay = 200 * time.Millisecond
	}

	return delay
}

// GetAdaptiveSequentialTimeout 获取自适应顺序查询超时
func (u *Manager) GetAdaptiveSequentialTimeout() time.Duration {
	avgLatency := u.GetAverageLatency()

	// 顺序查询超时 = 平均延迟 * 1.5
	timeout := time.Duration(float64(avgLatency) * 1.5)

	// 限制范围：500ms-2s
	if timeout < 500*time.Millisecond {
		timeout = 500 * time.Millisecond
	}
	if timeout > 2*time.Second {
		timeout = 2 * time.Second
	}

	return timeout
}

// GetDynamicParamStats 获取动态参数优化的统计信息
func (u *Manager) GetDynamicParamStats() map[string]interface{} {
	if u.dynamicParamOptimization == nil {
		return map[string]interface{}{}
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

	stats.totalLatency += latency
	stats.requestCount++

	if success {
		stats.successCount++
	} else {
		stats.errorCount++
	}
}

// SelectOptimalStrategy 基于历史性能数据选择最优策略
func (u *Manager) SelectOptimalStrategy() string {
	if u.strategyMetrics == nil {
		return u.strategy
	}

	u.strategyMetrics.mu.RLock()
	defer u.strategyMetrics.mu.RUnlock()

	var bestStrategy string
	var bestScore float64

	for strategy, stats := range u.strategyMetrics.strategyStats {
		// 需要足够的样本数据
		if stats.requestCount < 10 {
			continue
		}

		// 计算综合评分
		// 评分 = 成功率 * 100 - 平均延迟 / 10
		successRate := float64(stats.successCount) / float64(stats.requestCount)
		avgLatency := float64(stats.totalLatency.Milliseconds()) / float64(stats.requestCount)
		score := successRate*100 - avgLatency/10

		if score > bestScore {
			bestScore = score
			bestStrategy = strategy
		}
	}

	// 如果没有找到最优策略，返回当前策略
	if bestStrategy == "" {
		return u.strategy
	}

	return bestStrategy
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
func (u *Manager) GetStrategyMetrics() map[string]interface{} {
	if u.strategyMetrics == nil {
		return map[string]interface{}{}
	}

	u.strategyMetrics.mu.RLock()
	defer u.strategyMetrics.mu.RUnlock()

	metrics := make(map[string]interface{})

	for strategy, stats := range u.strategyMetrics.strategyStats {
		if stats.requestCount == 0 {
			continue
		}

		avgLatency := float64(stats.totalLatency.Milliseconds()) / float64(stats.requestCount)
		successRate := float64(stats.successCount) / float64(stats.requestCount)

		metrics[strategy] = map[string]interface{}{
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
func (u *Manager) GetPerformanceMetrics() map[string]interface{} {
	if u.strategyMetrics == nil {
		return map[string]interface{}{}
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

	return map[string]interface{}{
		"total_requests": totalRequests,
		"total_errors":   totalErrors,
		"avg_latency_ms": avgLatency,
		"error_rate":     errorRate,
		"availability":   availability,
	}
}
