package recursor

import "time"

// waitForReadyTimeoutWindows Windows 特定的启动超时（默认实现）
func (m *Manager) waitForReadyTimeoutWindows() time.Duration {
	return 30 * time.Second
}

// waitForReadyTimeoutLinux Linux 特定的启动超时（默认实现）
func (m *Manager) waitForReadyTimeoutLinux() time.Duration {
	return 10 * time.Second
}
