//go:build windows

package recursor

import (
	"fmt"
)

// ensureRootKeyLinux Windows 上的 root.key 管理（不支持）
// Windows 使用嵌入的 root.key，无法通过 unbound-anchor 更新
func (sm *SystemManager) ensureRootKeyLinux() (string, error) {
	return "", fmt.Errorf("ensureRootKey not supported on Windows")
}

// runUnboundAnchor Windows 上不支持
func (sm *SystemManager) runUnboundAnchor(rootKeyPath string) error {
	return fmt.Errorf("unbound-anchor not available on Windows")
}

// isTemporaryAnchorError Windows 上不支持
func (sm *SystemManager) isTemporaryAnchorError(err error, output string) bool {
	return false
}

// extractEmbeddedRootKey Windows 上不支持（已在 manager_windows.go 中实现）
func (sm *SystemManager) extractEmbeddedRootKey(targetPath string) error {
	return fmt.Errorf("extractEmbeddedRootKey not supported on Windows")
}
