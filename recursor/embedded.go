package recursor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// 嵌入 Unbound 二进制文件和数据文件
// 目录结构：
// recursor/
//
//	├── binaries/
//	│   ├── linux/unbound (仅 Windows 编译时打包)
//	│   └── windows/unbound.exe (仅 Windows 编译时打包)
//	└── data/
//	    └── root.key (所有平台都打包)
//
// 注意：Linux 上不打包 unbound 二进制文件，因为使用系统安装的 unbound
// 仅打包 root.key 用于 DNSSEC 验证的 fallback
// unboundBinaries 在平台特定的文件中定义：
// - embedded_windows.go: 打包 binaries/windows/* 和 data/*
// - embedded_linux.go: 仅打包 data/*

// unboundDir 存储 unbound 相关文件的目录（相对于主程序）
var unboundDir = "unbound"

// SetUnboundDir 设置 unbound 目录（用于测试或自定义路径）
func SetUnboundDir(dir string) {
	unboundDir = dir
}

// ExtractUnboundBinary 将嵌入的 unbound 二进制文件解压到主程序目录下
// 返回解压后的二进制文件路径
// 仅在 Windows 上支持，Linux 使用系统安装的 unbound
//
// 智能提取策略：
// - 如果二进制文件已存在且大小匹配，则跳过提取（节省资源）
// - 如果文件不存在或大小不匹配，则提取新文件
// - 这样允许用户替换为更新的 unbound 版本
func ExtractUnboundBinary() (string, error) {
	platform := runtime.GOOS
	arch := runtime.GOARCH

	// Linux 上不需要提取二进制文件，使用系统安装的 unbound
	if platform == "linux" {
		return "", fmt.Errorf("ExtractUnboundBinary not supported on Linux (use system unbound)")
	}

	// 验证支持的平台和架构
	if !isSupportedPlatform(platform, arch) {
		return "", fmt.Errorf("unsupported platform: %s/%s (only windows/amd64 is supported)", platform, arch)
	}

	// 确定二进制文件名
	binName := "unbound.exe"

	// 构建嵌入文件路径 - 必须使用正斜杠，embed.FS 总是使用 /
	binPath := "binaries/" + platform + "/" + binName

	// 尝试读取嵌入的二进制文件
	data, err := unboundBinaries.ReadFile(binPath)
	if err != nil {
		return "", fmt.Errorf("unbound binary not found for %s/%s: %w", platform, arch, err)
	}

	// 创建主程序目录下的 unbound 目录
	if err := os.MkdirAll(unboundDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create unbound directory: %w", err)
	}

	outPath := filepath.Join(unboundDir, binName)
	expectedSize := int64(len(data))

	// 检查文件是否已存在
	fileInfo, err := os.Stat(outPath)
	if err == nil {
		// 文件存在，检查大小是否匹配
		if fileInfo.Size() == expectedSize {
			// 文件已存在且大小匹配，跳过提取
			// 这样允许用户替换为更新的 unbound 版本
			return outPath, nil
		}
		// 文件存在但大小不匹配，删除旧文件后重新提取
		if err := os.Remove(outPath); err != nil {
			return "", fmt.Errorf("failed to remove old unbound binary: %w", err)
		}
	}

	// 写入二进制文件
	if err := os.WriteFile(outPath, data, 0755); err != nil {
		return "", fmt.Errorf("failed to write unbound binary: %w", err)
	}

	// 验证文件是否成功写入
	fileInfo, err = os.Stat(outPath)
	if err != nil {
		return "", fmt.Errorf("failed to verify unbound binary after extraction: %w", err)
	}

	if fileInfo.Size() == 0 {
		return "", fmt.Errorf("extracted unbound binary is empty (size: 0)")
	}

	if fileInfo.Size() != expectedSize {
		return "", fmt.Errorf("extracted unbound binary size mismatch: expected %d, got %d", expectedSize, fileInfo.Size())
	}

	return outPath, nil
}

// isSupportedPlatform 检查是否支持该平台和架构
// 仅 Windows x86-64 支持提取嵌入的 unbound 二进制文件
// Linux 使用系统安装的 unbound
func isSupportedPlatform(platform, arch string) bool {
	// 仅支持 Windows x86-64
	return platform == "windows" && arch == "amd64"
}

// GetUnboundConfigDir 获取 Unbound 配置目录（主程序目录下）
func GetUnboundConfigDir() (string, error) {
	if err := os.MkdirAll(unboundDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create unbound directory: %w", err)
	}
	return unboundDir, nil
}

// CleanupUnboundFiles 清理 unbound 目录下的文件
func CleanupUnboundFiles() error {
	if err := os.RemoveAll(unboundDir); err != nil {
		return fmt.Errorf("failed to cleanup unbound files: %w", err)
	}
	return nil
}

// extractRootKey 将嵌入的 root.key 文件提取到 unbound 目录
func extractRootKey() error {
	configDir, err := GetUnboundConfigDir()
	if err != nil {
		return err
	}

	// 读取嵌入的 root.key 文件
	data, err := unboundBinaries.ReadFile("data/root.key")
	if err != nil {
		return fmt.Errorf("root.key not found in embedded data: %w", err)
	}

	// 写入到 unbound 目录
	rootKeyPath := filepath.Join(configDir, "root.key")
	if err := os.WriteFile(rootKeyPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write root.key: %w", err)
	}

	return nil
}

// extractRootZone 将嵌入的 root.zone 文件提取到 unbound 目录
func extractRootZone() error {
	configDir, err := GetUnboundConfigDir()
	if err != nil {
		return err
	}

	// 读取嵌入的 root.zone 文件
	data, err := unboundBinaries.ReadFile("data/root.zone")
	if err != nil {
		return fmt.Errorf("root.zone not found in embedded data: %w", err)
	}

	// 写入到 unbound 目录
	rootZonePath := filepath.Join(configDir, "root.zone")
	if err := os.WriteFile(rootZonePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write root.zone: %w", err)
	}

	return nil
}

// extractRootZoneLinux 将嵌入的 root.zone 文件提取到 /etc/unbound/ (Linux 特定)
func extractRootZoneLinux() error {
	// 确保目录存在
	if err := os.MkdirAll("/etc/unbound", 0755); err != nil {
		return fmt.Errorf("failed to create /etc/unbound: %w", err)
	}

	// 读取嵌入的 root.zone 文件
	data, err := unboundBinaries.ReadFile("data/root.zone")
	if err != nil {
		return fmt.Errorf("root.zone not found in embedded data: %w", err)
	}

	// 写入到 /etc/unbound/
	rootZonePath := "/etc/unbound/root.zone"
	if err := os.WriteFile(rootZonePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write root.zone to %s: %w", rootZonePath, err)
	}

	return nil
}
