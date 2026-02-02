package recursor

import (
	"fmt"
	"os/exec"
	"time"
)

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
