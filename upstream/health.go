package upstream

import (
	"sync"
	"time"
)

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

	// 配置
	config *HealthCheckConfig
}

// NewServerHealth 创建服务器健康状态管理器
func NewServerHealth(address string, config *HealthCheckConfig) *ServerHealth {
	if config == nil {
		config = DefaultHealthCheckConfig()
	}

	return &ServerHealth{
		address: address,
		status:  HealthStatusHealthy,
		config:  config,
	}
}

// MarkSuccess 标记查询成功
func (h *ServerHealth) MarkSuccess() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.consecutiveSuccesses++
	h.consecutiveFailures = 0

	// 如果连续成功达到阈值，恢复健康状态
	if h.consecutiveSuccesses >= h.config.SuccessThreshold {
		if h.status != HealthStatusHealthy {
			h.status = HealthStatusHealthy
			h.consecutiveSuccesses = 0
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

	// 根据失败次数更新状态
	if h.consecutiveFailures >= h.config.CircuitBreakerThreshold {
		if h.status != HealthStatusUnhealthy {
			h.status = HealthStatusUnhealthy
			h.circuitBreakerStartTime = time.Now()
		}
	} else if h.consecutiveFailures >= h.config.FailureThreshold {
		if h.status == HealthStatusHealthy {
			h.status = HealthStatusDegraded
		}
	}
}

// ShouldSkipTemporarily 判断是否应该临时跳过此服务器
func (h *ServerHealth) ShouldSkipTemporarily() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 如果处于熔断状态，检查是否可以尝试恢复
	if h.status == HealthStatusUnhealthy {
		elapsed := time.Since(h.circuitBreakerStartTime).Seconds()
		if elapsed < float64(h.config.CircuitBreakerTimeout) {
			// 仍在熔断期内，跳过
			return true
		}
		// 熔断超时，允许尝试（半开状态）
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
	}

	if !h.lastFailureTime.IsZero() {
		stats["last_failure"] = h.lastFailureTime.Format(time.RFC3339)
		stats["seconds_since_last_failure"] = int(time.Since(h.lastFailureTime).Seconds())
	}

	if h.status == HealthStatusUnhealthy {
		elapsed := time.Since(h.circuitBreakerStartTime).Seconds()
		remaining := float64(h.config.CircuitBreakerTimeout) - elapsed
		if remaining < 0 {
			remaining = 0
		}
		stats["circuit_breaker_remaining_seconds"] = int(remaining)
	}

	return stats
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
}
