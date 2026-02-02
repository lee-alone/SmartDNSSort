package recursor

import (
	"testing"
)

// TestEnsureRootKeyNotSupported 测试在非 Linux 系统上调用 ensureRootKey
func TestEnsureRootKeyNotSupported(t *testing.T) {
	sm := NewSystemManager()
	sm.osType = "windows"

	// 在 Windows 上应该返回错误
	_, err := sm.ensureRootKey()
	if err == nil {
		t.Error("Expected error on Windows, got nil")
	}

	if err.Error() != "ensureRootKey not supported on Windows" {
		t.Errorf("Expected 'ensureRootKey not supported on Windows', got '%v'", err)
	}
}

// TestTryUpdateRootKeyNotSupported 测试在非 Linux 系统上调用 tryUpdateRootKey
func TestTryUpdateRootKeyNotSupported(t *testing.T) {
	sm := NewSystemManager()
	sm.osType = "windows"

	// 在 Windows 上应该返回错误
	err := sm.tryUpdateRootKey()
	if err == nil {
		t.Error("Expected error on Windows, got nil")
	}

	if err.Error() != "tryUpdateRootKey only supported on Linux" {
		t.Errorf("Expected 'tryUpdateRootKey only supported on Linux', got '%v'", err)
	}
}

// TestEnsureRootKeyUnsupportedOS 测试在不支持的操作系统上调用 ensureRootKey
func TestEnsureRootKeyUnsupportedOS(t *testing.T) {
	sm := NewSystemManager()
	sm.osType = "darwin" // macOS

	// 在 macOS 上应该返回错误
	_, err := sm.ensureRootKey()
	if err == nil {
		t.Error("Expected error on macOS, got nil")
	}

	if err.Error() != "ensureRootKey only supported on Linux" {
		t.Errorf("Expected 'ensureRootKey only supported on Linux', got '%v'", err)
	}
}
