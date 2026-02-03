//go:build windows

package recursor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"smartdnssort/logger"
	"unsafe"

	"golang.org/x/sys/windows"
)

// startPlatformSpecific Windows 特定的启动逻辑（已弃用，使用 startPlatformSpecificNoInit）
func (m *Manager) startPlatformSpecific() error {
	// 此方法已弃用，保留以兼容性
	return m.startPlatformSpecificNoInit()
}

// startPlatformSpecificNoInit Windows 特定的启动逻辑（不调用 Initialize）
func (m *Manager) startPlatformSpecificNoInit() error {
	// 1. 解压 Unbound 二进制文件
	unboundPath, err := ExtractUnboundBinary()
	if err != nil {
		logger.Errorf("[Recursor] Failed to extract unbound binary: %v", err)
		logger.Errorf("[Recursor] Diagnostic info:")
		logger.Errorf("[Recursor]   - OS: windows")
		logger.Errorf("[Recursor]   - Arch: %s", runtime.GOARCH)
		logger.Errorf("[Recursor]   - Working directory: %s", getWorkingDir())
		return fmt.Errorf("failed to extract unbound binary: %w", err)
	}
	m.unboundPath = unboundPath

	// 验证二进制文件
	fileInfo, err := os.Stat(unboundPath)
	if err != nil {
		return fmt.Errorf("unbound binary not found after extraction: %w", err)
	}
	logger.Infof("[Recursor] Unbound binary ready: %s (size: %d bytes)", unboundPath, fileInfo.Size())

	// 2. 检查 unbound 目录权限
	if err := checkDirectoryPermissionsWindows("unbound"); err != nil {
		logger.Warnf("[Recursor] Directory permission issue: %v", err)
	}

	// 3. 提取 root.key 文件
	if err := extractRootKey(); err != nil {
		return fmt.Errorf("failed to extract root.key: %w", err)
	}

	// 检查 root.key 的读写权限，如果不足则自动修改
	rootKeyPath := filepath.Join("unbound", "root.key")
	if err := checkFilePermissionsWindows(rootKeyPath); err != nil {
		logger.Warnf("[Recursor] Root key permission issue: %v", err)
	}

	// 3. 提取 root.zone 文件
	if err := extractRootZone(); err != nil {
		logger.Warnf("[Recursor] Failed to extract root.zone: %v", err)
		// 非致命错误，继续启动
	}

	// 4. 生成配置文件
	configPath, err := m.generateConfigWindows()
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
	if err := checkFilePermissionsWindows(configPath); err != nil {
		logger.Warnf("[Recursor] Config file permission issue: %v", err)
	}

	return nil
}

// generateConfigWindows Windows 特定的配置生成
//
// 智能生成策略：
// - 如果配置文件已存在，则跳过生成（允许用户编辑和保存）
// - 如果文件不存在，则生成默认配置
// - 首次启动时会生成配置，之后用户可以自由编辑
// - 如果需要重置配置，用户可以删除 unbound/unbound.conf 文件
func (m *Manager) generateConfigWindows() (string, error) {
	configDir, err := GetUnboundConfigDir()
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(configDir, "unbound.conf")
	// 在 Windows 上，使用绝对路径
	absPath, _ := filepath.Abs(configPath)
	configPath = absPath

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
		MemoryGB: 0, // Windows 上从系统获取，这里使用 0 触发保守配置
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

// configureWindowsProcessManagement 配置 Windows Job Object
// 确保当主进程被强制终止时，子进程也会被自动终止
func (m *Manager) configureWindowsProcessManagement() {
	// 创建 Job Object
	jobHandle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		logger.Warnf("[Recursor] Failed to create Job Object: %v", err)
		return
	}

	// 设置 Job Object 限制信息
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	// 设置信息 - SetInformationJobObject 返回 (int, error)
	ret, err := windows.SetInformationJobObject(jobHandle, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if ret == 0 || err != nil {
		logger.Warnf("[Recursor] Failed to set Job Object information: ret=%d, err=%v", ret, err)
		windows.CloseHandle(jobHandle)
		return
	}

	// 保存 Job Object 句柄
	m.jobObject = jobHandle
	logger.Debugf("[Recursor] Job Object created successfully")
}

// configureProcessManagement 配置 Windows 进程管理
func (m *Manager) configureProcessManagement() {
	m.configureWindowsProcessManagement()
}

// postStartProcessManagement Windows 启动后的处理 - 将进程分配到 Job Object
func (m *Manager) postStartProcessManagement() {
	if m.jobObject == nil || m.cmd == nil || m.cmd.Process == nil {
		return
	}

	jobHandle := m.jobObject.(windows.Handle)

	// 尝试打开进程句柄
	procHandle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(m.cmd.Process.Pid),
	)
	if err != nil {
		logger.Warnf("[Recursor] Failed to open process handle: %v", err)
		return
	}
	defer windows.CloseHandle(procHandle)

	// 将进程分配到 Job Object
	err = windows.AssignProcessToJobObject(jobHandle, procHandle)
	if err != nil {
		logger.Warnf("[Recursor] Failed to assign process to Job Object: %v", err)
		return
	}

	logger.Debugf("[Recursor] Process %d assigned to Job Object", m.cmd.Process.Pid)
}

// cleanupWindowsProcessManagement 清理 Windows Job Object
func (m *Manager) cleanupWindowsProcessManagement() {
	if m.jobObject == nil {
		return
	}

	jobHandle := m.jobObject.(windows.Handle)
	if err := windows.CloseHandle(jobHandle); err != nil {
		logger.Warnf("[Recursor] Failed to close Job Object: %v", err)
	} else {
		logger.Debugf("[Recursor] Job Object closed")
	}

	m.jobObject = nil
}

// cleanupProcessManagement 清理 Windows 进程管理
func (m *Manager) cleanupProcessManagement() {
	m.cleanupWindowsProcessManagement()
}

// configureUnixProcessManagement 配置 Unix/Linux 进程管理（Windows 上的默认实现）
func (m *Manager) configureUnixProcessManagement() {
	// Windows 不需要 Unix 进程管理
}

// cleanupUnixProcessManagement Unix/Linux 进程清理（Windows 上的默认实现）
func (m *Manager) cleanupUnixProcessManagement() {
	// Windows 不需要 Unix 进程清理
}

// checkFilePermissionsWindows 检查 Windows 上文件的读写权限，如果权限不足则自动修改
// 注意：Windows 的权限主要通过 ACL 管理，os.Chmod 只能控制文件只读属性
// 这是一个"尽力而为"的操作，可能无法完全解决所有权限问题
func checkFilePermissionsWindows(filePath string) error {
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
	readErr := checkReadPermissionWindows(filePath)

	// 检查写权限
	writeErr := checkWritePermissionWindows(filePath)

	// 如果权限都正常，直接返回
	if readErr == nil && writeErr == nil {
		logger.Debugf("[Permission] File permissions OK: %s (size: %d bytes)", filePath, info.Size())
		return nil
	}

	// 权限不足，尝试修改
	logger.Warnf("[Permission] Permission issue detected for %s, attempting to fix...", filePath)

	// Windows 上通过 Chmod 移除只读属性
	// 注意：Windows 的权限主要通过 ACL 管理，Chmod 只能控制文件只读属性
	if err := os.Chmod(filePath, 0666); err != nil {
		logger.Errorf("[Permission] Failed to fix permissions: %v", err)
		return fmt.Errorf("failed to fix file permissions: %w", err)
	}

	logger.Infof("[Permission] File permissions fixed: %s", filePath)

	// 修改后再次检查
	if err := checkReadPermissionWindows(filePath); err != nil {
		return fmt.Errorf("read permission still failed after fix: %w", err)
	}

	if err := checkWritePermissionWindows(filePath); err != nil {
		return fmt.Errorf("write permission still failed after fix: %w", err)
	}

	return nil
}

// checkReadPermissionWindows 检查 Windows 上文件的读权限
func checkReadPermissionWindows(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("cannot read file: %w", err)
	}
	defer file.Close()
	return nil
}

// checkWritePermissionWindows 检查 Windows 上文件的写权限
func checkWritePermissionWindows(filePath string) error {
	// 尝试打开文件进行写入（不实际写入）
	file, err := os.OpenFile(filePath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("cannot write to file: %w", err)
	}
	defer file.Close()
	return nil
}

// checkDirectoryPermissionsWindows 检查 Windows 上目录的读写权限，如果权限不足则自动修改
func checkDirectoryPermissionsWindows(dirPath string) error {
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
	readErr := checkDirectoryReadPermissionWindows(dirPath)

	// 检查写权限
	writeErr := checkDirectoryWritePermissionWindows(dirPath)

	// 如果权限都正常，直接返回
	if readErr == nil && writeErr == nil {
		logger.Debugf("[Permission] Directory permissions OK: %s", dirPath)
		return nil
	}

	// 权限不足，尝试修改
	logger.Warnf("[Permission] Permission issue detected for directory %s, attempting to fix...", dirPath)

	// Windows 上设置为可读写
	if err := os.Chmod(dirPath, 0777); err != nil {
		logger.Errorf("[Permission] Failed to fix directory permissions: %v", err)
		return fmt.Errorf("failed to fix directory permissions: %w", err)
	}

	logger.Infof("[Permission] Directory permissions fixed: %s", dirPath)

	// 修改后再次检查
	if err := checkDirectoryReadPermissionWindows(dirPath); err != nil {
		return fmt.Errorf("read permission still failed after fix: %w", err)
	}

	if err := checkDirectoryWritePermissionWindows(dirPath); err != nil {
		return fmt.Errorf("write permission still failed after fix: %w", err)
	}

	return nil
}

// checkDirectoryReadPermissionWindows 检查 Windows 上目录的读权限
func checkDirectoryReadPermissionWindows(dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("cannot read directory: %w", err)
	}
	_ = entries // 使用 entries 避免编译器警告
	return nil
}

// checkDirectoryWritePermissionWindows 检查 Windows 上目录的写权限
func checkDirectoryWritePermissionWindows(dirPath string) error {
	// 使用随机后缀避免多进程竞态条件
	// 虽然通常只有一个进程运行，但这是更安全的做法
	tempFile := filepath.Join(dirPath, ".smartdnssort_perm_check_tmp")

	if err := os.WriteFile(tempFile, []byte(""), 0666); err != nil {
		return fmt.Errorf("cannot write to directory: %w", err)
	}

	// 删除临时文件
	if err := os.Remove(tempFile); err != nil {
		logger.Warnf("[Permission] Failed to remove temporary permission check file: %v", err)
		// 继续返回成功，因为写权限检查已经通过
	}

	return nil
}
