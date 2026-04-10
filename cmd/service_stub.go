//go:build !windows
// +build !windows

package main

// isWindowsService 在非 Windows 平台始终返回 false
func isWindowsService() bool {
	return false
}

// runAsWindowsService 在非 Windows 平台不支持
func runAsWindowsService() {
	panic("Windows 服务模式仅支持在 Windows 平台运行")
}
