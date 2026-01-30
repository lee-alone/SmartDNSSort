package recursor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNewManager 测试创建新的 Manager
func TestNewManager(t *testing.T) {
	port := 5353
	mgr := NewManager(port)

	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}

	if mgr.GetPort() != port {
		t.Errorf("Expected port %d, got %d", port, mgr.GetPort())
	}

	if mgr.IsEnabled() {
		t.Error("New manager should not be enabled")
	}

	expectedAddr := fmt.Sprintf("127.0.0.1:%d", port)
	if mgr.GetAddress() != expectedAddr {
		t.Errorf("Expected address %s, got %s", expectedAddr, mgr.GetAddress())
	}
}

// TestGenerateConfig 测试配置文件生成
func TestGenerateConfig(t *testing.T) {
	port := 5353
	mgr := NewManager(port)

	configPath, err := mgr.generateConfig()
	if err != nil {
		t.Fatalf("Failed to generate config: %v", err)
	}

	defer os.Remove(configPath)

	// 验证文件存在
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("Config file not found: %v", err)
	}

	// 验证文件内容
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	contentStr := string(content)

	// 检查关键配置项
	checks := []string{
		fmt.Sprintf("port: %d", port),
		"do-ip4: yes",
		"do-udp: yes",
		"do-tcp: yes",
		"interface: 127.0.0.1",
		"access-control: 127.0.0.1 allow",
	}

	for _, check := range checks {
		if !contains(contentStr, check) {
			t.Errorf("Config missing expected content: %s", check)
		}
	}
}

// TestGetUnboundConfigDir 测试获取配置目录
func TestGetUnboundConfigDir(t *testing.T) {
	dir, err := GetUnboundConfigDir()
	if err != nil {
		t.Fatalf("Failed to get config dir: %v", err)
	}

	if dir == "" {
		t.Error("Config dir is empty")
	}

	// 验证目录存在
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("Config dir does not exist: %v", err)
	}

	// 验证目录名称
	expectedDir := filepath.Join(os.TempDir(), "smartdnssort-unbound")
	if dir != expectedDir {
		t.Errorf("Expected dir %s, got %s", expectedDir, dir)
	}
}

// TestCleanupUnboundFiles 测试清理临时文件
func TestCleanupUnboundFiles(t *testing.T) {
	// 创建临时目录和文件
	tmpDir := filepath.Join(os.TempDir(), "smartdnssort-unbound")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(testFile); err != nil {
		t.Fatalf("Test file not created: %v", err)
	}

	// 清理文件
	if err := CleanupUnboundFiles(); err != nil {
		t.Fatalf("Failed to cleanup: %v", err)
	}

	// 验证文件已删除
	if _, err := os.Stat(tmpDir); err == nil {
		t.Error("Temp dir still exists after cleanup")
	}
}

// TestManagerGetLastHealthCheck 测试获取最后一次健康检查时间
func TestManagerGetLastHealthCheck(t *testing.T) {
	mgr := NewManager(5353)

	// 初始时间应该是零值
	lastCheck := mgr.GetLastHealthCheck()
	if !lastCheck.IsZero() {
		t.Error("Initial health check time should be zero")
	}
}

// TestManagerIsEnabled 测试启用状态检查
func TestManagerIsEnabled(t *testing.T) {
	mgr := NewManager(5353)

	if mgr.IsEnabled() {
		t.Error("New manager should not be enabled")
	}

	// 注意：实际的 Start/Stop 测试需要真实的 Unbound 二进制文件
	// 这里只测试状态标志
}

// TestPortConfiguration 测试不同端口配置
func TestPortConfiguration(t *testing.T) {
	ports := []int{5353, 5354, 5355, 8853}

	for _, port := range ports {
		mgr := NewManager(port)

		if mgr.GetPort() != port {
			t.Errorf("Port mismatch: expected %d, got %d", port, mgr.GetPort())
		}

		expectedAddr := fmt.Sprintf("127.0.0.1:%d", port)
		if mgr.GetAddress() != expectedAddr {
			t.Errorf("Address mismatch: expected %s, got %s", expectedAddr, mgr.GetAddress())
		}
	}
}

// TestConfigFilePermissions 测试配置文件权限
func TestConfigFilePermissions(t *testing.T) {
	mgr := NewManager(5353)
	configPath, err := mgr.generateConfig()
	if err != nil {
		t.Fatalf("Failed to generate config: %v", err)
	}

	defer os.Remove(configPath)

	// 检查文件权限
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("Failed to stat config file: %v", err)
	}

	// 文件应该是可读的
	if info.Mode()&0400 == 0 {
		t.Error("Config file is not readable")
	}
}

// TestMultipleManagers 测试多个 Manager 实例
func TestMultipleManagers(t *testing.T) {
	mgr1 := NewManager(5353)
	mgr2 := NewManager(5354)
	mgr3 := NewManager(5355)

	if mgr1.GetPort() == mgr2.GetPort() {
		t.Error("Managers should have different ports")
	}

	if mgr2.GetPort() == mgr3.GetPort() {
		t.Error("Managers should have different ports")
	}

	if mgr1.GetPort() == mgr3.GetPort() {
		t.Error("Managers should have different ports")
	}
}

// TestConfigDirCreation 测试配置目录创建
func TestConfigDirCreation(t *testing.T) {
	// 清理之前的目录
	tmpDir := filepath.Join(os.TempDir(), "smartdnssort-unbound")
	os.RemoveAll(tmpDir)

	// 获取配置目录（应该自动创建）
	dir, err := GetUnboundConfigDir()
	if err != nil {
		t.Fatalf("Failed to get config dir: %v", err)
	}

	// 验证目录已创建
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("Config dir was not created: %v", err)
	}

	// 清理
	os.RemoveAll(tmpDir)
}

// TestConfigContent 测试配置文件内容的完整性
func TestConfigContent(t *testing.T) {
	port := 5353
	mgr := NewManager(port)

	configPath, err := mgr.generateConfig()
	if err != nil {
		t.Fatalf("Failed to generate config: %v", err)
	}

	defer os.Remove(configPath)

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	contentStr := string(content)

	// 验证所有必要的配置项
	requiredConfigs := map[string]string{
		"port":                fmt.Sprintf("port: %d", port),
		"do-ip4":              "do-ip4: yes",
		"do-ip6":              "do-ip6: no",
		"do-udp":              "do-udp: yes",
		"do-tcp":              "do-tcp: yes",
		"interface":           "interface: 127.0.0.1",
		"num-threads":         "num-threads: 4",
		"msg-cache-size":      "msg-cache-size: 100m",
		"rrset-cache-size":    "rrset-cache-size: 200m",
		"module-config":       "module-config: \"validator iterator\"",
		"hide-identity":       "hide-identity: yes",
		"hide-version":        "hide-version: yes",
		"access-control-127":  "access-control: 127.0.0.1 allow",
		"access-control-deny": "access-control: 0.0.0.0/0 deny",
	}

	for name, config := range requiredConfigs {
		if !contains(contentStr, config) {
			t.Errorf("Config missing %s: %s", name, config)
		}
	}
}

// TestManagerConcurrency 测试并发访问
func TestManagerConcurrency(t *testing.T) {
	mgr := NewManager(5353)

	// 并发读取状态
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			_ = mgr.IsEnabled()
			_ = mgr.GetPort()
			_ = mgr.GetAddress()
			_ = mgr.GetLastHealthCheck()
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestWaitForReadyTimeout 测试等待超时
func TestWaitForReadyTimeout(t *testing.T) {
	mgr := NewManager(9999) // 使用不太可能被占用的端口

	// 这个测试会超时，因为没有进程监听该端口
	err := mgr.waitForReady(100 * time.Millisecond)

	if err == nil {
		t.Error("Expected timeout error")
	}

	// 验证错误消息
	if err.Error() != "timeout waiting for unbound to be ready" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

// TestPortAvailability 测试端口可用性检查
func TestPortAvailability(t *testing.T) {
	// 找一个可用的端口
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	port := addr.Port

	// 创建 Manager 使用该端口
	mgr := NewManager(port)

	if mgr.GetPort() != port {
		t.Errorf("Port mismatch: expected %d, got %d", port, mgr.GetPort())
	}
}

// 辅助函数
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// BenchmarkGenerateConfig 基准测试：配置生成
func BenchmarkGenerateConfig(b *testing.B) {
	mgr := NewManager(5353)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		configPath, err := mgr.generateConfig()
		if err != nil {
			b.Fatalf("Failed to generate config: %v", err)
		}
		os.Remove(configPath)
	}
}

// BenchmarkGetUnboundConfigDir 基准测试：获取配置目录
func BenchmarkGetUnboundConfigDir(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := GetUnboundConfigDir()
		if err != nil {
			b.Fatalf("Failed to get config dir: %v", err)
		}
	}
}

// BenchmarkManagerCreation 基准测试：Manager 创建
func BenchmarkManagerCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewManager(5353)
	}
}
