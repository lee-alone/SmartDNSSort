package recursor

import (
	"fmt"
	"os"
	"os/exec"
	"smartdnssort/logger"
)

// StopService 停止 unbound 服务
func (sm *SystemManager) StopService() error {
	cmd := exec.Command("systemctl", "stop", "unbound")
	if err := cmd.Run(); err != nil {
		// 如果 systemctl 失败，尝试 killall
		killCmd := exec.Command("killall", "unbound")
		if err := killCmd.Run(); err != nil {
			// 两种方法都失败，可能 unbound 没有运行，这不是错误
			return nil
		}
	}
	return nil
}

// backupConfig 备份 unbound 配置文件
// 使用 Go 标准库而不是 Shell 命令
func (sm *SystemManager) backupConfig() error {
	src := "/etc/unbound/unbound.conf"
	dst := "/etc/unbound/unbound.conf.bak"

	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在，这不是错误
			return nil
		}
		return fmt.Errorf("failed to read config file %s: %w", src, err)
	}

	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup config to %s: %w", dst, err)
	}

	return nil
}

// handleExistingUnbound 处理已存在的 unbound
// 流程：
// 1. 停止服务
// 2. 禁用自启
// 3. 备份配置
func (sm *SystemManager) handleExistingUnbound() error {
	// 步骤 1：停止服务
	if err := sm.StopService(); err != nil {
		return fmt.Errorf("failed to stop unbound service: %w", err)
	}

	// 步骤 2：禁用自启
	if err := sm.DisableAutoStart(); err != nil {
		return fmt.Errorf("failed to disable autostart: %w", err)
	}

	// 步骤 3：备份配置
	if err := sm.backupConfig(); err != nil {
		// 备份失败不应该中断整个流程
		logger.Warnf("[SystemManager] Failed to backup config: %v", err)
	}

	return nil
}

// DisableAutoStart 禁用自启
func (sm *SystemManager) DisableAutoStart() error {
	cmd := exec.Command("systemctl", "disable", "unbound")
	if err := cmd.Run(); err != nil {
		// 如果 systemctl 失败，可能是权限问题或 systemctl 不可用
		// 尝试其他方法（如 chkconfig）
		altCmd := exec.Command("chkconfig", "unbound", "off")
		if err := altCmd.Run(); err != nil {
			// 两种方法都失败，记录警告但不中断
			return fmt.Errorf("failed to disable autostart: %w", err)
		}
	}
	return nil
}
