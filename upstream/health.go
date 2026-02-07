package upstream

import (
	"sync"
	"time"
)

// StatsConfig 统计配置（用于上游统计时间桶）
type StatsConfig struct {
	UpstreamStatsBucketMinutes int
	UpstreamStatsRetentionDays int
}

// HealthStatus 服务器健康状态
type HealthStatus int

const (
	// HealthStatusHealthy 健康状态
	HealthStatusHealthy HealthStatus = iota
	// HealthStatusDegraded 降级状态（部分失败）
	HealthStatusDegraded
	// HealthStatusUnhealthy 不健康状态（熔断）
	HealthStatusUnhealthy
)

// HealthCheck 健康检查配置
type HealthCheckConfig struct {
	// 连续失败多少次后进入降级状态
	FailureThreshold int
	// 连续失败多少次后进入熔断状态
	CircuitBreakerThreshold int
	// 熔断后多久尝试恢复（秒）
	CircuitBreakerTimeout int
	// 成功多少次后从降级/熔断状态恢复
	SuccessThreshold int
}

// DefaultHealthCheckConfig 默认健康检查配置
func DefaultHealthCheckConfig() *HealthCheckConfig {
	return &HealthCheckConfig{
		FailureThreshold:        3,  // 连续失败 3 次进入降级
		CircuitBreakerThreshold: 5,  // 连续失败 5 次进入熔断
		CircuitBreakerTimeout:   30, // 熔断 30 秒后尝试恢复
		SuccessThreshold:        2,  // 连续成功 2 次恢复健康
	}
}

// ServerHealth 服务器健康状态管理
type ServerHealth struct {
	mu sync.RWMutex

	// 服务器地址
	address string

	// 当前健康状态
	status HealthStatus

	// 连续失败次数
	consecutiveFailures int

	// 连续成功次数
	consecutiveSuccesses int

	// 最后失败时间
	lastFailureTime time.Time

	// 熔断开始时间
	circuitBreakerStartTime time.Time

	// 连续恢复尝试次数（用于指数退避）
	consecutiveRecoveryAttempts int

	// 配置
	config *HealthCheckConfig

	// 平均延迟（使用 EWMA 计算）
	latency time.Duration

	// EWMA 的 alpha 因子，例如 0.2
	latencyAlpha float64

	// 累计成功次数
	totalSuccesses int64

	// 累计失败次数
	totalFailures int64

	// 新增：上游统计时间桶追踪器
	statsTracker *UpstreamStatsTracker
}

// NewServerHealth 创建服务器健康状态管理器
// statsConfig: 统计配置，用于动态计算桶数量
func NewServerHealth(address string, config *HealthCheckConfig, statsConfig *StatsConfig) *ServerHealth {
	if config == nil {
		config = DefaultHealthCheckConfig()
	}

	// 计算桶数量
	bucketMinutes := 10 // 默认 10 分钟
	retentionDays := 90 // 默认 90 天

	if statsConfig != nil {
		if statsConfig.UpstreamStatsBucketMinutes > 0 {
			bucketMinutes = statsConfig.UpstreamStatsBucketMinutes
		}
		if statsConfig.UpstreamStatsRetentionDays > 0 {
			retentionDays = statsConfig.UpstreamStatsRetentionDays
		}
	}

	bucketCount := (retentionDays * 24 * 60) / bucketMinutes
	if bucketCount < 1 {
		bucketCount = 1
	}

	return &ServerHealth{
		address:      address,
		status:       HealthStatusHealthy,
		config:       config,
		latency:      200 * time.Millisecond, // 初始延迟设为 200ms 的默认值
		latencyAlpha: 0.2,                    // EWMA 的 alpha 因子
		statsTracker: NewUpstreamStatsTracker(address, time.Duration(bucketMinutes)*time.Minute, bucketCount),
	}
}

// MarkSuccess 标记查询成功
func (h *ServerHealth) MarkSuccess() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.consecutiveSuccesses++
	h.consecutiveFailures = 0
	h.totalSuccesses++ // 增加累计成功计数
	h.statsTracker.RecordSuccess()

	// 如果连续成功达到阈值，恢复健康状态
	if h.consecutiveSuccesses >= h.config.SuccessThreshold {
		if h.status != HealthStatusHealthy {
			h.status = HealthStatusHealthy
			h.consecutiveSuccesses = 0
			h.consecutiveRecoveryAttempts = 0 // 重置恢复尝试计数
		}
	}
}

// MarkFailure 标记查询失败
func (h *ServerHealth) MarkFailure() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.consecutiveFailures++
	h.consecutiveSuccesses = 0
	h.lastFailureTime = time.Now()
	h.totalFailures++ // 增加累计失败计数
	h.statsTracker.RecordFailure()

	// 根据失败次数更新状态
	if h.consecutiveFailures >= h.config.CircuitBreakerThreshold {
		if h.status != HealthStatusUnhealthy {
			h.status = HealthStatusUnhealthy
			h.circuitBreakerStartTime = time.Now()
			h.consecutiveRecoveryAttempts++ // 增加恢复尝试计数
		}
	} else if h.consecutiveFailures >= h.config.FailureThreshold {
		if h.status == HealthStatusHealthy {
			h.status = HealthStatusDegraded
		}
	}
}

// MarkTimeout 标记查询超时，增加延迟惩罚但不触发熔断计数
func (h *ServerHealth) MarkTimeout(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.consecutiveSuccesses = 0
	h.lastFailureTime = time.Now()

	// 更新延迟记录，使该服务器在排序中靠后
	if d <= 0 {
		d = 1 * time.Second // 默认惩罚
	}

	if h.latency == 0 {
		h.latency = d
	} else {
		// EWMA: 增加延迟权重，使其优先级降低频率
		newLatency := time.Duration(h.latencyAlpha*float64(d) + (1.0-h.latencyAlpha)*float64(h.latency))
		h.latency = newLatency
	}
}

// ShouldSkipTemporarily 判断是否应该临时跳过此服务器
func (h *ServerHealth) ShouldSkipTemporarily() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 如果处于熔断状态，检查是否可以尝试恢复
	if h.status == HealthStatusUnhealthy {
		elapsed := time.Since(h.circuitBreakerStartTime)

		// 指数退避：第一次 10s，第二次 20s，第三次 40s，以此类推
		// backoffDuration = 10 * 2^(consecutiveRecoveryAttempts)
		backoffDuration := time.Duration(10*(1<<uint(h.consecutiveRecoveryAttempts))) * time.Second

		if elapsed < backoffDuration {
			// 仍在退避期内，跳过
			return true
		}
		// 退避超时，允许尝试（半开状态）
		return false
	}

	// 健康或降级状态不跳过
	return false
}

// GetStatus 获取当前健康状态
func (h *ServerHealth) GetStatus() HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.status
}

// GetStats 获取健康统计信息
func (h *ServerHealth) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	statusStr := "healthy"
	switch h.status {
	case HealthStatusDegraded:
		statusStr = "degraded"
	case HealthStatusUnhealthy:
		statusStr = "unhealthy"
	}

	stats := map[string]interface{}{
		"address":               h.address,
		"status":                statusStr,
		"consecutive_failures":  h.consecutiveFailures,
		"consecutive_successes": h.consecutiveSuccesses,
		"success":               h.totalSuccesses, // 累计成功次数
		"failure":               h.totalFailures,  // 累计失败次数
	}

	if !h.lastFailureTime.IsZero() {
		stats["last_failure"] = h.lastFailureTime.Format(time.RFC3339)
		stats["seconds_since_last_failure"] = int(time.Since(h.lastFailureTime).Seconds())
	}

	if h.status == HealthStatusUnhealthy {
		elapsed := time.Since(h.circuitBreakerStartTime)
		backoffDuration := time.Duration(10*(1<<uint(h.consecutiveRecoveryAttempts))) * time.Second
		remaining := backoffDuration - elapsed
		if remaining < 0 {
			remaining = 0
		}
		stats["circuit_breaker_remaining_seconds"] = int(remaining.Seconds())
		stats["recovery_attempts"] = h.consecutiveRecoveryAttempts
		stats["backoff_duration_seconds"] = int(backoffDuration.Seconds())
	}

	return stats
}

// GetStatsWithTimeRange 获取指定时间范围的统计信息
func (h *ServerHealth) GetStatsWithTimeRange(days int) map[string]interface{} {
	if days < 1 || days > 90 {
		days = 7
	}

	startTime := time.Now().AddDate(0, 0, -days)
	success, failure := h.statsTracker.Aggregate(startTime)
	total := success + failure

	h.mu.RLock()
	defer h.mu.RUnlock()

	successRate := 0.0
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}

	statusStr := "healthy"
	switch h.status {
	case HealthStatusDegraded:
		statusStr = "degraded"
	case HealthStatusUnhealthy:
		statusStr = "unhealthy"
	}

	return map[string]interface{}{
		"address":      h.address,
		"success":      success,
		"failure":      failure,
		"total":        total,
		"success_rate": successRate,
		"status":       statusStr,
		"latency_ms":   h.latency.Seconds() * 1000,
	}
}

// Reset 重置健康状态（用于测试或手动恢复）
func (h *ServerHealth) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.status = HealthStatusHealthy
	h.consecutiveFailures = 0
	h.consecutiveSuccesses = 0
	h.lastFailureTime = time.Time{}
	h.circuitBreakerStartTime = time.Time{}
	h.totalSuccesses = 0 // 重置累计成功
	h.totalFailures = 0  // 重置累计失败
	h.statsTracker.Reset()
}

// ClearStats 清除统计数据（成功/失败计数），但保留健康状态
func (h *ServerHealth) ClearStats() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.totalSuccesses = 0
	h.totalFailures = 0
}

// RecordLatency 记录一次成功的查询延迟，并更新 EWMA 值
func (h *ServerHealth) RecordLatency(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.latency == 0 { // 首次记录
		h.latency = d
	} else {
		// EWMA 公式: new_avg = alpha * new_value + (1 - alpha) * old_avg
		newLatency := time.Duration(h.latencyAlpha*float64(d) + (1.0-h.latencyAlpha)*float64(h.latency))
		h.latency = newLatency
	}
}

// GetLatency 获取当前的平均延迟（EWMA）
func (h *ServerHealth) GetLatency() time.Duration {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.latency
}

// Stop 停止统计追踪器
func (h *ServerHealth) Stop() {
	h.statsTracker.Stop()
}
