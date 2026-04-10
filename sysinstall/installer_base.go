package sysinstall

import (
	"fmt"
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
