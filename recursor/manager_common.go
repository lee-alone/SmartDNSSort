package recursor

import (
	"time"
)

// waitForReadyTimeoutWindows Windows 特定的启动超时
// Windows 使用嵌入式 unbound，启动通常较快
func (m *Manager) waitForReadyTimeoutWindows() time.Duration {
	return WaitReadyTimeoutWindows
}

// waitForReadyTimeoutLinux Linux 特定的启动超时
// Linux 上系统 unbound 启动可能需要更长时间，特别是首次启动时
func (m *Manager) waitForReadyTimeoutLinux() time.Duration {
	return WaitReadyTimeoutLinux
}
