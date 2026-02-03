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

		// 2. 确保 root.key 存在（Linux 特定）
		if _, err := m.sysManager.ensureRootKey(); err != nil {
			logger.Warnf("[Recursor] Failed to ensure root.key: %v", err)
			logger.Warnf("[Recursor] DNSSEC validation may be disabled")
		} else {
			logger.Infof("[Recursor] Root key ready")

			// 检查 root.key 的读写权限，如果不足则自动修改
			if err := checkFilePermissions("/etc/unbound/root.key"); err != nil {
				logger.Warnf("[Recursor] Root key permission issue: %v", err)
			}
		}
	}

	// 3. 确保 /etc/unbound 目录存在，然后检查权限
	if err := os.MkdirAll("/etc/unbound", 0755); err != nil {
		logger.Warnf("[Recursor] Failed to create config directory: %v", err)
	}

	if err := checkDirectoryPermissions("/etc/unbound"); err != nil {
		logger.Warnf("[Recursor] Directory permission issue: %v", err)
	}

	// 3. 注意：root.zone 提取已由 RootZoneManager 在 manager.go 中处理
	// RootZoneManager 会自动选择正确的平台特定路径
	// Linux: /etc/unbound/root.zone
	// Windows: unbound/root.zone

	// 4. 生成配置文件
	configPath, err := m.generateConfigLinux()
	if err != nil {
		return fmt.Errorf("failed to generate unbound config: %w", err)
	}
	m.configPath = configPath
	logger.Infof("[Recursor] Config file ready: %s", configPath)

	// 验证配置文件
	if !fileExists(configPath) {
		return fmt.Errorf("config file not found after generation: %s", configPath)
	}

	// 检查配置文件的读权限，如果不足则自动修改
	if err := checkFilePermissions(configPath); err != nil {
		logger.Warnf("[Recursor] Config file permission issue: %v", err)
	}

	return nil
}

// generateConfigLinux Linux 特定的配置生成
//
// 智能生成策略：
// - 如果配置文件已存在，则跳过生成（允许用户编辑和保存）
// - 如果文件不存在，则生成默认配置
// - 首次启动时会生成配置，之后用户可以自由编辑
func (m *Manager) generateConfigLinux() (string, error) {
	configPath := "/etc/unbound/unbound.conf.d/smartdnssort.conf"

	// 确保目录存在
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	// 检查配置文件是否已存在
	if fileExists(configPath) {
		logger.Infof("[Recursor] Using existing config file: %s", configPath)
		return configPath, nil
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

	logger.Infof("[Recursor] Generated new config file: %s", configPath)
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

// checkFilePermissions 检查文件的读写权限，如果权限不足则自动修改
// 返回是否修改了权限
func checkFilePermissions(filePath string) error {
	// 检查文件是否存在
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", filePath)
		}
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// 检查是否是文件（不是目录）
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", filePath)
	}

	// 检查读权限
	readErr := checkReadPermission(filePath)

	// 检查写权限
	writeErr := checkWritePermission(filePath)

	// 如果权限都正常，直接返回
	if readErr == nil && writeErr == nil {
		logger.Debugf("[Permission] File permissions OK: %s (mode: %o)", filePath, info.Mode())
		return nil
	}

	// 权限不足，尝试修改
	logger.Warnf("[Permission] Permission issue detected for %s, attempting to fix...", filePath)

	// 设置为 0644（所有者可读写，其他用户只读）
	if err := os.Chmod(filePath, 0644); err != nil {
		logger.Errorf("[Permission] Failed to fix permissions: %v", err)
		return fmt.Errorf("failed to fix file permissions: %w", err)
	}

	logger.Infof("[Permission] File permissions fixed: %s (mode: 0644)", filePath)

	// 修改后再次检查
	if err := checkReadPermission(filePath); err != nil {
		return fmt.Errorf("read permission still failed after fix: %w", err)
	}

	if err := checkWritePermission(filePath); err != nil {
		return fmt.Errorf("write permission still failed after fix: %w", err)
	}

	return nil
}

// checkReadPermission 检查文件的读权限
func checkReadPermission(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("cannot read file: %w", err)
	}
	defer file.Close()
	return nil
}

// checkWritePermission 检查文件的写权限
func checkWritePermission(filePath string) error {
	// 尝试打开文件进行写入（不实际写入）
	file, err := os.OpenFile(filePath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("cannot write to file: %w", err)
	}
	defer file.Close()
	return nil
}

// checkDirectoryPermissions 检查目录的读写执行权限，如果权限不足则自动修改
func checkDirectoryPermissions(dirPath string) error {
	// 检查目录是否存在
	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory not found: %s", dirPath)
		}
		return fmt.Errorf("failed to stat directory: %w", err)
	}

	// 检查是否是目录
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dirPath)
	}

	// 检查读权限
	readErr := checkDirectoryReadPermission(dirPath)

	// 检查写权限
	writeErr := checkDirectoryWritePermission(dirPath)

	// 如果权限都正常，直接返回
	if readErr == nil && writeErr == nil {
		logger.Debugf("[Permission] Directory permissions OK: %s (mode: %o)", dirPath, info.Mode())
		return nil
	}

	// 权限不足，尝试修改
	logger.Warnf("[Permission] Permission issue detected for directory %s, attempting to fix...", dirPath)

	// 设置为 0755（所有者可读写执行，其他用户可读执行）
	if err := os.Chmod(dirPath, 0755); err != nil {
		logger.Errorf("[Permission] Failed to fix directory permissions: %v", err)
		return fmt.Errorf("failed to fix directory permissions: %w", err)
	}

	logger.Infof("[Permission] Directory permissions fixed: %s (mode: 0755)", dirPath)

	// 修改后再次检查
	if err := checkDirectoryReadPermission(dirPath); err != nil {
		return fmt.Errorf("read permission still failed after fix: %w", err)
	}

	if err := checkDirectoryWritePermission(dirPath); err != nil {
		return fmt.Errorf("write permission still failed after fix: %w", err)
	}

	return nil
}

// checkDirectoryReadPermission 检查目录的读权限
func checkDirectoryReadPermission(dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("cannot read directory: %w", err)
	}
	_ = entries // 使用 entries 避免编译器警告
	return nil
}

// checkDirectoryWritePermission 检查目录的写权限
func checkDirectoryWritePermission(dirPath string) error {
	// 尝试在目录中创建临时文件
	tempFile := filepath.Join(dirPath, ".smartdnssort_perm_check_tmp")

	if err := os.WriteFile(tempFile, []byte(""), 0644); err != nil {
		return fmt.Errorf("cannot write to directory: %w", err)
	}

	// 删除临时文件
	if err := os.Remove(tempFile); err != nil {
		logger.Warnf("[Permission] Failed to remove temporary permission check file: %v", err)
		// 继续返回成功，因为写权限检查已经通过
	}

	return nil
}
