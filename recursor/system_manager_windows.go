//go:build windows

package recursor

// Windows 特定的 SystemManager 实现
// 注意：Windows 上使用嵌入的 unbound 二进制文件和 root.key
// 不需要系统级的 unbound 管理

// ensureRootKeyLinux 在 Windows 上不支持
// 此方法仅作为编译占位符，实际不会被调用
// （因为 ensureRootKey() 在 Windows 上会直接返回错误）
func (sm *SystemManager) ensureRootKeyLinux() (string, error) {
	// 此方法不会被调用，仅为编译占位符
	panic("ensureRootKeyLinux should not be called on Windows")
}
