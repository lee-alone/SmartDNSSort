package recursor

import (
	"context"
	"fmt"
	"time"
)

// IsEnabled 检查 Recursor 是否启用
func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// GetPort 获取 Unbound 监听端口
func (m *Manager) GetPort() int {
	return m.port
}

// GetAddress 获取 Unbound 地址
func (m *Manager) GetAddress() string {
	return fmt.Sprintf("127.0.0.1:%d", m.port)
}

// GetLastHealthCheck 获取最后一次健康检查时间
func (m *Manager) GetLastHealthCheck() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastHealthCheck
}

// GetStartTime 获取进程启动时间
func (m *Manager) GetStartTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.startTime
}

// GetRestartAttempts 获取当前重启尝试次数
func (m *Manager) GetRestartAttempts() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.restartAttempts
}

// GetLastRestartTime 获取最后一次重启时间
func (m *Manager) GetLastRestartTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastRestartTime
}

// Query 执行 DNS 查询（用于测试）
func (m *Manager) Query(ctx context.Context, domain string) error {
	if !m.IsEnabled() {
		return fmt.Errorf("recursor not enabled")
	}

	// 这里可以添加实际的 DNS 查询逻辑
	// 用于验证 Unbound 是否正常工作
	return nil
}

// GetSystemInfo 获取系统信息
func (m *Manager) GetSystemInfo() SystemInfo {
	if m.sysManager == nil {
		return SystemInfo{}
	}
	return m.sysManager.GetSystemInfo()
}

// GetUnboundVersion 获取 unbound 版本
func (m *Manager) GetUnboundVersion() string {
	if m.sysManager == nil {
		return ""
	}
	return m.sysManager.unboundVer
}

// GetInstallState 获取安装状态
func (m *Manager) GetInstallState() InstallState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.installState
}

// SetInstallState 设置安装状态
func (m *Manager) SetInstallState(state InstallState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installState = state
}
