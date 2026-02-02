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
	logger.Infof("[Recursor] Extracted unbound binary to: %s", unboundPath)

	// 验证二进制文件
	fileInfo, err := os.Stat(unboundPath)
	if err != nil {
		return fmt.Errorf("unbound binary not found after extraction: %w", err)
	}
	logger.Infof("[Recursor] Unbound binary size: %d bytes", fileInfo.Size())

	// 2. 提取 root.key 文件
	if err := extractRootKey(); err != nil {
		return fmt.Errorf("failed to extract root.key: %w", err)
	}

	// 3. 生成配置文件
	configPath, err := m.generateConfigWindows()
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

// generateConfigWindows Windows 特定的配置生成
func (m *Manager) generateConfigWindows() (string, error) {
	configDir, err := GetUnboundConfigDir()
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(configDir, "unbound.conf")
	// 在 Windows 上，使用绝对路径
	absPath, _ := filepath.Abs(configPath)
	configPath = absPath

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
