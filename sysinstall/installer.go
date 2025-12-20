package sysinstall

import (
	"fmt"
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
	log    func(format string, args ...any)
}

// NewSystemInstaller 创建新的系统安装器
func NewSystemInstaller(cfg InstallerConfig) *SystemInstaller {
	si := &SystemInstaller{
		config: cfg,
	}

	// 日志函数
	if cfg.Verbose {
		si.log = func(format string, args ...any) {
			fmt.Printf("[INFO] "+format+"\n", args...)
		}
	} else {
		si.log = func(format string, args ...any) {}
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

// Install 执行安装流程
func (si *SystemInstaller) Install() error {
	fmt.Println("============================================")
	fmt.Println("SmartDNSSort 服务安装程序")
	fmt.Println("============================================")

	if si.config.DryRun {
		fmt.Println("[DRY-RUN 模式] 仅预览，不实际执行任何操作")
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

	// 复制 Web 文件
	if err := si.CopyWebFiles(); err != nil {
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
		fmt.Println("[DRY-RUN 模式] 仅预览，不实际执行任何操作")
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
