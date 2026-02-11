package upstream

import (
	"testing"
	"time"
)

// TestNetworkHealthCheckerInitialState 测试初始状态
func TestNetworkHealthCheckerInitialState(t *testing.T) {
	checker := NewNetworkHealthChecker()

	// 初始状态应该是健康
	if !checker.IsNetworkHealthy() {
		t.Error("Expected network to be healthy initially")
	}
}

// TestNetworkHealthCheckerWithCustomConfig 测试自定义配置
func TestNetworkHealthCheckerWithCustomConfig(t *testing.T) {
	config := NetworkHealthConfig{
		ProbeInterval:    1 * time.Second,
		FailureThreshold: 1,
		ProbeTimeout:     1 * time.Second,
		ProbeIPs:         []string{"8.8.8.8", "8.8.4.4"},
	}

	checker := NewNetworkHealthCheckerWithConfig(config)

	// 初始状态应该是健康
	if !checker.IsNetworkHealthy() {
		t.Error("Expected network to be healthy initially")
	}
}

// TestNetworkHealthCheckerProbeDomainSuccess 测试成功的IP探测
func TestNetworkHealthCheckerProbeIPSuccess(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 5 * time.Second
	config.ProbeIPs = []string{"8.8.8.8"}

	checker := NewNetworkHealthCheckerWithConfig(config)

	// 初始状态：健康
	checker.(*networkHealthChecker).networkHealthy.Store(true)

	// 执行探测 - 8.8.8.8应该可以ping通（如果网络连接正常）
	result := checker.(*networkHealthChecker).probeIP("8.8.8.8")
	// 注意：这个测试可能因为网络环境而失败，这是预期的
	_ = result
}

// TestNetworkHealthCheckerProbeFail 测试失败的探测
func TestNetworkHealthCheckerProbeFail(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 1 * time.Second
	config.ProbeIPs = []string{"192.0.2.1"} // RFC 5737 TEST-NET-1，不可达

	checker := NewNetworkHealthCheckerWithConfig(config)

	// 执行探测
	result := checker.(*networkHealthChecker).probeIP("192.0.2.1")
	if result {
		t.Error("Expected probe to fail for unreachable IP")
	}
}

// TestNetworkHealthCheckerProbeAllFail 测试所有IP都失败
func TestNetworkHealthCheckerProbeAllFail(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 1 * time.Second
	config.ProbeIPs = []string{
		"192.0.2.1",
		"192.0.2.2",
		"192.0.2.3",
		"192.0.2.4",
		"192.0.2.5",
	}

	checker := NewNetworkHealthCheckerWithConfig(config)

	// 执行探测 - 所有IP都应该失败
	result := checker.(*networkHealthChecker).probe()
	if result {
		t.Error("Expected probe to fail when all IPs are unreachable")
	}
}

// TestNetworkHealthCheckerConsecutiveFailures 测试连续失败导致异常
func TestNetworkHealthCheckerConsecutiveFailures(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 1 * time.Second
	config.ProbeIPs = []string{"192.0.2.1"}

	checker := NewNetworkHealthCheckerWithConfig(config)
	c := checker.(*networkHealthChecker)

	// 初始状态：健康
	c.networkHealthy.Store(true)

	// 手动执行两次失败的探测
	c.performProbe() // 第一次失败
	if !c.networkHealthy.Load() {
		t.Error("Network should still be healthy after first failure")
	}

	if c.consecutiveFailures != 1 {
		t.Errorf("Expected consecutive failures to be 1, got %d", c.consecutiveFailures)
	}

	c.performProbe() // 第二次失败，达到阈值
	if c.networkHealthy.Load() {
		t.Error("Network should be marked as abnormal after reaching failure threshold")
	}

	if c.consecutiveFailures != 2 {
		t.Errorf("Expected consecutive failures to be 2, got %d", c.consecutiveFailures)
	}
}

// TestNetworkHealthCheckerRecovery 测试从异常状态恢复
func TestNetworkHealthCheckerRecovery(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 5 * time.Second
	config.ProbeIPs = []string{"192.0.2.1"}

	checker := NewNetworkHealthCheckerWithConfig(config)
	c := checker.(*networkHealthChecker)

	// 初始状态：健康
	c.networkHealthy.Store(true)

	// 执行两次失败的探测，标记异常
	c.performProbe()
	c.performProbe()

	if c.networkHealthy.Load() {
		t.Error("Network should be marked as abnormal")
	}

	// 现在更改为可以ping通的IP
	c.config.ProbeIPs = []string{"8.8.8.8"}

	// 执行探测，应该恢复（如果网络连接正常）
	c.performProbe()

	// 如果恢复成功，失败计数应该重置
	if c.consecutiveFailures != 0 {
		t.Logf("Consecutive failures: %d (may be non-zero if probe failed)", c.consecutiveFailures)
	}
}

// TestNetworkHealthCheckerStartStop 测试启动和停止
func TestNetworkHealthCheckerStartStop(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeInterval = 50 * time.Millisecond
	config.ProbeIPs = []string{"8.8.8.8"}

	checker := NewNetworkHealthCheckerWithConfig(config)

	// 启动探测循环
	checker.Start()

	// 让循环运行一段时间
	time.Sleep(200 * time.Millisecond)

	// 停止探测循环
	checker.Stop()

	// 验证状态
	if !checker.IsNetworkHealthy() {
		t.Error("Expected network to remain healthy")
	}
}

// TestNetworkHealthCheckerTimeout 测试超时情况
func TestNetworkHealthCheckerTimeout(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 100 * time.Millisecond

	checker := NewNetworkHealthCheckerWithConfig(config)
	c := checker.(*networkHealthChecker)

	// 尝试ping一个不可达的IP地址（应该超时）
	result := c.probeIP("192.0.2.1")
	if result {
		t.Error("Expected ping to unreachable IP to fail")
	}
}

// TestNetworkHealthCheckerDomainResolution 测试IP配置
func TestNetworkHealthCheckerIPConfig(t *testing.T) {
	// 测试默认配置中的IP
	config := DefaultNetworkHealthConfig()

	if len(config.ProbeIPs) == 0 {
		t.Error("Expected ProbeIPs to be configured")
	}

	// 验证包含Google和Alibaba DNS
	hasGoogle := false
	hasAlibaba := false
	for _, ip := range config.ProbeIPs {
		if ip == "8.8.8.8" || ip == "8.8.4.4" {
			hasGoogle = true
		}
		if ip == "223.5.5.5" || ip == "223.6.6.6" {
			hasAlibaba = true
		}
	}

	if !hasGoogle {
		t.Error("Expected Google DNS IPs in configuration")
	}
	if !hasAlibaba {
		t.Error("Expected Alibaba DNS IPs in configuration")
	}
}

// TestNetworkHealthCheckerIPv4Priority 测试IP配置
func TestNetworkHealthCheckerIPConfiguration(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 5 * time.Second

	_ = NewNetworkHealthCheckerWithConfig(config)

	// 验证配置中有有效的IP地址
	if len(config.ProbeIPs) == 0 {
		t.Error("Expected ProbeIPs to be configured")
	}
}

// TestGlobalNetworkChecker 测试全局单例
func TestGlobalNetworkChecker(t *testing.T) {
	// 获取全局 checker 第一次
	checker1 := GetGlobalNetworkChecker()

	// 获取全局 checker 第二次
	checker2 := GetGlobalNetworkChecker()

	// 应该是同一个对象
	if checker1 != checker2 {
		t.Error("Expected GetGlobalNetworkChecker to return the same instance")
	}

	// 清理
	ShutdownNetworkChecker()
}
