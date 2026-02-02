package recursor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"smartdnssort/logger"
	"strings"
)

// SystemManager 管理系统级 unbound
type SystemManager struct {
	osType      string // "linux", "windows"
	distro      string // "ubuntu", "centos", "arch", "alpine"
	pkgManager  string // "apt", "yum", "pacman", "apk"
	unboundPath string // unbound 二进制路径
	unboundVer  string // unbound 版本
}

// NewSystemManager 创建新的 SystemManager
func NewSystemManager() *SystemManager {
	return &SystemManager{
		osType: runtime.GOOS,
	}
}

// DetectSystem 检测系统类型和包管理器
func (sm *SystemManager) DetectSystem() error {
	if sm.osType == "windows" {
		// Windows 使用嵌入式 unbound，无需检测
		return nil
	}

	if sm.osType != "linux" {
		return fmt.Errorf("unsupported OS: %s", sm.osType)
	}

	// 检测 Linux 发行版
	distro, err := sm.detectLinuxDistro()
	if err != nil {
		return err
	}
	sm.distro = distro

	// 根据发行版选择包管理器
	sm.pkgManager = sm.getPkgManager(distro)

	return nil
}

// detectLinuxDistro 检测 Linux 发行版
func (sm *SystemManager) detectLinuxDistro() (string, error) {
	// 优先读取 /etc/os-release
	content, err := os.ReadFile("/etc/os-release")
	if err == nil {
		return sm.parseOSRelease(string(content)), nil
	}

	// 备选 /etc/lsb-release
	content, err = os.ReadFile("/etc/lsb-release")
	if err == nil {
		return sm.parseLSBRelease(string(content)), nil
	}

	return "", fmt.Errorf("unable to detect Linux distribution")
}

// parseOSRelease 解析 /etc/os-release
func (sm *SystemManager) parseOSRelease(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "ID=") {
			id := strings.TrimPrefix(line, "ID=")
			id = strings.Trim(id, "\"")
			return sm.normalizeDistro(id)
		}
	}
	return "unknown"
}

// parseLSBRelease 解析 /etc/lsb-release
func (sm *SystemManager) parseLSBRelease(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "DISTRIB_ID=") {
			id := strings.TrimPrefix(line, "DISTRIB_ID=")
			return sm.normalizeDistro(strings.ToLower(id))
		}
	}
	return "unknown"
}

// normalizeDistro 规范化发行版名称
func (sm *SystemManager) normalizeDistro(distro string) string {
	distro = strings.ToLower(distro)
	switch {
	case strings.Contains(distro, "ubuntu"):
		return "ubuntu"
	case strings.Contains(distro, "debian"):
		return "debian"
	case strings.Contains(distro, "centos"):
		return "centos"
	case strings.Contains(distro, "rhel") || strings.Contains(distro, "fedora"):
		return "rhel"
	case strings.Contains(distro, "arch"):
		return "arch"
	case strings.Contains(distro, "alpine"):
		return "alpine"
	default:
		return distro
	}
}

// getPkgManager 根据发行版获取包管理器
func (sm *SystemManager) getPkgManager(distro string) string {
	switch distro {
	case "ubuntu", "debian":
		return "apt"
	case "centos", "rhel":
		return "yum"
	case "arch":
		return "pacman"
	case "alpine":
		return "apk"
	default:
		return "apt" // 默认使用 apt
	}
}

// IsUnboundInstalled 检查 unbound 是否已安装
func (sm *SystemManager) IsUnboundInstalled() bool {
	if sm.osType == "windows" {
		return false // Windows 使用嵌入式
	}

	// 直接检查标准位置（最简单、最可靠）
	standardPaths := []string{
		"/usr/sbin/unbound",
		"/usr/bin/unbound",
	}

	for _, path := range standardPaths {
		if _, err := os.Stat(path); err == nil {
			// 文件存在，尝试执行以验证
			cmd := exec.Command(path, "-V")
			if err := cmd.Run(); err == nil {
				return true
			}
		}
	}

	// 备选：尝试从 PATH 中查找
	cmd := exec.Command("unbound", "-V")
	err := cmd.Run()
	return err == nil
}

// GetUnboundVersion 获取 unbound 版本
func (sm *SystemManager) GetUnboundVersion() (string, error) {
	// 首先获取 unbound 的实际路径
	path, err := sm.getUnboundPath()
	if err == nil && path != "" {
		cmd := exec.Command(path, "-V")
		output, err := cmd.CombinedOutput()
		if err == nil {
			// 解析版本号
			// 输出格式: "unbound 1.19.0"
			parts := strings.Fields(string(output))
			if len(parts) >= 2 {
				return parts[1], nil
			}
		}
	}

	// 备选：尝试从 PATH 中查找
	cmd := exec.Command("unbound", "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	// 解析版本号
	// 输出格式: "unbound 1.19.0"
	parts := strings.Fields(string(output))
	if len(parts) >= 2 {
		return parts[1], nil
	}

	return "", fmt.Errorf("unable to parse unbound version")
}

// getUnboundPath 获取 unbound 二进制路径
func (sm *SystemManager) getUnboundPath() (string, error) {
	// 直接检查标准位置（最简单、最可靠）
	standardPaths := []string{
		"/usr/sbin/unbound",
		"/usr/bin/unbound",
	}

	for _, path := range standardPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// 备选：尝试 which 命令
	cmd := exec.Command("which", "unbound")
	output, err := cmd.CombinedOutput()
	if err == nil {
		path := strings.TrimSpace(string(output))
		if path != "" {
			return path, nil
		}
	}

	return "", fmt.Errorf("unbound executable not found in standard locations (/usr/sbin/unbound, /usr/bin/unbound)")
}

// InstallUnbound 安装 unbound
// 注意：此方法已移至 system_manager_install.go

// executeInstall 执行实际的安装命令
// 注意：此方法已移至 system_manager_install.go

// StopService 停止 unbound 服务
// 注意：此方法已移至 system_manager_service.go

// backupConfig 备份 unbound 配置文件
// 注意：此方法已移至 system_manager_service.go

// handleExistingUnbound 处理已存在的 unbound
// 注意：此方法已移至 system_manager_service.go

// DisableAutoStart 禁用自启
// 注意：此方法已移至 system_manager_service.go

// UninstallUnbound 卸载 unbound
// 注意：此方法已移至 system_manager_install.go

// GetSystemInfo 获取系统信息
func (sm *SystemManager) GetSystemInfo() SystemInfo {
	return SystemInfo{
		OS:          sm.osType,
		Distro:      sm.distro,
		CPUCores:    runtime.NumCPU(),
		UnboundPath: sm.unboundPath,
		UnboundVer:  sm.unboundVer,
		IsInstalled: sm.IsUnboundInstalled(),
	}
}

// SystemInfo 系统信息
type SystemInfo struct {
	OS          string
	Distro      string
	Arch        string
	CPUCores    int
	MemoryGB    float64
	UnboundPath string
	UnboundVer  string
	IsInstalled bool
	IsRunning   bool
}

// ensureRootKey 确保 root.key 存在（平台无关的通用方法）
// 在 Linux 上会尝试使用 unbound-anchor，失败时使用嵌入的 root.key
// 在 Windows 上直接返回错误（Windows 使用嵌入的 root.key）
func (sm *SystemManager) ensureRootKey() (string, error) {
	if sm.osType == "windows" {
		return "", fmt.Errorf("ensureRootKey not supported on Windows")
	}

	if sm.osType != "linux" {
		return "", fmt.Errorf("ensureRootKey only supported on Linux")
	}

	// 调用 Linux 特定的实现
	return sm.ensureRootKeyLinux()
}

// tryUpdateRootKey 尝试更新 root.key（后台任务）
// 仅在 Linux 上有效
func (sm *SystemManager) tryUpdateRootKey() error {
	if sm.osType != "linux" {
		return fmt.Errorf("tryUpdateRootKey only supported on Linux")
	}

	rootKeyPath := "/etc/unbound/root.key"

	// 检查文件是否存在
	if _, err := os.Stat(rootKeyPath); os.IsNotExist(err) {
		// 文件不存在，调用 ensureRootKey 生成
		_, err := sm.ensureRootKey()
		return err
	}

	// 文件存在，尝试更新
	logger.Infof("[SystemManager] Attempting to update root.key...")
	cmd := exec.Command("unbound-anchor", "-a", rootKeyPath, "-4")
	output, err := cmd.CombinedOutput()

	if err != nil {
		logger.Debugf("[SystemManager] Root key update failed (non-critical): %v", err)
		logger.Debugf("[SystemManager] Output: %s", string(output))
		// 更新失败不是致命错误，继续使用现有的 root.key
		return nil
	}

	logger.Infof("[SystemManager] Root key updated successfully")
	return nil
}
