//go:build !windows && !linux

package recursor

// startPlatformSpecificNoInit 平台特定的启动逻辑（其他平台的默认实现）
func (m *Manager) startPlatformSpecificNoInit() error {
	// 其他平台不支持嵌入式 unbound
	return nil
}

// configureProcessManagement 配置进程管理（其他平台的默认实现）
func (m *Manager) configureProcessManagement() {
	// 其他平台不需要特殊的进程管理配置
}

// postStartProcessManagement 启动后的处理（其他平台的默认实现）
func (m *Manager) postStartProcessManagement() {
	// 其他平台不需要启动后的特殊处理
}

// cleanupProcessManagement 清理进程管理（其他平台的默认实现）
func (m *Manager) cleanupProcessManagement() {
	// 其他平台不需要特殊的清理
}

// configureUnixProcessManagement 配置 Unix/Linux 进程管理（其他平台的默认实现）
func (m *Manager) configureUnixProcessManagement() {
	// 其他平台不需要特殊的进程管理配置
}

// cleanupUnixProcessManagement Unix/Linux 进程清理（其他平台的默认实现）
func (m *Manager) cleanupUnixProcessManagement() {
	// 其他平台不需要特殊的清理
}
