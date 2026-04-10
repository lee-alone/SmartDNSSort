//go:build windows
// +build windows

package sysinstall

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IsAdmin 检查是否以管理员权限运行
func (si *SystemInstaller) IsAdmin() bool {
	// 在 Windows 上，尝试执行需要管理员权限的操作来检测
	cmd := exec.Command("net", "session")
	err := cmd.Run()
	return err == nil
}

// Install 执行 Windows 服务安装流程
func (si *SystemInstaller) Install() error {
	fmt.Println("============================================")
	fmt.Println("SmartDNSSort 服务安装程序 (Windows)")
	fmt.Println("============================================")

	if si.config.DryRun {
		fmt.Println("[DRY-RUN 模式] 仅预览，不实际执行任何操作")
	}

	// 检查管理员权限
	if !si.config.DryRun && !si.IsAdmin() {
		return fmt.Errorf("安装需要管理员权限，请右键以管理员身份运行")
	}

	// 在复制文件之前先尝试停止有可能卡死的旧服务，释放对 exe 的占用锁
	if !si.config.DryRun {
		si.log("检查并停止旧服务...")
		_ = si.StopService()
		time.Sleep(1 * time.Second)
	}

	// 创建目录
	if err := si.CreateDirectories(); err != nil {
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

	// 生成默认配置
	if err := si.GenerateDefaultConfig(); err != nil {
		return err
	}

	// 安装服务
	if err := si.InstallService(); err != nil {
		return err
	}

	// 打印服务配置信息
	fmt.Println("\n服务配置信息:")
	fmt.Printf("  二进制文件: %s\n", DefaultBinaryPath())
	fmt.Printf("  配置文件: %s\n", DefaultConfigPath())
	fmt.Printf("  数据目录: %s\n", DefaultDataDir)
	fmt.Printf("  日志目录: %s\n", DefaultLogDir)
	
	// 等待服务注册完成
	fmt.Println("\n等待服务注册完成...")
	time.Sleep(2 * time.Second)

	// 启动服务
	if err := si.StartService(); err != nil {
		fmt.Printf("\n⚠ 警告：启动服务失败：%v\n", err)
		fmt.Println("\n可能的原因和解决方法:")
		fmt.Println("  1. 配置文件问题 - 检查: " + DefaultConfigPath())
		fmt.Println("  2. 端口占用 - 确保 53 端口未被其他 DNS 服务占用")
		fmt.Println("  3. 权限问题 - 确保以管理员身份运行")
		fmt.Println("\n手动启动服务: sc start " + ServiceName)
		fmt.Println("查看服务日志: 事件查看器 -> Windows 日志 -> 应用程序")
		fmt.Println("\n建议: 先尝试手动启动服务以查看详细错误信息")
	}

	// 显示安装成功信息
	fmt.Println("\n" + strings.Repeat("=", 44))
	fmt.Println("SmartDNSSort 已成功安装！")
	fmt.Println(strings.Repeat("=", 44))

	if !si.config.DryRun {
		status, _ := si.GetServiceStatus()
		fmt.Printf("✓ 服务状态：%s\n", status)
		fmt.Printf("✓ 配置文件：%s\n", DefaultConfigPath())
		fmt.Printf("✓ 数据目录：%s\n", DefaultDataDir)
		fmt.Printf("✓ 日志目录：%s\n", DefaultLogDir)
		fmt.Printf("✓ Web UI：http://localhost:8080\n")
		fmt.Println("\n管理命令：")
		fmt.Println(" 查看状态： sc query SmartDNSSort")
		fmt.Println(" 启动服务： sc start SmartDNSSort")
		fmt.Println(" 停止服务： sc stop SmartDNSSort")
		fmt.Println(" 卸载服务： SmartDNSSort -s uninstall")
	}

	return nil
}

// Uninstall 执行 Windows 服务卸载流程
func (si *SystemInstaller) Uninstall() error {
	fmt.Println("============================================")
	fmt.Println("SmartDNSSort 服务卸载程序 (Windows)")
	fmt.Println("============================================")

	if si.config.DryRun {
		fmt.Println("[DRY-RUN 模式] 仅预览，不实际执行任何操作")
	}

	// 检查管理员权限
	if !si.config.DryRun && !si.IsAdmin() {
		return fmt.Errorf("卸载需要管理员权限，请右键以管理员身份运行")
	}

	// 停止服务
	if err := si.StopService(); err != nil {
		fmt.Printf("警告：停止服务失败：%v\n", err)
	}

	// 删除服务
	if err := si.UninstallService(); err != nil {
		fmt.Printf("警告：删除服务失败：%v\n", err)
	}

	// 删除目录和文件
	if err := si.RemoveDirectories(); err != nil {
		fmt.Printf("警告：删除目录失败：%v\n", err)
	}

	fmt.Println("\n" + strings.Repeat("=", 44))
	fmt.Println("SmartDNSSort 已成功卸载！")
	fmt.Println(strings.Repeat("=", 44))

	return nil
}

// Status 显示服务状态
func (si *SystemInstaller) Status() error {
	fmt.Println("============================================")
	fmt.Println("SmartDNSSort 服务状态 (Windows)")
	fmt.Println("============================================")

	status, err := si.GetServiceStatus()
	if err != nil && status == "" {
		return fmt.Errorf("未能获取服务状态，服务可能未安装")
	}

	fmt.Printf("服务状态：%s\n", status)

	switch status {
	case "RUNNING":
		fmt.Println("\n✓ 服务正在运行")
		fmt.Printf("✓ 配置文件：%s\n", DefaultConfigPath())
		fmt.Printf("✓ 数据目录：%s\n", DefaultDataDir)
		fmt.Printf("✓ Web UI：http://localhost:8080\n")
	case "STOPPED":
		fmt.Println("\n✗ 服务已停止")
		fmt.Println("\n启动服务：sc start SmartDNSSort")
	case "NOT_FOUND":
		fmt.Println("\n✗ 服务未安装")
		fmt.Println("\n安装服务：SmartDNSSort -s install")
	default:
		fmt.Printf("\n⚠ 服务状态未知：%s\n", status)
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
		{DefaultConfigDir, 0755, "配置目录"},
		{DefaultDataDir, 0755, "数据目录"},
		{DefaultLogDir, 0755, "日志目录"},
		{DefaultWebDir, 0755, "Web UI 目录"},
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

// CopyBinary 复制二进制文件
func (si *SystemInstaller) CopyBinary() error {
	srcPath := GetExePath()
	if srcPath == "" {
		return fmt.Errorf("无法获取当前可执行文件路径")
	}

	dstPath := DefaultBinaryPath()

	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将复制二进制文件：%s -> %s\n", srcPath, dstPath)
		return nil
	}

	// 确保目标目录存在
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("创建目标目录失败: %v", err)
	}

	si.log("复制二进制文件：%s -> %s", srcPath, dstPath)

	// 读取源文件
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("读取源文件失败: %v", err)
	}

	// 写入目标文件
	if err := os.WriteFile(dstPath, data, 0755); err != nil {
		return fmt.Errorf("写入目标文件失败: %v", err)
	}

	return nil
}

// CopyWebFiles 复制 Web 静态文件
func (si *SystemInstaller) CopyWebFiles() error {
	srcDir := filepath.Join(GetExeDir(), "web")
	dstDir := DefaultWebDir

	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将复制 Web 文件：%s -> %s\n", srcDir, dstDir)
		return nil
	}

	// 检查源目录是否存在
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		si.log("Web 源目录不存在，跳过：%s", srcDir)
		return nil
	}

	si.log("复制 Web 文件：%s -> %s", srcDir, dstDir)
	return copyDirRecursive(srcDir, dstDir)
}

// GenerateDefaultConfig 生成默认配置文件
func (si *SystemInstaller) GenerateDefaultConfig() error {
	configPath := DefaultConfigPath()

	// 检查文件是否已存在
	if _, err := os.Stat(configPath); err == nil {
		si.log("配置文件已存在：%s", configPath)
		return nil
	}

	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将创建默认配置文件：%s\n", configPath)
		return nil
	}

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
`

	si.log("创建默认配置文件：%s", configPath)
	if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}

	return nil
}

// RemoveDirectories 删除相关目录
func (si *SystemInstaller) RemoveDirectories() error {
	dirs := []string{
		DefaultBinaryDir,
		DefaultConfigDir,
	}

	for _, dir := range dirs {
		if si.config.DryRun {
			fmt.Printf("[DRY-RUN] 将删除目录：%s\n", dir)
			continue
		}

		si.log("删除目录：%s", dir)
		if err := os.RemoveAll(dir); err != nil {
			fmt.Printf("警告：删除目录失败 %s: %v\n", dir, err)
		}
	}

	return nil
}

// copyDirRecursive 递归复制目录
func copyDirRecursive(src, dst string) error {
	// 确保目标目录存在
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDirRecursive(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}

	return nil
}
