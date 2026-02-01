package recursor

import (
	"time"
)

// waitForReadyTimeoutWindows Windows 特定的启动超时
func (m *Manager) waitForReadyTimeoutWindows() time.Duration {
	return 30 * time.Second
}

// waitForReadyTimeoutLinux Linux 特定的启动超时
// Linux 上系统 unbound 启动可能需要更长时间，特别是首次启动时
func (m *Manager) waitForReadyTimeoutLinux() time.Duration {
	return 20 * time.Second
}
