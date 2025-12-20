package sysinstall

import "path/filepath"

const (
	// 标准目录
	DefaultConfigDir = "/etc/SmartDNSSort"
	DefaultDataDir   = "/var/lib/SmartDNSSort"
	DefaultLogDir    = "/var/log/SmartDNSSort"
	DefaultBinaryDir = "/usr/local/bin"

	// 文件与服务名
	BinaryName  = "SmartDNSSort"
	ServiceName = "SmartDNSSort"
)

// DefaultConfigPath 获取默认配置文件完整路径
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir, "config.yaml")
}

// DefaultBinaryPath 获取默认二进制文件完整路径
func DefaultBinaryPath() string {
	return filepath.Join(DefaultBinaryDir, BinaryName)
}
