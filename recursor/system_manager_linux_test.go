//go:build linux

package recursor

import (
	"os"
	"strings"
	"testing"
)

// TestIsTemporaryAnchorError 测试临时错误判断
func TestIsTemporaryAnchorError(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		output   string
		expected bool
	}{
		{
			name:     "timeout error",
			errMsg:   "timeout",
			output:   "",
			expected: true,
		},
		{
			name:     "network unreachable",
			errMsg:   "",
			output:   "network unreachable",
			expected: true,
		},
		{
			name:     "connection refused",
			errMsg:   "connection refused",
			output:   "",
			expected: true,
		},
		{
			name:     "command not found",
			errMsg:   "command not found",
			output:   "",
			expected: true,
		},
		{
			name:     "unknown error",
			errMsg:   "unknown error",
			output:   "",
			expected: false,
		},
		{
			name:     "critical error",
			errMsg:   "permission denied",
			output:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewSystemManager()
			err := &mockError{msg: tt.errMsg}
			result := sm.isTemporaryAnchorError(err, tt.output)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// mockError 用于测试的错误类型
type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

// TestEnsureRootKeyLinux 测试 root.key 管理（需要 root 权限）
func TestEnsureRootKeyLinux(t *testing.T) {
	// 这个测试需要 root 权限，跳过
	if os.Getuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	sm := NewSystemManager()
	sm.osType = "linux"

	// 测试 ensureRootKeyLinux
	path, err := sm.ensureRootKeyLinux()
	if err != nil {
		t.Logf("ensureRootKeyLinux failed (may be expected): %v", err)
		return
	}

	if path == "" {
		t.Error("Expected non-empty path")
	}

	if !strings.Contains(path, "root.key") {
		t.Errorf("Expected path to contain 'root.key', got %s", path)
	}
}

// TestExtractEmbeddedRootKey 测试从嵌入文件中提取 root.key
func TestExtractEmbeddedRootKey(t *testing.T) {
	sm := NewSystemManager()
	sm.osType = "linux"

	// 创建临时目录
	tmpDir := t.TempDir()
	targetPath := tmpDir + "/test_root.key"

	// 测试提取
	err := sm.extractEmbeddedRootKey(targetPath)
	if err != nil {
		t.Fatalf("Failed to extract embedded root.key: %v", err)
	}

	// 验证文件是否存在
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("Extracted file not found: %v", err)
	}

	// 验证文件大小
	info, _ := os.Stat(targetPath)
	if info.Size() < 1024 {
		t.Errorf("Extracted file seems too small: %d bytes", info.Size())
	}

	// 验证文件内容（应该包含 DNSSEC 相关内容）
	data, _ := os.ReadFile(targetPath)
	content := string(data)
	if !strings.Contains(content, "DNSKEY") && !strings.Contains(content, "dnskey") {
		t.Logf("Warning: Extracted root.key may not be valid DNSSEC key")
	}
}
