package upstream

import (
	"net"
	"net/http"
	"net/http/httptest"
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

// TestNetworkHealthCheckerProbeSuccess 测试成功的探测
func TestNetworkHealthCheckerProbeSuccess(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := &networkHealthChecker{
		probeInterval:    100 * time.Millisecond,
		failureThreshold: 2,
		probeTimeout:     5 * time.Second,
		probeURLs:        []string{server.URL},
		stopCh:           make(chan struct{}),
	}

	// 初始状态：健康
	checker.networkHealthy.Store(true)

	// 执行探测
	result := checker.probe()
	if !result {
		t.Error("Expected probe to succeed")
	}
}

// TestNetworkHealthCheckerProbeFail 测试失败的探测
func TestNetworkHealthCheckerProbeFail(t *testing.T) {
	checker := &networkHealthChecker{
		probeInterval:    100 * time.Millisecond,
		failureThreshold: 2,
		probeTimeout:     5 * time.Second,
		probeURLs:        []string{"http://localhost:65432/invalid"}, // 无法连接的地址
		stopCh:           make(chan struct{}),
	}

	// 执行探测
	result := checker.probe()
	if result {
		t.Error("Expected probe to fail")
	}
}

// TestNetworkHealthCheckerDualProbe 测试双URL探测（任一成功即可）
func TestNetworkHealthCheckerDualProbe(t *testing.T) {
	// 第一个URL失败
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failServer.Close()

	// 第二个URL成功
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer successServer.Close()

	checker := &networkHealthChecker{
		probeInterval:    100 * time.Millisecond,
		failureThreshold: 2,
		probeTimeout:     5 * time.Second,
		probeURLs:        []string{failServer.URL, successServer.URL},
		stopCh:           make(chan struct{}),
	}

	// 执行探测，应该成功
	result := checker.probe()
	if !result {
		t.Error("Expected probe to succeed when one URL succeeds")
	}
}

// TestNetworkHealthCheckerProbeURL 测试单个URL的探测
func TestNetworkHealthCheckerProbeURL(t *testing.T) {
	// 测试 200 OK
	server200 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server200.Close()

	checker := &networkHealthChecker{
		probeTimeout: 5 * time.Second,
		stopCh:       make(chan struct{}),
	}

	if !checker.probeURL(server200.URL) {
		t.Error("Expected 200 OK to be successful")
	}

	// 测试 204 No Content
	server204 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server204.Close()

	if !checker.probeURL(server204.URL) {
		t.Error("Expected 204 No Content to be successful")
	}

	// 测试 404 Not Found
	server404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server404.Close()

	if checker.probeURL(server404.URL) {
		t.Error("Expected 404 Not Found to fail")
	}

	// 测试连接超时
	if checker.probeURL("http://localhost:65432/invalid") {
		t.Error("Expected connection failure to fail")
	}
}

// TestNetworkHealthCheckerConsecutiveFailures 测试连续失败导致异常
func TestNetworkHealthCheckerConsecutiveFailures(t *testing.T) {
	checker := &networkHealthChecker{
		probeInterval:    10 * time.Millisecond,
		failureThreshold: 2,
		probeTimeout:     1 * time.Second,
		probeURLs:        []string{"http://localhost:65432/invalid"}, // 必然失败
		stopCh:           make(chan struct{}),
	}

	// 初始状态：健康
	checker.networkHealthy.Store(true)

	// 手动执行两次失败的探测
	checker.performProbe() // 第一次失败
	if !checker.networkHealthy.Load() {
		t.Error("Network should still be healthy after first failure")
	}

	if checker.consecutiveFailures != 1 {
		t.Errorf("Expected consecutive failures to be 1, got %d", checker.consecutiveFailures)
	}

	checker.performProbe() // 第二次失败，达到阈值
	if checker.networkHealthy.Load() {
		t.Error("Network should be marked as abnormal after reaching failure threshold")
	}

	if checker.consecutiveFailures != 2 {
		t.Errorf("Expected consecutive failures to be 2, got %d", checker.consecutiveFailures)
	}
}

// TestNetworkHealthCheckerRecovery 测试从异常状态恢复
func TestNetworkHealthCheckerRecovery(t *testing.T) {
	// 创建两个服务器：一个失败，一个成功
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failServer.Close()

	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer successServer.Close()

	checker := &networkHealthChecker{
		probeInterval:    10 * time.Millisecond,
		failureThreshold: 2,
		probeTimeout:     1 * time.Second,
		probeURLs:        []string{failServer.URL}, // 初始为失败的URL
		stopCh:           make(chan struct{}),
	}

	// 初始状态：健康
	checker.networkHealthy.Store(true)

	// 执行两次失败的探测，标记异常
	checker.performProbe()
	checker.performProbe()

	if checker.networkHealthy.Load() {
		t.Error("Network should be marked as abnormal")
	}

	// 现在更改为成功的URL
	checker.probeURLs = []string{successServer.URL}

	// 执行探测，应该恢复
	checker.performProbe()

	if !checker.networkHealthy.Load() {
		t.Error("Network should be recovered after successful probe")
	}

	if checker.consecutiveFailures != 0 {
		t.Errorf("Expected consecutive failures to be reset to 0, got %d", checker.consecutiveFailures)
	}
}

// TestNetworkHealthCheckerStartStop 测试启动和停止
func TestNetworkHealthCheckerStartStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := &networkHealthChecker{
		probeInterval:    50 * time.Millisecond,
		failureThreshold: 2,
		probeTimeout:     5 * time.Second,
		probeURLs:        []string{server.URL},
		stopCh:           make(chan struct{}),
	}

	checker.networkHealthy.Store(true)

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
	// 创建一个会延迟响应的服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // 延迟超过超时时间
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := &networkHealthChecker{
		probeTimeout: 100 * time.Millisecond, // 很短的超时
		stopCh:       make(chan struct{}),
	}

	// 探测应该超时失败
	result := checker.probeURL(server.URL)
	if result {
		t.Error("Expected probe to timeout and fail")
	}
}

// TestNetworkHealthCheckerConn 测试网络连接错误
func TestNetworkHealthCheckerConn(t *testing.T) {
	checker := &networkHealthChecker{
		probeTimeout: 5 * time.Second,
		stopCh:       make(chan struct{}),
	}

	// 使用一个无效的URL
	result := checker.probeURL("http://invalid.host.that.does.not.exist:99999/")
	if result {
		t.Error("Expected invalid host to fail")
	}
}

// TestNetworkHealthCheckerProbeURLWithLocalhost 测试本地端口探测
func TestNetworkHealthCheckerProbeURLWithLocalhost(t *testing.T) {
	// 使用 localhost:0 让系统分配一个可用的端口
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	checker := &networkHealthChecker{
		probeTimeout: 5 * time.Second,
		stopCh:       make(chan struct{}),
	}

	if !checker.probeURL(server.URL) {
		t.Error("Expected local probe to succeed")
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
