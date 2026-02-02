package recursor

import (
	"fmt"
	"os"
	"runtime"
	"smartdnssort/logger"
)

// Initialize 初始化 Unbound（首次启用时调用）
// 流程：
// 1. 检测系统类型和包管理器
// 2. 检查 unbound 是否已安装
// 3. 如果未安装，执行安装
// 4. 获取版本信息和路径
// 5. 创建配置生成器
// 6. 验证配置
// 仅在 Linux 上执行，Windows 使用嵌入式 unbound
func (m *Manager) Initialize() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. 检测系统
	if err := m.sysManager.DetectSystem(); err != nil {
		return fmt.Errorf("failed to detect system: %w", err)
	}

	logger.Infof("[Recursor] System detected: OS=%s, Distro=%s", m.sysManager.osType, m.sysManager.distro)

	// 2. 检查 unbound 是否已安装
	if !m.sysManager.IsUnboundInstalled() {
		logger.Infof("[Recursor] Unbound not installed, installing...")
		// 3. 安装 unbound
		if err := m.sysManager.InstallUnbound(); err != nil {
			return fmt.Errorf("failed to install unbound: %w", err)
		}
		logger.Infof("[Recursor] Unbound installed successfully")
	} else {
		logger.Infof("[Recursor] Unbound already installed")
		// 4. 处理已存在的 unbound
		if err := m.sysManager.handleExistingUnbound(); err != nil {
			logger.Warnf("[Recursor] Failed to handle existing unbound: %v", err)
		}
	}

	// 5. 获取版本信息
	version, err := m.sysManager.GetUnboundVersion()
	if err != nil {
		return fmt.Errorf("failed to get unbound version: %w", err)
	}

	logger.Infof("[Recursor] Unbound version: %s", version)

	// 6. 获取 unbound 路径
	path, err := m.sysManager.getUnboundPath()
	if err != nil {
		return fmt.Errorf("failed to get unbound path: %w", err)
	}

	m.sysManager.unboundPath = path
	m.sysManager.unboundVer = version
	logger.Infof("[Recursor] Unbound path: %s", path)

	// 7. 创建配置生成器
	sysInfo := m.sysManager.GetSystemInfo()
	m.configGen = NewConfigGenerator(version, sysInfo, m.port)

	// 8. 验证配置
	if err := m.configGen.ValidateConfig(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// 9. 确定是否为系统级
	m.isSystemLevel = runtime.GOOS == "linux"

	logger.Infof("[Recursor] Initialization complete: OS=%s, Version=%s, SystemLevel=%v",
		sysInfo.OS, version, m.isSystemLevel)

	return nil
}

// Cleanup 清理资源（卸载时调用）
// 流程：
// 1. 停止 unbound 进程
// 2. 删除配置文件
// 3. Linux: 卸载 unbound 系统包
func (m *Manager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. 停止 unbound
	if m.enabled {
		m.mu.Unlock()
		err := m.Stop()
		m.mu.Lock()
		if err != nil {
			logger.Warnf("[Recursor] Failed to stop unbound: %v", err)
		}
	}

	// 2. 删除配置文件
	if m.configPath != "" {
		_ = os.Remove(m.configPath)
	}

	// 3. Linux: 卸载 unbound
	if runtime.GOOS == "linux" && m.sysManager != nil {
		if err := m.sysManager.UninstallUnbound(); err != nil {
			logger.Warnf("[Recursor] Failed to uninstall unbound: %v", err)
		}
	}

	logger.Infof("[Recursor] Cleanup complete")
	return nil
}
