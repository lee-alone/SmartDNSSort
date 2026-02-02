//go:build linux

package recursor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"smartdnssort/logger"
	"syscall"
)

// startPlatformSpecific Linux 特定的启动逻辑（已弃用，使用 startPlatformSpecificNoInit）
func (m *Manager) startPlatformSpecific() error {
	// 此方法已弃用，保留以兼容性
	return m.startPlatformSpecificNoInit()
}

// startPlatformSpecificNoInit Linux 特定的启动逻辑（不调用 Initialize）
func (m *Manager) startPlatformSpecificNoInit() error {
	// 1. 获取 unbound 路径
	if m.sysManager != nil {
		m.unboundPath = m.sysManager.unboundPath
		logger.Infof("[Recursor] Using system unbound: %s", m.unboundPath)
	}

	// 2. 生成配置文件
	configPath, err := m.generateConfigLinux()
	if err != nil {
		return fmt.Errorf("failed to generate unbound config: %w", err)
	}
	m.configPath = configPath
	logger.Infof("[Recursor] Generated config file: %s", configPath)

	// 验证配置文件
	if !fileExists(configPath) {
		return fmt.Errorf("config file not found after generation: %s", configPath)
	}

	return nil
}

// generateConfigLinux Linux 特定的配置生成
func (m *Manager) generateConfigLinux() (string, error) {
	configPath := "/etc/unbound/unbound.conf.d/smartdnssort.conf"

	// 确保目录存在
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	// 获取版本信息
	version := ""
	if m.sysManager != nil {
		version = m.sysManager.unboundVer
	}

	// 使用 ConfigGenerator 生成配置
	sysInfo := SystemInfo{
		CPUCores: runtime.NumCPU(),
		MemoryGB: 0, // Linux 上从系统获取，这里使用 0 触发保守配置
	}
	generator := NewConfigGenerator(version, sysInfo, m.port)
	config, err := generator.GenerateConfig()
	if err != nil {
		return "", fmt.Errorf("failed to generate config: %w", err)
	}

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return "", fmt.Errorf("failed to write config file: %w", err)
	}

	return configPath, nil
}

// configureUnixProcessManagement 配置 Linux 进程管理
// 使用进程组确保 Ctrl+C 时能正确关闭子进程
func (m *Manager) configureUnixProcessManagement() {
	// 在 Linux 上，使用 SysProcAttr 设置进程组
	// 这样可以通过发送信号给进程组来关闭所有子进程
	if m.cmd.SysProcAttr == nil {
		m.cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	// 设置进程组 ID，使其成为新进程组的领导者
	// 这样可以通过 syscall.Kill(-pid, signal) 向整个进程组发送信号
	m.cmd.SysProcAttr.Setsid = true
}

// cleanupUnixProcessManagement Linux 进程清理（无需特殊处理）
func (m *Manager) cleanupUnixProcessManagement() {
	// Linux 进程组会自动清理，无需特殊处理
}

// configureProcessManagement 配置 Linux 进程管理
func (m *Manager) configureProcessManagement() {
	m.configureUnixProcessManagement()
}

// cleanupProcessManagement 清理 Linux 进程管理
func (m *Manager) cleanupProcessManagement() {
	m.cleanupUnixProcessManagement()
}

// postStartProcessManagement Linux 启动后的处理（无需特殊处理）
func (m *Manager) postStartProcessManagement() {
	// Linux 进程组已在 configureProcessManagement 中配置
}
