package recursor

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Manager 管理嵌入的 Unbound 递归解析器
type Manager struct {
	mu              sync.RWMutex
	cmd             *exec.Cmd
	unboundPath     string
	configPath      string
	port            int
	enabled         bool
	stopCh          chan struct{}
	lastHealthCheck time.Time
}

// NewManager 创建新的 Manager
func NewManager(port int) *Manager {
	return &Manager{
		port:   port,
		stopCh: make(chan struct{}),
	}
}

// Start 启动嵌入的 Unbound 进程
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.enabled {
		return fmt.Errorf("recursor already running")
	}

	// 1. 解压 Unbound 二进制文件
	unboundPath, err := ExtractUnboundBinary()
	if err != nil {
		return fmt.Errorf("failed to extract unbound binary: %w", err)
	}
	m.unboundPath = unboundPath

	// 2. 生成配置文件
	configPath, err := m.generateConfig()
	if err != nil {
		return fmt.Errorf("failed to generate unbound config: %w", err)
	}
	m.configPath = configPath

	// 3. 启动 Unbound 进程
	// -d: 前台运行（便于日志和进程管理）
	// -c: 指定配置文件
	m.cmd = exec.Command(m.unboundPath, "-c", m.configPath, "-d")

	// 设置输出（可选，用于调试）
	m.cmd.Stdout = os.Stdout
	m.cmd.Stderr = os.Stderr

	if err := m.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start unbound process: %w", err)
	}

	m.enabled = true
	m.lastHealthCheck = time.Now()

	// 4. 等待 Unbound 启动完成（检查端口是否可用）
	if err := m.waitForReady(5 * time.Second); err != nil {
		return fmt.Errorf("unbound may not be ready: %w", err)
	}

	// 5. 启动健康检查 goroutine
	go m.healthCheckLoop()

	return nil
}

// Stop 停止 Unbound 进程
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled {
		return nil
	}

	// 1. 停止健康检查
	close(m.stopCh)

	// 2. 优雅停止进程
	if m.cmd != nil && m.cmd.Process != nil {
		// 发送 SIGTERM 信号
		if err := m.cmd.Process.Signal(os.Interrupt); err != nil {
			return fmt.Errorf("failed to signal unbound: %w", err)
		}

		// 等待进程退出（最多 5 秒）
		done := make(chan error, 1)
		go func() {
			done <- m.cmd.Wait()
		}()

		select {
		case <-time.After(5 * time.Second):
			if err := m.cmd.Process.Kill(); err != nil {
				return fmt.Errorf("failed to kill unbound: %w", err)
			}
		case err := <-done:
			if err != nil {
				// 进程已退出，忽略错误
			}
		}
	}

	// 3. 清理临时文件
	if m.configPath != "" {
		if err := os.Remove(m.configPath); err != nil {
			// 忽略删除错误
		}
	}

	if m.unboundPath != "" {
		if err := os.Remove(m.unboundPath); err != nil {
			// 忽略删除错误
		}
	}

	m.enabled = false
	return nil
}

// generateConfig 生成 Unbound 配置文件
func (m *Manager) generateConfig() (string, error) {
	configDir, err := GetUnboundConfigDir()
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(configDir, "unbound.conf")

	// 生成配置内容
	config := fmt.Sprintf(`# SmartDNSSort Embedded Unbound Configuration
# Auto-generated, do not edit manually

server:
    # 监听配置
    port: %d
    do-ip4: yes
    do-ip6: no
    do-udp: yes
    do-tcp: yes
    
    # 仅本地访问
    interface: 127.0.0.1
    
    # 性能优化
    num-threads: 4
    msg-cache-size: 100m
    rrset-cache-size: 200m
    cache-min-ttl: 60
    cache-max-ttl: 86400
    
    # DNSSEC 验证
    module-config: "validator iterator"
    
    # 日志配置
    verbosity: 1
    log-queries: no
    log-replies: no
    
    # 安全配置
    hide-identity: yes
    hide-version: yes
    
    # 访问控制
    access-control: 127.0.0.1 allow
    access-control: ::1 allow
    access-control: 0.0.0.0/0 deny
    access-control: ::/0 deny
`, m.port)

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return "", fmt.Errorf("failed to write config file: %w", err)
	}

	return configPath, nil
}

// waitForReady 等待 Unbound 启动完成
func (m *Manager) waitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for unbound to be ready")
		}

		// 尝试连接到 Unbound 端口
		conn, err := net.DialTimeout("udp", fmt.Sprintf("127.0.0.1:%d", m.port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// healthCheckLoop 定期检查 Unbound 进程健康状态
func (m *Manager) healthCheckLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.performHealthCheck()
		}
	}
}

// performHealthCheck 执行一次健康检查
func (m *Manager) performHealthCheck() {
	m.mu.RLock()
	if !m.enabled || m.cmd == nil || m.cmd.Process == nil {
		m.mu.RUnlock()
		return
	}
	cmd := m.cmd
	m.mu.RUnlock()

	// 检查进程是否仍在运行
	if err := cmd.Process.Signal(os.Signal(nil)); err != nil {
		// 进程已死亡，尝试重启
		m.mu.Lock()
		m.enabled = false
		m.mu.Unlock()

		if err := m.Start(); err != nil {
			// 重启失败，记录错误
			return
		}
		return
	}

	m.mu.Lock()
	m.lastHealthCheck = time.Now()
	m.mu.Unlock()
}

// IsEnabled 检查 Recursor 是否启用
func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// GetPort 获取 Unbound 监听端口
func (m *Manager) GetPort() int {
	return m.port
}

// GetAddress 获取 Unbound 地址
func (m *Manager) GetAddress() string {
	return fmt.Sprintf("127.0.0.1:%d", m.port)
}

// GetLastHealthCheck 获取最后一次健康检查时间
func (m *Manager) GetLastHealthCheck() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastHealthCheck
}

// Query 执行 DNS 查询（用于测试）
func (m *Manager) Query(ctx context.Context, domain string) error {
	if !m.IsEnabled() {
		return fmt.Errorf("recursor not enabled")
	}

	// 这里可以添加实际的 DNS 查询逻辑
	// 用于验证 Unbound 是否正常工作
	return nil
}
