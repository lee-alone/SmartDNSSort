package upstream

import (
	"testing"
	"time"
)

// MockNetworkHealthChecker 是用于测试的网络健康检查器模拟
type MockNetworkHealthChecker struct {
	healthy bool
}

func (m *MockNetworkHealthChecker) IsNetworkHealthy() bool {
	return m.healthy
}

func (m *MockNetworkHealthChecker) Start() {
	// 模拟实现，不做任何事
}

func (m *MockNetworkHealthChecker) Stop() {
	// 模拟实现，不做任何事
}

// TestMarkSuccessFrozenWhenNetworkUnhealthy 测试网络异常时冻结成功统计
func TestMarkSuccessFrozenWhenNetworkUnhealthy(t *testing.T) {
	mockChecker := &MockNetworkHealthChecker{healthy: false}

	health := NewServerHealth("test:53", DefaultHealthCheckConfig(), &StatsConfig{
		UpstreamStatsBucketMinutes: 10,
		UpstreamStatsRetentionDays: 90,
	}, mockChecker)

	// 初始状态
	if health.consecutiveSuccesses != 0 {
		t.Errorf("Expected initial consecutive successes to be 0, got %d", health.consecutiveSuccesses)
	}

	if health.totalSuccesses != 0 {
		t.Errorf("Expected initial total successes to be 0, got %d", health.totalSuccesses)
	}

	// 调用 MarkSuccess 时网络不健康，应该冻结统计
	health.MarkSuccess()

	// 验证统计没有更新
	if health.consecutiveSuccesses != 0 {
		t.Errorf("Expected consecutive successes to remain 0 when network is unhealthy, got %d", health.consecutiveSuccesses)
	}

	if health.totalSuccesses != 0 {
		t.Errorf("Expected total successes to remain 0 when network is unhealthy, got %d", health.totalSuccesses)
	}
}

// TestMarkFailureFrozenWhenNetworkUnhealthy 测试网络异常时冻结失败统计
func TestMarkFailureFrozenWhenNetworkUnhealthy(t *testing.T) {
	mockChecker := &MockNetworkHealthChecker{healthy: false}

	health := NewServerHealth("test:53", DefaultHealthCheckConfig(), &StatsConfig{
		UpstreamStatsBucketMinutes: 10,
		UpstreamStatsRetentionDays: 90,
	}, mockChecker)

	// 初始状态
	if health.consecutiveFailures != 0 {
		t.Errorf("Expected initial consecutive failures to be 0, got %d", health.consecutiveFailures)
	}

	if health.totalFailures != 0 {
		t.Errorf("Expected initial total failures to be 0, got %d", health.totalFailures)
	}

	// 调用 MarkFailure 时网络不健康，应该冻结统计
	health.MarkFailure()

	// 验证统计没有更新
	if health.consecutiveFailures != 0 {
		t.Errorf("Expected consecutive failures to remain 0 when network is unhealthy, got %d", health.consecutiveFailures)
	}

	if health.totalFailures != 0 {
		t.Errorf("Expected total failures to remain 0 when network is unhealthy, got %d", health.totalFailures)
	}
}

// TestCircuitBreakerFrozenWhenNetworkUnhealthy 测试网络异常时熔断计数被冻结
func TestCircuitBreakerFrozenWhenNetworkUnhealthy(t *testing.T) {
	mockChecker := &MockNetworkHealthChecker{healthy: false}

	health := NewServerHealth("test:53", DefaultHealthCheckConfig(), &StatsConfig{
		UpstreamStatsBucketMinutes: 10,
		UpstreamStatsRetentionDays: 90,
	}, mockChecker)

	// 设置初始状态为健康
	health.status = HealthStatusHealthy

	// 连续调用 MarkFailure，即使达到熔断阈值，也不应该进入熔断状态
	for i := 0; i < 10; i++ {
		health.MarkFailure()
	}

	// 验证状态没有变化
	if health.status != HealthStatusHealthy {
		t.Errorf("Expected status to remain healthy when network is unhealthy, got %v", health.status)
	}

	if health.consecutiveFailures != 0 {
		t.Errorf("Expected consecutive failures to remain 0 when network is unhealthy, got %d", health.consecutiveFailures)
	}
}

// TestStatisticsUpdateWhenNetworkHealthy 测试网络正常时统计正常更新
func TestStatisticsUpdateWhenNetworkHealthy(t *testing.T) {
	mockChecker := &MockNetworkHealthChecker{healthy: true}

	health := NewServerHealth("test:53", DefaultHealthCheckConfig(), &StatsConfig{
		UpstreamStatsBucketMinutes: 10,
		UpstreamStatsRetentionDays: 90,
	}, mockChecker)

	// 调用 MarkSuccess
	health.MarkSuccess()

	// 验证统计更新
	if health.consecutiveSuccesses != 1 {
		t.Errorf("Expected consecutive successes to be 1, got %d", health.consecutiveSuccesses)
	}

	if health.totalSuccesses != 1 {
		t.Errorf("Expected total successes to be 1, got %d", health.totalSuccesses)
	}

	// 调用 MarkFailure
	health.MarkFailure()

	// 验证开始计数失败
	if health.consecutiveFailures != 1 {
		t.Errorf("Expected consecutive failures to be 1, got %d", health.consecutiveFailures)
	}

	if health.consecutiveSuccesses != 0 {
		t.Errorf("Expected consecutive successes to be reset to 0, got %d", health.consecutiveSuccesses)
	}

	if health.totalFailures != 1 {
		t.Errorf("Expected total failures to be 1, got %d", health.totalFailures)
	}
}

// TestNetworkSwitchFromHealthyToUnhealthy 测试从网络正常切换到异常
func TestNetworkSwitchFromHealthyToUnhealthy(t *testing.T) {
	mockChecker := &MockNetworkHealthChecker{healthy: true}

	health := NewServerHealth("test:53", DefaultHealthCheckConfig(), &StatsConfig{
		UpstreamStatsBucketMinutes: 10,
		UpstreamStatsRetentionDays: 90,
	}, mockChecker)

	// 网络正常时，记录一些成功
	health.MarkSuccess()
	health.MarkSuccess()
	health.MarkSuccess()

	if health.totalSuccesses != 3 {
		t.Errorf("Expected total successes to be 3, got %d", health.totalSuccesses)
	}

	// 切换到网络异常
	mockChecker.healthy = false

	// 再调用 MarkSuccess，应该被冻结
	health.MarkSuccess()

	// 验证统计没有更新
	if health.totalSuccesses != 3 {
		t.Errorf("Expected total successes to remain 3, got %d", health.totalSuccesses)
	}

	if health.consecutiveSuccesses != 3 {
		t.Errorf("Expected consecutive successes to remain 3, got %d", health.consecutiveSuccesses)
	}
}

// TestNetworkSwitchFromUnhealthyToHealthy 测试从网络异常切换到正常
func TestNetworkSwitchFromUnhealthyToHealthy(t *testing.T) {
	mockChecker := &MockNetworkHealthChecker{healthy: false}

	health := NewServerHealth("test:53", DefaultHealthCheckConfig(), &StatsConfig{
		UpstreamStatsBucketMinutes: 10,
		UpstreamStatsRetentionDays: 90,
	}, mockChecker)

	// 网络异常时，尝试标记成功（应该被冻结）
	health.MarkSuccess()
	health.MarkSuccess()

	if health.totalSuccesses != 0 {
		t.Errorf("Expected total successes to be 0, got %d", health.totalSuccesses)
	}

	// 切换到网络正常
	mockChecker.healthy = true

	// 再调用 MarkSuccess，应该正常更新
	health.MarkSuccess()

	if health.totalSuccesses != 1 {
		t.Errorf("Expected total successes to be 1, got %d", health.totalSuccesses)
	}

	if health.consecutiveSuccesses != 1 {
		t.Errorf("Expected consecutive successes to be 1, got %d", health.consecutiveSuccesses)
	}
}

// TestShouldSkipTemporarilyUnaffectedByNetworkHealth 测试网络状态不影响 ShouldSkipTemporarily
func TestShouldSkipTemporarilyUnaffectedByNetworkHealth(t *testing.T) {
	mockChecker := &MockNetworkHealthChecker{healthy: false}

	health := NewServerHealth("test:53", DefaultHealthCheckConfig(), &StatsConfig{
		UpstreamStatsBucketMinutes: 10,
		UpstreamStatsRetentionDays: 90,
	}, mockChecker)

	// 即使网络状态不健康，熔断逻辑也应该继续工作
	// ShouldSkipTemporarily 不应该被网络状态影响

	// 验证初始状态不跳过
	if health.ShouldSkipTemporarily() {
		t.Error("Expected ShouldSkipTemporarily to return false for healthy status")
	}

	// 手动设置为熔断状态（绕过 MarkFailure 的冻结）
	health.mu.Lock()
	health.status = HealthStatusUnhealthy
	health.circuitBreakerStartTime = health.circuitBreakerStartTime.Add(-100 * time.Second) // 已超过恢复时间
	health.mu.Unlock()

	// 应该不跳过（进入半开状态）
	if health.ShouldSkipTemporarily() {
		t.Error("Expected ShouldSkipTemporarily to return false after circuit breaker timeout")
	}
}
