//go:build windows
// +build windows

package sysinstall

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
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

	// 确保服务所需的目录都存在
	if err := EnsureServiceDirs(); err != nil {
		return fmt.Errorf("创建服务目录失败: %v", err)
	}

	// 确保配置文件存在 (这一步非常关键!)
	if err := GenerateServiceConfigIfMissing(); err != nil {
		return fmt.Errorf("生成配置文件失败: %v", err)
	}

	// 验证配置文件是否真的存在
	configPath := DefaultConfigPath()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("配置文件不存在: %s", configPath)
	}
	si.log("配置文件已就绪: %s", configPath)

	// 直接使用 sc 命令安装服务 (更可靠)
	return si.installServiceWithSC()
}

// installServiceWithSC 使用 sc 命令安装服务
func (si *SystemInstaller) installServiceWithSC() error {
	binaryPath := DefaultBinaryPath()

	// 先检查服务是否已存在,如果存在则先删除
	si.log("检查旧服务...")
	checkCmd := exec.Command("sc", "query", ServiceName)
	if err := checkCmd.Run(); err == nil {
		// 服务存在,先停止
		si.log("发现旧服务,正在停止...")
		stopCmd := exec.Command("sc", "stop", ServiceName)
		_ = stopCmd.Run()
		time.Sleep(2 * time.Second)
		
		// 删除旧服务
		si.log("删除旧服务...")
		delCmd := exec.Command("sc", "delete", ServiceName)
		if output, err := delCmd.CombinedOutput(); err != nil {
			si.log("警告: 删除旧服务失败: %v", err)
		} else {
			si.log("旧服务已删除,输出: %s", string(output))
		}
		
		// 等待删除完成
		time.Sleep(2 * time.Second)
	}

	// 使用 sc create 命令创建服务
	// 注意:移除 Tcpip 依赖,避免启动问题
	si.log("创建服务: %s", ServiceName)
	si.log("二进制路径: %s", binaryPath)
	
	cmd := exec.Command("sc", "create", ServiceName,
		"binPath=", "\""+binaryPath+"\"",
		"start=", "auto",
		"DisplayName=", "SmartDNSSort DNS Server")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建服务失败: %v, 输出: %s", err, string(output))
	}
	si.log("服务创建成功")

	// 设置服务描述
	descCmd := exec.Command("sc", "description", ServiceName, ServiceDesc)
	if err := descCmd.Run(); err != nil {
		si.log("警告：设置服务描述失败: %v", err)
	}

	// 配置服务使用 LocalSystem 账户 (默认就是,但明确设置)
	// obj= LocalSystem 表示使用本地系统账户
	objCmd := exec.Command("sc", "config", ServiceName, "obj=", "LocalSystem")
	if err := objCmd.Run(); err != nil {
		si.log("警告：设置服务账户失败: %v", err)
	} else {
		si.log("服务账户已设置为 LocalSystem")
	}

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

	// 优先使用 SCM API
	if err := si.StartServiceWithMgr(); err != nil {
		si.log("SCM API 启动失败,回退到 sc 命令: %v", err)
		// 回退到 sc 命令
		cmd := exec.Command("sc", "start", ServiceName)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("启动服务失败: %v, 输出: %s", err, string(output))
		}
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

	// 优先使用 SCM API
	if err := si.StopServiceWithMgr(); err != nil {
		si.log("SCM API 停止失败,回退到 sc 命令: %v", err)
		// 回退到 sc 命令
		cmd := exec.Command("sc", "stop", ServiceName)
		output, err := cmd.CombinedOutput()
		if err != nil {
			if strings.Contains(string(output), "not been started") ||
				strings.Contains(string(output), "has not been started") {
				si.log("服务未运行，跳过停止")
				return nil
			}
			return fmt.Errorf("停止服务失败: %v, 输出: %s", err, string(output))
		}
	}

	si.log("服务停止成功")
	return nil
}

// GetServiceStatus 获取 Windows 服务状态
func (si *SystemInstaller) GetServiceStatus() (string, error) {
	// 优先使用 SCM API
	status, err := si.GetServiceStatusWithMgr()
	if err == nil {
		return status, nil
	}

	// 回退到 sc 命令
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
