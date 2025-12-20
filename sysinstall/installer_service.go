package sysinstall

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GenerateServiceFile 生成 systemd 服务文件内容
func (si *SystemInstaller) GenerateServiceFile() string {
	configPath := si.config.ConfigPath
	if configPath == "" {
		configPath = "/etc/SmartDNSSort/config.yaml"
	}

	workDir := si.config.WorkDir
	if workDir == "" {
		workDir = "/var/lib/SmartDNSSort"
	}

	runUser := si.config.RunUser
	if runUser == "" {
		runUser = "root"
	}

	execStart := fmt.Sprintf("/usr/local/bin/SmartDNSSort -c %s -w %s",
		configPath, workDir)

	serviceContent := fmt.Sprintf(`[Unit]
Description=SmartDNSSort DNS Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=5
User=%s
WorkingDirectory=%s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=SmartDNSSort

[Install]
WantedBy=multi-user.target
`, execStart, runUser, workDir)

	return serviceContent
}

// WriteServiceFile 写入 systemd 服务文件
func (si *SystemInstaller) WriteServiceFile() error {
	servicePath := "/etc/systemd/system/SmartDNSSort.service"
	content := si.GenerateServiceFile()

	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将写入服务文件：%s\n", servicePath)
		fmt.Printf("[DRY-RUN] 内容：\n%s\n", content)
		return nil
	}

	si.log("写入 systemd 服务文件：%s", servicePath)
	if err := os.WriteFile(servicePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入服务文件失败: %v", err)
	}

	return nil
}

// ReloadSystemd 重新加载 systemd
func (si *SystemInstaller) ReloadSystemd() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将执行命令：systemctl daemon-reload\n")
		return nil
	}

	si.log("重新加载 systemd 配置")
	cmd := exec.Command("systemctl", "daemon-reload")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload 失败: %v, 输出: %s", err, string(output))
	}

	return nil
}

// EnableService 启用服务
func (si *SystemInstaller) EnableService() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将执行命令：systemctl enable SmartDNSSort\n")
		return nil
	}

	si.log("启用 SmartDNSSort 服务")
	cmd := exec.Command("systemctl", "enable", "SmartDNSSort")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable 失败: %v, 输出: %s", err, string(output))
	}

	return nil
}

// StartService 启动服务
func (si *SystemInstaller) StartService() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将执行命令：systemctl start SmartDNSSort\n")
		return nil
	}

	si.log("启动 SmartDNSSort 服务")
	cmd := exec.Command("systemctl", "start", "SmartDNSSort")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl start 失败: %v, 输出: %s", err, string(output))
	}

	return nil
}

// StopService 停止服务
func (si *SystemInstaller) StopService() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将执行命令：systemctl stop SmartDNSSort\n")
		return nil
	}

	si.log("停止 SmartDNSSort 服务")
	cmd := exec.Command("systemctl", "stop", "SmartDNSSort")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl stop 失败: %v, 输出: %s", err, string(output))
	}

	return nil
}

// DisableService 禁用服务
func (si *SystemInstaller) DisableService() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将执行命令：systemctl disable SmartDNSSort\n")
		return nil
	}

	si.log("禁用 SmartDNSSort 服务")
	cmd := exec.Command("systemctl", "disable", "SmartDNSSort")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl disable 失败: %v, 输出: %s", err, string(output))
	}

	return nil
}

// GetServiceStatus 获取服务状态
func (si *SystemInstaller) GetServiceStatus() (string, error) {
	cmd := exec.Command("systemctl", "is-active", "SmartDNSSort")
	output, err := cmd.CombinedOutput()
	status := strings.TrimSpace(string(output))

	if err != nil && status == "inactive" {
		return "inactive", nil
	}

	return status, err
}

// GetServiceDetails 获取服务详细信息
func (si *SystemInstaller) GetServiceDetails() (string, error) {
	cmd := exec.Command("systemctl", "status", "SmartDNSSort")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// GetRecentLogs 获取最近的日志
func (si *SystemInstaller) GetRecentLogs(lines int) (string, error) {
	cmd := exec.Command("journalctl", "-u", "SmartDNSSort", "-n", fmt.Sprintf("%d", lines), "--no-pager")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// RemoveServiceFile 删除服务文件
func (si *SystemInstaller) RemoveServiceFile() error {
	servicePath := "/etc/systemd/system/SmartDNSSort.service"

	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将删除服务文件：%s\n", servicePath)
		return nil
	}

	si.log("删除服务文件：%s", servicePath)
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除服务文件失败: %v", err)
	}

	return nil
}
