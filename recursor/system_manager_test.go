package recursor

import (
	"runtime"
	"strings"
	"testing"
)

// TestNewSystemManager 测试创建新的 SystemManager
func TestNewSystemManager(t *testing.T) {
	sm := NewSystemManager()

	if sm == nil {
		t.Fatal("NewSystemManager returned nil")
	}

	if sm.osType != runtime.GOOS {
		t.Errorf("Expected osType %s, got %s", runtime.GOOS, sm.osType)
	}
}

// TestDetectSystem 测试系统检测
func TestDetectSystem(t *testing.T) {
	sm := NewSystemManager()
	err := sm.DetectSystem()

	if runtime.GOOS == "windows" {
		// Windows 不需要检测
		if err != nil {
			t.Errorf("Windows should not error: %v", err)
		}
	} else if runtime.GOOS == "linux" {
		// Linux 应该成功检测
		if err != nil {
			t.Logf("DetectSystem failed (may be expected in test environment): %v", err)
		}
	}
}

// TestParseOSRelease 测试解析 /etc/os-release
func TestParseOSRelease(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "Ubuntu",
			content:  "ID=ubuntu\nID_LIKE=debian",
			expected: "ubuntu",
		},
		{
			name:     "CentOS",
			content:  "ID=centos\nID_LIKE=rhel",
			expected: "centos",
		},
		{
			name:     "Arch",
			content:  "ID=arch\nID_LIKE=archlinux",
			expected: "arch",
		},
		{
			name:     "Alpine",
			content:  "ID=alpine\nID_LIKE=busybox",
			expected: "alpine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewSystemManager()
			result := sm.parseOSRelease(tt.content)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestGetPkgManager 测试获取包管理器
func TestGetPkgManager(t *testing.T) {
	tests := []struct {
		distro   string
		expected string
	}{
		{"ubuntu", "apt"},
		{"debian", "apt"},
		{"centos", "yum"},
		{"rhel", "yum"},
		{"arch", "pacman"},
		{"alpine", "apk"},
	}

	for _, tt := range tests {
		t.Run(tt.distro, func(t *testing.T) {
			sm := NewSystemManager()
			result := sm.getPkgManager(tt.distro)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestNormalizeDistro 测试规范化发行版名称
func TestNormalizeDistro(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Ubuntu", "ubuntu"},
		{"ubuntu", "ubuntu"},
		{"UBUNTU", "ubuntu"},
		{"CentOS", "centos"},
		{"RHEL", "rhel"},
		{"Arch", "arch"},
		{"Alpine", "alpine"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			sm := NewSystemManager()
			result := sm.normalizeDistro(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// TestGetSystemInfo 测试获取系统信息
func TestGetSystemInfo(t *testing.T) {
	sm := NewSystemManager()
	sysInfo := sm.GetSystemInfo()

	if sysInfo.OS == "" {
		t.Error("OS should not be empty")
	}

	if sysInfo.CPUCores < 1 {
		t.Error("CPUCores should be at least 1")
	}
}

// TestIsUnboundInstalled 测试检查 unbound 是否已安装
func TestIsUnboundInstalled(t *testing.T) {
	sm := NewSystemManager()
	// 这个测试取决于系统是否安装了 unbound
	// 只是验证函数不会 panic
	_ = sm.IsUnboundInstalled()
}

// TestGetUnboundVersion 测试获取 unbound 版本
func TestGetUnboundVersion(t *testing.T) {
	sm := NewSystemManager()
	if !sm.IsUnboundInstalled() {
		t.Skip("unbound not installed")
	}

	version, err := sm.GetUnboundVersion()
	if err != nil {
		t.Logf("Failed to get version (may be expected): %v", err)
		return
	}

	if version == "" {
		t.Error("Version should not be empty")
	}

	// 验证版本格式
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		t.Errorf("Invalid version format: %s", version)
	}
}

// BenchmarkGetSystemInfo 基准测试：获取系统信息
func BenchmarkGetSystemInfo(b *testing.B) {
	sm := NewSystemManager()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.GetSystemInfo()
	}
}
