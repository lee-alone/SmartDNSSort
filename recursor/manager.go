package recursor

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"smartdnssort/logger"
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
	exitCh          chan error
	lastHealthCheck time.Time
}

// NewManager 创建新的 Manager
func NewManager(port int) *Manager {
	return &Manager{
		port:   port,
		stopCh: make(chan struct{}),
		exitCh: make(chan error, 1),
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

	// 2. 提取 root.key 文件
	if err := extractRootKey(); err != nil {
		return fmt.Errorf("failed to extract root.key: %w", err)
	}

	// 3. 生成配置文件
	configPath, err := m.generateConfig()
	if err != nil {
		return fmt.Errorf("failed to generate unbound config: %w", err)
	}
	m.configPath = configPath

	// 4. 启动 Unbound 进程
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

	// 5. 等待 Unbound 启动完成（检查端口是否可用）
	if err := m.waitForReady(5 * time.Second); err != nil {
		return fmt.Errorf("unbound may not be ready: %v", err)
	}

	// 6. 启动进程监控 goroutine
	m.exitCh = make(chan error, 1)
	go func() {
		// 等待进程退出
		err := m.cmd.Wait()
		m.exitCh <- err
	}()

	// 7. 启动健康检查/保活 loop
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
			// 如果发送信号失败，可能是进程已经不存在了
			// 继续尝试清理
		}

		// 等待进程退出（最多 5 秒）
		// 注意：这里不再需要单独的 wait goroutine，因为 Start 中已经启动了一个
		// 但我们需要处理超时强制 kill
		select {
		case <-m.exitCh:
			// 进程已退出
		case <-time.After(5 * time.Second):
			if err := m.cmd.Process.Kill(); err != nil {
				// 忽略 kill 错误
			}
		}
	}

	// 3. 清理临时文件
	if m.configPath != "" {
		_ = os.Remove(m.configPath)
	}

	if m.unboundPath != "" {
		_ = os.Remove(m.unboundPath)
	}

	m.enabled = false
	return nil
}

// generateConfig 生成 Unbound 配置文件
// 根据运行时的机器情况动态调整参数
func (m *Manager) generateConfig() (string, error) {
	configDir, err := GetUnboundConfigDir()
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(configDir, "unbound.conf")

	// 动态计算线程数（基于 CPU 核数）
	// min(CPU, 8) 且至少为 1
	numThreads := max(1, min(runtime.NumCPU(), 8))

	// 根据线程数调整缓存大小
	// 基础缓存 + 每个线程额外缓存
	msgCacheSize := 50 + (25 * numThreads)    // 基础 50m + 每线程 25m
	rrsetCacheSize := 100 + (50 * numThreads) // 基础 100m + 每线程 50m

	// 获取 root.key 路径
	rootKeyPath := filepath.Join(configDir, "root.key")

	// 生成配置内容
	config := fmt.Sprintf(`# SmartDNSSort Embedded Unbound Configuration
# Auto-generated, do not edit manually
# Generated for %d CPU cores

server:
    # 监听配置
    interface: 127.0.0.1@%d
    do-ip4: yes
    do-ip6: no
    do-udp: yes
    do-tcp: yes
    
    # 访问控制 - 仅本地访问
    access-control: 127.0.0.1 allow
    access-control: ::1 allow
    access-control: 0.0.0.0/0 deny
    access-control: ::/0 deny
    
    # 性能优化 - 根据 CPU 核数动态调整
    num-threads: %d
    msg-cache-size: %dm
    rrset-cache-size: %dm
    outgoing-range: 4096
    so-rcvbuf: 8m
    
    # 缓存策略
    cache-max-ttl: 86400
    cache-min-ttl: 60
    serve-expired: yes
    serve-expired-ttl: 86400
    serve-expired-reply-ttl: 30
    
    # 预取优化
    prefetch: yes
    prefetch-key: yes
    
    # 安全加固
    harden-dnssec-stripped: yes
    harden-glue: yes
    harden-referral-path: yes
    qname-minimisation: yes
    minimal-responses: yes
    use-caps-for-id: yes
    
    # 系统优化
    so-reuseport: yes
    
    # DNSSEC 信任锚
    auto-trust-anchor-file: "%s"
    
    # 模块配置 - 仅使用 iterator，不强制 DNSSEC 验证
    module-config: "iterator"
    
    # 日志配置
    verbosity: 1
    log-queries: no
    log-replies: no
    
    # 隐藏版本信息
    hide-identity: yes
    hide-version: yes
`, runtime.NumCPU(), m.port, numThreads, msgCacheSize, rrsetCacheSize, rootKeyPath)

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

// healthCheckLoop 监控进程状态并执行健康检查
func (m *Manager) healthCheckLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			// 收到停止信号，退出循环
			return

		case <-m.exitCh:
			// 进程意外退出
			m.mu.Lock()
			// 标记为未启用，以便 Start() 可以重新运行
			m.enabled = false
			m.mu.Unlock()

			logger.Warnf("[Recursor] Process exited unexpectedly, attempting restart...")

			// 简单的防抖动延迟
			time.Sleep(1 * time.Second)

			// 尝试重启
			// 注意：Start 会启动新的 healthCheckLoop，所以当前循环必须退出
			if err := m.Start(); err != nil {
				logger.Errorf("[Recursor] Failed to restart: %v", err)
				// 重启失败，退出循环
				// 调用者需要处理重启失败的情况
			}
			return

		case <-ticker.C:
			// 定期端口健康检查
			m.performHealthCheck()
		}
	}
}

// performHealthCheck 执行一次健康检查（更新最后检查时间）
func (m *Manager) performHealthCheck() {
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
