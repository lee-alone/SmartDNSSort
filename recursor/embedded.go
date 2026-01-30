package recursor

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// 嵌入 Unbound 二进制文件
// 目录结构：
// recursor/binaries/
//
//	├── linux/unbound
//	└── windows/unbound.exe
//
//go:embed binaries/linux/unbound binaries/windows/unbound.exe
var unboundBinaries embed.FS

// ExtractUnboundBinary 将嵌入的 unbound 二进制文件解压到临时目录
// 返回解压后的二进制文件路径
func ExtractUnboundBinary() (string, error) {
	platform := runtime.GOOS
	arch := runtime.GOARCH

	// 确定二进制文件名
	binName := "unbound"
	if platform == "windows" {
		binName = "unbound.exe"
	}

	// 构建嵌入文件路径
	binPath := filepath.Join("binaries", platform, binName)

	// 尝试读取嵌入的二进制文件
	data, err := unboundBinaries.ReadFile(binPath)
	if err != nil {
		return "", fmt.Errorf("unbound binary not found for %s/%s: %w", platform, arch, err)
	}

	// 创建临时目录
	tmpDir := filepath.Join(os.TempDir(), "smartdnssort-unbound")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// 写入二进制文件
	outPath := filepath.Join(tmpDir, binName)
	if err := os.WriteFile(outPath, data, 0755); err != nil {
		return "", fmt.Errorf("failed to write unbound binary: %w", err)
	}

	return outPath, nil
}

// GetUnboundConfigDir 获取 Unbound 配置目录
func GetUnboundConfigDir() (string, error) {
	tmpDir := filepath.Join(os.TempDir(), "smartdnssort-unbound")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return tmpDir, nil
}

// CleanupUnboundFiles 清理临时文件
func CleanupUnboundFiles() error {
	tmpDir := filepath.Join(os.TempDir(), "smartdnssort-unbound")
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("failed to cleanup unbound files: %w", err)
	}
	return nil
}
