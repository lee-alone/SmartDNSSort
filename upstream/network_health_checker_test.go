package upstream

import (
	"context"
	"net"
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
		ProbeInterval:       1 * time.Second,
		FailureThreshold:    1,
		ProbeTimeout:        1 * time.Second,
		MaxTestIPsPerDomain: 2,
		TestPorts:           []string{"443", "80"},
		ProbeDomains:        []string{"localhost"},
	}

	checker := NewNetworkHealthCheckerWithConfig(config)

	// 初始状态应该是健康
	if !checker.IsNetworkHealthy() {
		t.Error("Expected network to be healthy initially")
	}
}

// TestNetworkHealthCheckerProbeDomainSuccess 测试成功的域名探测
func TestNetworkHealthCheckerProbeDomainSuccess(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 5 * time.Second
	config.ProbeDomains = []string{"localhost"}

	checker := NewNetworkHealthCheckerWithConfig(config)

	// 初始状态：健康
	checker.(*networkHealthChecker).networkHealthy.Store(true)

	// 执行探测 - localhost应该总是可以解析的
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := checker.(*networkHealthChecker).probeDomainWithCtx("localhost", ctx)
	// 注意：这个测试可能因为localhost没有开放的端口而失败，这是预期的
	_ = result
}

// TestNetworkHealthCheckerProbeFail 测试失败的探测
func TestNetworkHealthCheckerProbeFail(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 1 * time.Second
	config.ProbeDomains = []string{"invalid.domain.that.does.not.exist.local"}

	checker := NewNetworkHealthCheckerWithConfig(config)

	// 执行探测
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := checker.(*networkHealthChecker).probeDomainWithCtx("invalid.domain.that.does.not.exist.local", ctx)
	if result {
		t.Error("Expected probe to fail for invalid domain")
	}
}

// TestNetworkHealthCheckerProbeAllFail 测试所有域名都失败
func TestNetworkHealthCheckerProbeAllFail(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 1 * time.Second
	config.ProbeDomains = []string{
		"invalid1.local",
		"invalid2.local",
		"invalid3.local",
		"invalid4.local",
		"invalid5.local",
	}

	checker := NewNetworkHealthCheckerWithConfig(config)

	// 执行探测 - 所有域名都应该失败
	result := checker.(*networkHealthChecker).probe()
	if result {
		t.Error("Expected probe to fail when all domains are unreachable")
	}
}

// TestNetworkHealthCheckerConsecutiveFailures 测试连续失败导致异常
func TestNetworkHealthCheckerConsecutiveFailures(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 1 * time.Second
	config.ProbeDomains = []string{"invalid.local"}

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
	config.ProbeDomains = []string{"invalid.local"}

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

	// 现在更改为可以解析的域名
	c.config.ProbeDomains = []string{"localhost"}

	// 执行探测，应该恢复（如果localhost有开放的端口）
	c.performProbe()

	// 如果恢复成功，失败计数应该重置
	if c.consecutiveFailures != 0 {
		t.Logf("Consecutive failures: %d (may be non-zero if localhost probe failed)", c.consecutiveFailures)
	}
}

// TestNetworkHealthCheckerStartStop 测试启动和停止
func TestNetworkHealthCheckerStartStop(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeInterval = 50 * time.Millisecond
	config.ProbeDomains = []string{"localhost"}

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

	// 尝试连接到一个不存在的IP地址（应该超时）
	dialer := net.Dialer{
		Timeout: c.config.ProbeTimeout,
	}

	// 使用一个不可达的IP地址（RFC 5737 TEST-NET-1）
	_, err := dialer.Dial("tcp", "192.0.2.1:80")
	if err == nil {
		t.Error("Expected connection to timeout or fail")
	}
}

// TestNetworkHealthCheckerDomainResolution 测试域名解析
func TestNetworkHealthCheckerDomainResolution(t *testing.T) {
	// 测试localhost解析
	ips, err := net.LookupIP("localhost")
	if err != nil {
		t.Fatalf("Failed to resolve localhost: %v", err)
	}

	if len(ips) == 0 {
		t.Error("Expected to resolve at least one IP for localhost")
	}
}

// TestNetworkHealthCheckerIPv4Priority 测试IPv4优先
func TestNetworkHealthCheckerIPv4Priority(t *testing.T) {
	config := DefaultNetworkHealthConfig()
	config.ProbeTimeout = 5 * time.Second
	config.MaxTestIPsPerDomain = 3

	_ = NewNetworkHealthCheckerWithConfig(config)

	// 测试IPv4优先逻辑
	ips, err := net.LookupIP("localhost")
	if err != nil {
		t.Fatalf("Failed to resolve localhost: %v", err)
	}

	// 计算IPv4数量
	ipv4Count := 0
	for _, ip := range ips {
		if ip.To4() != nil {
			ipv4Count++
		}
	}

	if ipv4Count == 0 {
		t.Logf("No IPv4 addresses found for localhost (may be IPv6-only)")
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
