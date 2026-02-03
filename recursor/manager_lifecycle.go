package recursor

import (
	"net"
	"smartdnssort/logger"
	"time"
)

// healthCheckLoop 监控进程状态并执行健康检查
func (m *Manager) healthCheckLoop() {
	ticker := time.NewTicker(HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.healthCtx.Done():
			// Context 已取消，退出循环
			logger.Debugf("[Recursor] Health check loop cancelled")
			return

		case <-m.stopCh:
			// 收到停止信号，退出循环
			logger.Debugf("[Recursor] Health check loop received stop signal")
			return

		case <-m.exitCh:
			// 进程意外退出
			m.mu.Lock()
			// 检查是否已被禁用（Stop() 调用时会禁用）
			if !m.enabled {
				m.mu.Unlock()
				// 已禁用，不尝试重启
				logger.Debugf("[Recursor] Process exited but recursor is disabled, not restarting")
				return
			}

			m.restartAttempts++
			m.lastRestartTime = time.Now()
			attempts := m.restartAttempts
			m.mu.Unlock()

			// 检查重启次数是否超过限制
			if attempts > MaxRestartAttempts {
				logger.Errorf("[Recursor] Process exited unexpectedly. Max restart attempts (%d) exceeded, giving up", attempts)
				m.mu.Lock()
				m.enabled = false
				m.mu.Unlock()
				return
			}

			// 计算指数退避延迟：1s, 2s, 4s, 8s, 16s
			backoffDuration := time.Duration(1<<uint(attempts-1)) * time.Second
			if backoffDuration > MaxBackoffDuration {
				backoffDuration = MaxBackoffDuration
			}

			logger.Warnf("[Recursor] Process exited unexpectedly. Restart attempt %d/%d after %v delay...",
				attempts, MaxRestartAttempts, backoffDuration)

			// 等待指数退避时间
			select {
			case <-m.healthCtx.Done():
				// Context 已取消
				return
			case <-m.stopCh:
				// 在等待期间收到停止信号
				return
			case <-time.After(backoffDuration):
				// 继续重启
			}

			// 尝试重启
			if err := m.Start(); err != nil {
				logger.Errorf("[Recursor] Failed to restart (attempt %d): %v", attempts, err)
				// 不继续循环，因为 Start() 失败意味着无法恢复
				// 等待下一次进程退出或停止信号
			} else {
				// 重启成功，当前 goroutine 应该退出
				// 因为 Start() 已经启动了新的 healthCheckLoop
				logger.Infof("[Recursor] Process restarted successfully")
				return
			}

		case <-ticker.C:
			// 定期端口健康检查
			m.performHealthCheck()
		}
	}
}

// performHealthCheck 执行一次健康检查
// 通过尝试连接端口来验证服务实际可用
// 更新最后检查时间戳，用于监控 Unbound 的活跃状态
func (m *Manager) performHealthCheck() {
	// 尝试连接端口验证服务实际可用
	conn, err := net.DialTimeout("tcp", m.GetAddress(), 500*time.Millisecond)
	if err == nil {
		conn.Close()
		m.mu.Lock()
		m.lastHealthCheck = time.Now()
		m.mu.Unlock()
		logger.Debugf("[Recursor] Health check passed")
	} else {
		logger.Warnf("[Recursor] Health check failed: %v", err)
	}
}

// updateRootKeyInBackground 已弃用 - 不再需要定期更新 root.key
//
// 原因：
// 1. DNSSEC 根密钥极少更新（通常几年才变化一次）
// 2. Unbound 的 auto-trust-anchor-file 会在运行期间自动监控文件变化
// 3. 如果文件更新，unbound 会自动重新加载
// 4. 无需应用层干预，完全由 unbound 管理
//
// 此方法已被移除，保留注释以说明设计决策
// 启动时通过 ensureRootKey() 确保文件存在即可
