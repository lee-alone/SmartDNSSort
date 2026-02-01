package recursor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"smartdnssort/logger"
)

// DebugWindowsUnbound 诊断 Windows 上的 unbound 启动问题
func DebugWindowsUnbound() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("this debug function is only for Windows")
	}

	logger.Infof("[Debug] Starting Windows Unbound diagnostics...")

	// 1. 检查二进制文件
	logger.Infof("[Debug] Step 1: Checking unbound binary...")
	unboundPath, err := ExtractUnboundBinary()
	if err != nil {
		logger.Errorf("[Debug] Failed to extract unbound binary: %v", err)
		return err
	}
	logger.Infof("[Debug] Unbound binary extracted to: %s", unboundPath)

	// 验证文件
	fileInfo, err := os.Stat(unboundPath)
	if err != nil {
		logger.Errorf("[Debug] Unbound binary not found: %v", err)
		return err
	}
	logger.Infof("[Debug] Unbound binary size: %d bytes", fileInfo.Size())

	// 2. 检查 root.key
	logger.Infof("[Debug] Step 2: Checking root.key...")
	if err := extractRootKey(); err != nil {
		logger.Errorf("[Debug] Failed to extract root.key: %v", err)
		return err
	}

	configDir, _ := GetUnboundConfigDir()
	rootKeyPath := filepath.Join(configDir, "root.key")
	fileInfo, err = os.Stat(rootKeyPath)
	if err != nil {
		logger.Errorf("[Debug] root.key not found: %v", err)
		return err
	}
	logger.Infof("[Debug] root.key size: %d bytes", fileInfo.Size())

	// 3. 生成配置文件
	logger.Infof("[Debug] Step 3: Generating config file...")
	mgr := NewManager(5353)
	configPath, err := mgr.generateConfig()
	if err != nil {
		logger.Errorf("[Debug] Failed to generate config: %v", err)
		return err
	}
	logger.Infof("[Debug] Config file generated at: %s", configPath)

	// 验证配置文件
	fileInfo, err = os.Stat(configPath)
	if err != nil {
		logger.Errorf("[Debug] Config file not found: %v", err)
		return err
	}
	logger.Infof("[Debug] Config file size: %d bytes", fileInfo.Size())

	// 读取配置文件内容（前500字符）
	content, err := os.ReadFile(configPath)
	if err != nil {
		logger.Errorf("[Debug] Failed to read config file: %v", err)
		return err
	}
	if len(content) > 500 {
		logger.Infof("[Debug] Config file content (first 500 chars):\n%s...", string(content[:500]))
	} else {
		logger.Infof("[Debug] Config file content:\n%s", string(content))
	}

	// 4. 尝试启动 unbound
	logger.Infof("[Debug] Step 4: Attempting to start unbound...")
	cmd := exec.Command(unboundPath, "-c", configPath, "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		logger.Errorf("[Debug] Failed to start unbound: %v", err)
		return err
	}
	logger.Infof("[Debug] Unbound process started (PID: %d)", cmd.Process.Pid)

	// 5. 等待一下，看看进程是否还在运行
	logger.Infof("[Debug] Step 5: Checking if process is still running...")
	// 给进程一些时间来启动或失败
	go func() {
		err := cmd.Wait()
		if err != nil {
			logger.Errorf("[Debug] Unbound process exited with error: %v", err)
		} else {
			logger.Infof("[Debug] Unbound process exited normally")
		}
	}()

	logger.Infof("[Debug] Diagnostics complete. Unbound should be running on 127.0.0.1:5353")
	return nil
}
