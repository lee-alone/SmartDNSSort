//go:build windows
// +build windows

package sysinstall

import (
	"os"
	"path/filepath"
)

const (
	// Windows 标准目录
	DefaultConfigDir = "C:\\ProgramData\\SmartDNSSort"
	DefaultDataDir   = "C:\\ProgramData\\SmartDNSSort\\data"
	DefaultLogDir    = "C:\\ProgramData\\SmartDNSSort\\logs"
	DefaultBinaryDir = "C:\\Program Files\\SmartDNSSort"
	DefaultWebDir    = "C:\\Program Files\\SmartDNSSort\\web"

	// 文件与服务名
	BinaryName  = "SmartDNSSort.exe"
	ServiceName = "SmartDNSSort"
	ServiceDesc = "SmartDNSSort DNS Server - Intelligent DNS Sorting Service"
)

// DefaultConfigPath 获取默认配置文件完整路径
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir, "config.yaml")
}

// DefaultBinaryPath 获取默认二进制文件完整路径
func DefaultBinaryPath() string {
	return filepath.Join(DefaultBinaryDir, BinaryName)
}

// GetExePath 获取当前可执行文件路径
func GetExePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// GetExeDir 获取当前可执行文件所在目录
func GetExeDir() string {
	exe := GetExePath()
	if exe == "" {
		return ""
	}
	return filepath.Dir(exe)
}
