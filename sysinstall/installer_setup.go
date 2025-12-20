package sysinstall

import (
	"fmt"
	"os"
	"os/exec"
)

// CheckSystemd 检查系统是否支持 systemd
func (si *SystemInstaller) CheckSystemd() error {
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
		{"/var/lib/SmartDNSSort/web", 0755, "Web UI 目录"},
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
