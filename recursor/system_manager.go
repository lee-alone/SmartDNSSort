package recursor

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"smartdnssort/logger"
	"strings"
	"time"
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
// 流程：
// 1. 检查是否已安装
// 2. 如果已安装，处理现有的 unbound（停止、禁用自启）
// 3. 如果未安装，执行安装
// 4. 禁用自启动
// 5. 停止当前进程
func (sm *SystemManager) InstallUnbound() error {
	if sm.osType == "windows" {
		return fmt.Errorf("unbound installation not needed on Windows")
	}

	// 步骤 1：检查是否已安装
	isInstalled := sm.IsUnboundInstalled()

	if isInstalled {
		// 步骤 2a：已安装，获取版本和路径
		ver, err := sm.GetUnboundVersion()
		if err != nil {
			return fmt.Errorf("failed to get unbound version: %w", err)
		}
		sm.unboundVer = ver

		path, err := sm.getUnboundPath()
		if err != nil {
			return fmt.Errorf("failed to get unbound path: %w", err)
		}
		sm.unboundPath = path

		// 步骤 2b：处理现有的 unbound
		return sm.handleExistingUnbound()
	}

	// 步骤 3：未安装，执行安装
	if err := sm.executeInstall(); err != nil {
		return err
	}

	// 步骤 4：禁用自启动
	if err := sm.DisableAutoStart(); err != nil {
		return fmt.Errorf("failed to disable autostart: %w", err)
	}

	// 步骤 5：停止当前进程
	if err := sm.StopService(); err != nil {
		return fmt.Errorf("failed to stop unbound service: %w", err)
	}

	// 等待更长时间，确保安装完成和 PATH 更新
	time.Sleep(2 * time.Second)

	// 验证 unbound 是否真的已安装
	if !sm.IsUnboundInstalled() {
		return fmt.Errorf("unbound installation verification failed: executable not found in PATH")
	}

	// 获取版本和路径
	ver, err := sm.GetUnboundVersion()
	if err != nil {
		return fmt.Errorf("failed to get unbound version after installation: %w", err)
	}
	sm.unboundVer = ver

	path, err := sm.getUnboundPath()
	if err != nil {
		return fmt.Errorf("failed to get unbound path after installation: %w", err)
	}
	sm.unboundPath = path

	return nil
}

// executeInstall 执行实际的安装命令
func (sm *SystemManager) executeInstall() error {
	var cmd *exec.Cmd

	switch sm.pkgManager {
	case "apt":
		// 更新包列表
		updateCmd := exec.Command("apt-get", "update")
		if err := updateCmd.Run(); err != nil {
			return fmt.Errorf("failed to update apt: %w", err)
		}
		// 安装 unbound
		cmd = exec.Command("apt-get", "install", "-y", "unbound")
	case "yum":
		cmd = exec.Command("yum", "install", "-y", "unbound")
	case "pacman":
		cmd = exec.Command("pacman", "-S", "--noconfirm", "unbound")
	case "apk":
		cmd = exec.Command("apk", "add", "unbound")
	default:
		return fmt.Errorf("unsupported package manager: %s", sm.pkgManager)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install unbound on %s using %s: %w", sm.distro, sm.pkgManager, err)
	}

	return nil
}

// StopService 停止 unbound 服务
func (sm *SystemManager) StopService() error {
	cmd := exec.Command("systemctl", "stop", "unbound")
	if err := cmd.Run(); err != nil {
		// 如果 systemctl 失败，尝试 killall
		killCmd := exec.Command("killall", "unbound")
		if err := killCmd.Run(); err != nil {
			// 两种方法都失败，可能 unbound 没有运行，这不是错误
			return nil
		}
	}
	return nil
}

// backupConfig 备份 unbound 配置文件
// 使用 Go 标准库而不是 Shell 命令
func (sm *SystemManager) backupConfig() error {
	src := "/etc/unbound/unbound.conf"
	dst := "/etc/unbound/unbound.conf.bak"

	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在，这不是错误
			return nil
		}
		return fmt.Errorf("failed to read config file %s: %w", src, err)
	}

	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup config to %s: %w", dst, err)
	}

	return nil
}

// handleExistingUnbound 处理已存在的 unbound
// 流程：
// 1. 停止服务
// 2. 禁用自启
// 3. 备份配置
func (sm *SystemManager) handleExistingUnbound() error {
	// 步骤 1：停止服务
	if err := sm.StopService(); err != nil {
		return fmt.Errorf("failed to stop unbound service: %w", err)
	}

	// 步骤 2：禁用自启
	if err := sm.DisableAutoStart(); err != nil {
		return fmt.Errorf("failed to disable autostart: %w", err)
	}

	// 步骤 3：备份配置
	if err := sm.backupConfig(); err != nil {
		// 备份失败不应该中断整个流程
		logger.Warnf("[SystemManager] Failed to backup config: %v", err)
	}

	return nil
}

// DisableAutoStart 禁用自启
func (sm *SystemManager) DisableAutoStart() error {
	cmd := exec.Command("systemctl", "disable", "unbound")
	if err := cmd.Run(); err != nil {
		// 如果 systemctl 失败，可能是权限问题或 systemctl 不可用
		// 尝试其他方法（如 chkconfig）
		altCmd := exec.Command("chkconfig", "unbound", "off")
		if err := altCmd.Run(); err != nil {
			// 两种方法都失败，记录警告但不中断
			return fmt.Errorf("failed to disable autostart: %w", err)
		}
	}
	return nil
}

// UninstallUnbound 卸载 unbound
func (sm *SystemManager) UninstallUnbound() error {
	if sm.osType == "windows" {
		return fmt.Errorf("unbound uninstall not needed on Windows")
	}

	// 停止服务
	cmd := exec.Command("systemctl", "stop", "unbound")
	_ = cmd.Run()

	// 禁用自启
	cmd = exec.Command("systemctl", "disable", "unbound")
	_ = cmd.Run()

	// 根据包管理器卸载
	var uninstallCmd *exec.Cmd
	switch sm.pkgManager {
	case "apt":
		uninstallCmd = exec.Command("apt-get", "remove", "-y", "unbound")
	case "yum":
		uninstallCmd = exec.Command("yum", "remove", "-y", "unbound")
	case "pacman":
		uninstallCmd = exec.Command("pacman", "-R", "--noconfirm", "unbound")
	case "apk":
		uninstallCmd = exec.Command("apk", "del", "unbound")
	default:
		return fmt.Errorf("unsupported package manager: %s", sm.pkgManager)
	}

	if err := uninstallCmd.Run(); err != nil {
		return fmt.Errorf("failed to uninstall unbound: %w", err)
	}

	return nil
}

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
