//go:build windows
// +build windows

package sysinstall

import (
	"fmt"
	"os/exec"
	"strings"
)

// InstallService 安装 Windows 服务
func (si *SystemInstaller) InstallService() error {
	binaryPath := DefaultBinaryPath()

	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将安装服务：%s\n", ServiceName)
		fmt.Printf("[DRY-RUN] 服务路径：%s\n", binaryPath)
		return nil
	}

	si.log("安装 Windows 服务：%s", ServiceName)

	// 使用 sc create 命令创建服务
	// 服务类型为 own（独立进程），启动类型为 auto（自动启动）
	cmd := exec.Command("sc", "create", ServiceName,
		"binPath=", binaryPath,
		"start=", "auto",
		"DisplayName=", "SmartDNSSort DNS Server",
		"depend=", "Tcpip")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建服务失败: %v, 输出: %s", err, string(output))
	}

	// 设置服务描述
	descCmd := exec.Command("sc", "description", ServiceName, ServiceDesc)
	if output, err := descCmd.CombinedOutput(); err != nil {
		si.log("警告：设置服务描述失败: %v, 输出: %s", err, string(output))
	}

	si.log("服务安装成功")
	return nil
}

// UninstallService 卸载 Windows 服务
func (si *SystemInstaller) UninstallService() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将卸载服务：%s\n", ServiceName)
		return nil
	}

	si.log("卸载 Windows 服务：%s", ServiceName)

	// 先删除服务
	cmd := exec.Command("sc", "delete", ServiceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("删除服务失败: %v, 输出: %s", err, string(output))
	}

	si.log("服务卸载成功")
	return nil
}

// StartService 启动 Windows 服务
func (si *SystemInstaller) StartService() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将启动服务：%s\n", ServiceName)
		return nil
	}

	si.log("启动 Windows 服务：%s", ServiceName)

	cmd := exec.Command("sc", "start", ServiceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("启动服务失败: %v, 输出: %s", err, string(output))
	}

	si.log("服务启动成功")
	return nil
}

// StopService 停止 Windows 服务
func (si *SystemInstaller) StopService() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将停止服务：%s\n", ServiceName)
		return nil
	}

	si.log("停止 Windows 服务：%s", ServiceName)

	cmd := exec.Command("sc", "stop", ServiceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 如果服务未运行，忽略错误
		if strings.Contains(string(output), "not been started") ||
			strings.Contains(string(output), "has not been started") {
			si.log("服务未运行，跳过停止")
			return nil
		}
		return fmt.Errorf("停止服务失败: %v, 输出: %s", err, string(output))
	}

	si.log("服务停止成功")
	return nil
}

// GetServiceStatus 获取 Windows 服务状态
func (si *SystemInstaller) GetServiceStatus() (string, error) {
	cmd := exec.Command("sc", "query", ServiceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "NOT_FOUND", fmt.Errorf("查询服务失败: %v", err)
	}

	outputStr := string(output)

	// 解析服务状态
	if strings.Contains(outputStr, "RUNNING") {
		return "RUNNING", nil
	} else if strings.Contains(outputStr, "STOPPED") {
		return "STOPPED", nil
	} else if strings.Contains(outputStr, "PAUSED") {
		return "PAUSED", nil
	} else if strings.Contains(outputStr, "START_PENDING") {
		return "START_PENDING", nil
	} else if strings.Contains(outputStr, "STOP_PENDING") {
		return "STOP_PENDING", nil
	} else if strings.Contains(outputStr, "does not exist") {
		return "NOT_FOUND", nil
	}

	return "UNKNOWN", nil
}

// EnableService 启用 Windows 服务（设置自动启动）
func (si *SystemInstaller) EnableService() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将启用服务自动启动：%s\n", ServiceName)
		return nil
	}

	si.log("启用服务自动启动：%s", ServiceName)

	cmd := exec.Command("sc", "config", ServiceName, "start=", "auto")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("启用服务失败: %v, 输出: %s", err, string(output))
	}

	return nil
}

// DisableService 禁用 Windows 服务
func (si *SystemInstaller) DisableService() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将禁用服务：%s\n", ServiceName)
		return nil
	}

	si.log("禁用服务：%s", ServiceName)

	cmd := exec.Command("sc", "config", ServiceName, "start=", "disabled")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("禁用服务失败: %v, 输出: %s", err, string(output))
	}

	return nil
}
