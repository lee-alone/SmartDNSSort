package sysinstall

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
)

// InstallerConfig 安装配置
type InstallerConfig struct {
	ConfigPath string // 配置文件路径
	WorkDir    string // 工作目录
	RunUser    string // 运行用户
	BinaryPath string // 二进制路径
	DryRun     bool   // 是否为干运行模式
	Verbose    bool   // 是否显示详细信息
}

// SystemInstaller 系统安装器
type SystemInstaller struct {
	config InstallerConfig
	log    func(format string, args ...interface{})
}

// NewSystemInstaller 创建新的系统安装器
func NewSystemInstaller(cfg InstallerConfig) *SystemInstaller {
	si := &SystemInstaller{
		config: cfg,
	}

	// 日志函数
	if cfg.Verbose {
		si.log = func(format string, args ...interface{}) {
			fmt.Printf("[INFO] "+format+"\n", args...)
		}
	} else {
		si.log = func(format string, args ...interface{}) {}
	}

	return si
}

// IsRoot 检查是否以 root 权限运行
func (si *SystemInstaller) IsRoot() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	currentUser, err := user.Current()
	return err == nil && currentUser.Uid == "0"
}

// CheckSystemd 检查系统是否支持 systemd
func (si *SystemInstaller) CheckSystemd() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("此功能仅支持 Linux 系统")
	}

	cmd := exec.Command("systemctl", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("系统不支持 systemd，请确保已安装 systemd 服务管理器")
	}

	return nil
}

// CreateDirectories 创建必要的目录
func (si *SystemInstaller) CreateDirectories() error {
	dirs := []struct {
		path string
		mode os.FileMode
		desc string
	}{
		{"/etc/SmartDNSSort", 0755, "配置目录"},
		{"/var/lib/SmartDNSSort", 0755, "数据目录"},
		{"/var/log/SmartDNSSort", 0755, "日志目录"},
	}

	for _, dir := range dirs {
		if si.config.DryRun {
			fmt.Printf("[DRY-RUN] 将创建目录：%s (%s)\n", dir.path, dir.desc)
			continue
		}

		si.log("创建目录：%s", dir.path)
		if err := os.MkdirAll(dir.path, dir.mode); err != nil {
			return fmt.Errorf("创建目录失败 %s: %v", dir.path, err)
		}
	}

	return nil
}

// GenerateDefaultConfig 生成默认配置文件
func (si *SystemInstaller) GenerateDefaultConfig() error {
	configPath := si.config.ConfigPath
	if configPath == "" {
		configPath = "/etc/SmartDNSSort/config.yaml"
	}

	// 检查文件是否已存在
	if _, err := os.Stat(configPath); err == nil {
		si.log("配置文件已存在：%s", configPath)
		return nil
	}

	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将创建默认配置文件：%s\n", configPath)
		return nil
	}

	// 读取嵌入的默认配置
	defaultConfig := `# SmartDNSSort 配置文件

# DNS 服务器配置
dns:
  listen_port: 53
  enable_tcp: true
  enable_ipv6: true

# 上游 DNS 服务器配置
upstream:
  servers:
    - "192.168.1.10"
    - "192.168.1.11"
    - "192.168.1.25"
  strategy: "random"
  timeout_ms: 3000
  concurrency: 4

# Ping 检测配置
ping:
  count: 3
  timeout_ms: 500
  concurrency: 16
  strategy: "min"

# DNS 缓存配置
cache:
  min_ttl_seconds: 3600
  max_ttl_seconds: 84600

# Web UI 管理界面配置
webui:
  enabled: true
  listen_port: 8080

# 广告拦截配置
adblock:
  enabled: false
  rule_file: "rules.txt"
`

	si.log("创建默认配置文件：%s", configPath)
	if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}

	return nil
}

// CopyBinary 复制二进制文件到系统目录
func (si *SystemInstaller) CopyBinary() error {
	sourcePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %v", err)
	}

	destPath := "/usr/local/bin/SmartDNSSort"

	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将复制二进制文件：%s -> %s\n", sourcePath, destPath)
		return nil
	}

	si.log("复制二进制文件：%s -> %s", sourcePath, destPath)

	// 读取源文件
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("读取二进制文件失败: %v", err)
	}

	// 写入目标文件
	if err := os.WriteFile(destPath, data, 0755); err != nil {
		return fmt.Errorf("写入二进制文件失败: %v", err)
	}

	return nil
}

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

// RemoveDirectories 删除相关目录
func (si *SystemInstaller) RemoveDirectories() error {
	dirs := []struct {
		path string
		desc string
	}{
		{"/etc/SmartDNSSort", "配置目录"},
		{"/var/lib/SmartDNSSort", "数据目录"},
		{"/var/log/SmartDNSSort", "日志目录"},
		{"/usr/local/bin/SmartDNSSort", "二进制文件"},
	}

	for _, dir := range dirs {
		if si.config.DryRun {
			fmt.Printf("[DRY-RUN] 将删除：%s (%s)\n", dir.path, dir.desc)
			continue
		}

		si.log("删除：%s", dir.path)
		if err := os.RemoveAll(dir.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除 %s 失败: %v", dir.path, err)
		}
	}

	return nil
}

// Install 执行安装流程
func (si *SystemInstaller) Install() error {
	fmt.Println("============================================")
	fmt.Println("SmartDNSSort 服务安装程序")
	fmt.Println("============================================")

	if si.config.DryRun {
		fmt.Println("[DRY-RUN 模式] 仅预览，不实际执行任何操作\n")
	}

	// 检查权限
	if !si.config.DryRun && !si.IsRoot() {
		return fmt.Errorf("安装需要 root 权限，请使用 sudo 运行")
	}

	// 检查 systemd
	if err := si.CheckSystemd(); err != nil {
		return err
	}

	// 创建目录
	if err := si.CreateDirectories(); err != nil {
		return err
	}

	// 生成默认配置
	if err := si.GenerateDefaultConfig(); err != nil {
		return err
	}

	// 复制二进制文件
	if err := si.CopyBinary(); err != nil {
		return err
	}

	// 写入服务文件
	if err := si.WriteServiceFile(); err != nil {
		return err
	}

	// 重新加载 systemd
	if err := si.ReloadSystemd(); err != nil {
		return err
	}

	// 启用服务
	if err := si.EnableService(); err != nil {
		return err
	}

	// 启动服务
	if err := si.StartService(); err != nil {
		return err
	}

	// 显示安装成功信息
	fmt.Println("\n" + strings.Repeat("=", 44))
	fmt.Println("SmartDNSSort 已成功安装！")
	fmt.Println(strings.Repeat("=", 44))

	if !si.config.DryRun {
		status, _ := si.GetServiceStatus()
		fmt.Printf("✓ 服务状态：%s\n", status)
		fmt.Printf("✓ 配置文件：/etc/SmartDNSSort/config.yaml\n")
		fmt.Printf("✓ 数据目录：/var/lib/SmartDNSSort\n")
		fmt.Printf("✓ 日志目录：/var/log/SmartDNSSort\n")
		fmt.Printf("✓ Web UI：http://localhost:8080\n")
		fmt.Println("\n管理命令：")
		fmt.Println("  查看状态：  sudo systemctl status SmartDNSSort")
		fmt.Println("  查看日志：  sudo journalctl -u SmartDNSSort -f")
		fmt.Println("  卸载服务：  sudo SmartDNSSort -s uninstall")
	}

	return nil
}

// Uninstall 执行卸载流程
func (si *SystemInstaller) Uninstall() error {
	fmt.Println("============================================")
	fmt.Println("SmartDNSSort 服务卸载程序")
	fmt.Println("============================================")

	if si.config.DryRun {
		fmt.Println("[DRY-RUN 模式] 仅预览，不实际执行任何操作\n")
	}

	// 检查权限
	if !si.config.DryRun && !si.IsRoot() {
		return fmt.Errorf("卸载需要 root 权限，请使用 sudo 运行")
	}

	// 停止服务
	if err := si.StopService(); err != nil {
		fmt.Printf("警告：停止服务失败：%v\n", err)
	}

	// 禁用服务
	if err := si.DisableService(); err != nil {
		fmt.Printf("警告：禁用服务失败：%v\n", err)
	}

	// 删除服务文件
	if err := si.RemoveServiceFile(); err != nil {
		return err
	}

	// 重新加载 systemd
	if err := si.ReloadSystemd(); err != nil {
		return err
	}

	// 删除目录和文件
	if err := si.RemoveDirectories(); err != nil {
		return err
	}

	fmt.Println("\n" + strings.Repeat("=", 44))
	fmt.Println("SmartDNSSort 已成功卸载！")
	fmt.Println(strings.Repeat("=", 44))

	return nil
}

// Status 显示服务状态
func (si *SystemInstaller) Status() error {
	fmt.Println("============================================")
	fmt.Println("SmartDNSSort 服务状态")
	fmt.Println("============================================\n")

	status, err := si.GetServiceStatus()
	if err != nil && status == "" {
		return fmt.Errorf("未能获取服务状态，服务可能未安装")
	}

	if status == "active" {
		fmt.Printf("✓ 服务状态：%s (运行中)\n\n", status)

		// 获取详细信息
		details, _ := si.GetServiceDetails()
		fmt.Println(details)

		// 获取最近日志
		fmt.Println("\n最近日志（最后 10 行）：")
		logs, _ := si.GetRecentLogs(10)
		fmt.Println(logs)
	} else {
		fmt.Printf("✗ 服务状态：%s (未运行)\n", status)
		fmt.Println("\n可能的原因：")
		fmt.Println("  1. 服务未安装，请运行：sudo SmartDNSSort -s install")
		fmt.Println("  2. 服务启动失败，查看日志：sudo journalctl -u SmartDNSSort")
		fmt.Println("  3. 配置文件错误，检查：/etc/SmartDNSSort/config.yaml")
	}

	return nil
}
