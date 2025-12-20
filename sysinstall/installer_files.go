package sysinstall

import (
	"fmt"
	"os"
)

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

// CopyWebFiles 复制 Web 静态文件到系统目录
func (si *SystemInstaller) CopyWebFiles() error {
	webDest := "/var/lib/SmartDNSSort/web"

	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将复制 Web 文件到：%s\n", webDest)
		return nil
	}

	si.log("复制 Web 文件到：%s", webDest)

	// 查找源 Web 目录
	sourcePaths := []string{
		"web",
		"./web",
	}

	var sourceDir string
	for _, path := range sourcePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			sourceDir = path
			break
		}
	}

	if sourceDir == "" {
		si.log("警告：找不到 Web 源文件目录，跳过复制")
		return nil
	}

	// 创建目标目录
	if err := os.MkdirAll(webDest, 0755); err != nil {
		return fmt.Errorf("创建 Web 目录失败: %v", err)
	}

	// 递归复制目录中的所有文件
	if err := si.copyDirRecursive(sourceDir, webDest); err != nil {
		return fmt.Errorf("复制 Web 文件失败: %v", err)
	}

	return nil
}

// copyDirRecursive 递归复制目录
func (si *SystemInstaller) copyDirRecursive(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := src + "/" + entry.Name()
		dstPath := dst + "/" + entry.Name()

		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			if err := si.copyDirRecursive(srcPath, dstPath); err != nil {
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
