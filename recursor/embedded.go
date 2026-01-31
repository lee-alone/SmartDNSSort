package recursor

import (
	"embed"
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
//	│   ├── linux/unbound
//	│   └── windows/unbound.exe
//	└── data/
//	    └── root.key
//
//go:embed binaries/* data/*
var unboundBinaries embed.FS

// unboundDir 存储 unbound 相关文件的目录（相对于主程序）
var unboundDir = "unbound"

// SetUnboundDir 设置 unbound 目录（用于测试或自定义路径）
func SetUnboundDir(dir string) {
	unboundDir = dir
}

// ExtractUnboundBinary 将嵌入的 unbound 二进制文件解压到主程序目录下
// 返回解压后的二进制文件路径
// 仅支持 Linux x86-64 和 Windows x86-64
func ExtractUnboundBinary() (string, error) {
	platform := runtime.GOOS
	arch := runtime.GOARCH

	// 验证支持的平台和架构
	if !isSupportedPlatform(platform, arch) {
		return "", fmt.Errorf("unsupported platform: %s/%s (only linux/amd64 and windows/amd64 are supported)", platform, arch)
	}

	// 确定二进制文件名
	binName := "unbound"
	if platform == "windows" {
		binName = "unbound.exe"
	}

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

	// 写入二进制文件
	outPath := filepath.Join(unboundDir, binName)
	if err := os.WriteFile(outPath, data, 0755); err != nil {
		return "", fmt.Errorf("failed to write unbound binary: %w", err)
	}

	return outPath, nil
}

// isSupportedPlatform 检查是否支持该平台和架构
func isSupportedPlatform(platform, arch string) bool {
	// 仅支持 Linux x86-64 和 Windows x86-64
	return (platform == "linux" && arch == "amd64") ||
		(platform == "windows" && arch == "amd64")
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
