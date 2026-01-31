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

// InstallState 安装状态
type InstallState int

const (
	StateNotInstalled InstallState = iota
	StateInstalling
	StateInstalled
	StateError
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
	startTime       time.Time // 进程启动时间

	// 新增字段 - 系统级管理
	sysManager    *SystemManager
	configGen     *ConfigGenerator
	isSystemLevel bool
	installState  InstallState
}

// NewManager 创建新的 Manager
func NewManager(port int) *Manager {
	return &Manager{
		port:         port,
		stopCh:       make(chan struct{}),
		exitCh:       make(chan error, 1),
		sysManager:   NewSystemManager(),
		installState: StateNotInstalled,
	}
}

// Start 启动嵌入的 Unbound 进程
// 二进制文件和配置文件解压到主程序目录下的 unbound/ 子目录
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.enabled {
		return fmt.Errorf("recursor already running")
	}

	// 首次启用时执行初始化
	if m.installState == StateNotInstalled {
		m.installState = StateInstalling
		m.mu.Unlock()
		if err := m.Initialize(); err != nil {
			m.mu.Lock()
			m.installState = StateError
			return err
		}
		m.mu.Lock()
		m.installState = StateInstalled
	}

	// 1. 解压 Unbound 二进制文件到主程序目录（仅 Windows）
	if runtime.GOOS == "windows" {
		unboundPath, err := ExtractUnboundBinary()
		if err != nil {
			return fmt.Errorf("failed to extract unbound binary: %w", err)
		}
		m.unboundPath = unboundPath
		logger.Infof("[Recursor] Extracted unbound binary to: %s", unboundPath)

		// 提取 root.key 文件
		if err := extractRootKey(); err != nil {
			return fmt.Errorf("failed to extract root.key: %w", err)
		}
	} else {
		// Linux: 使用系统级 unbound
		if m.sysManager != nil {
			m.unboundPath = m.sysManager.unboundPath
		}
	}

	// 2. 生成配置文件
	configPath, err := m.generateConfig()
	if err != nil {
		return fmt.Errorf("failed to generate unbound config: %w", err)
	}
	m.configPath = configPath
	logger.Infof("[Recursor] Generated config file: %s", configPath)

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
	m.startTime = time.Now()
	m.lastHealthCheck = time.Now()
	logger.Infof("[Recursor] Unbound process started (PID: %d)", m.cmd.Process.Pid)

	// 4. 等待 Unbound 启动完成（检查端口是否可用）
	if err := m.waitForReady(5 * time.Second); err != nil {
		return fmt.Errorf("unbound may not be ready: %v", err)
	}

	// 5. 启动进程监控 goroutine
	m.exitCh = make(chan error, 1)
	go func() {
		// 等待进程退出
		err := m.cmd.Wait()
		m.exitCh <- err
	}()

	// 6. 启动健康检查/保活 loop
	go m.healthCheckLoop()

	return nil
}

// Stop 停止 Unbound 进程
// 清理主程序目录下 unbound/ 子目录中的文件
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

	// 3. 清理配置文件
	// 注意：只清理配置文件，不清理 unbound 二进制文件
	// 在 Linux 上，unbound 是系统包，不应该被删除
	// 在 Windows 上，unbound 是嵌入式的，但也不应该在这里删除
	if m.configPath != "" {
		_ = os.Remove(m.configPath)
	}

	m.enabled = false
	m.startTime = time.Time{} // 重置启动时间
	logger.Infof("[Recursor] Unbound process stopped")
	return nil
}

// generateConfig 生成 Unbound 配置文件
// 根据运行时的机器情况动态调整参数
func (m *Manager) generateConfig() (string, error) {
	// 如果已有 ConfigGenerator，使用它
	if m.configGen != nil {
		config, err := m.configGen.GenerateConfig()
		if err != nil {
			return "", err
		}

		// 确定配置文件路径
		var configPath string
		if runtime.GOOS == "linux" {
			configPath = "/etc/unbound/unbound.conf.d/smartdnssort.conf"
		} else {
			configDir, _ := GetUnboundConfigDir()
			configPath = filepath.Join(configDir, "unbound.conf")
		}

		// 写入配置文件
		if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
			return "", fmt.Errorf("failed to write config file: %w", err)
		}

		return configPath, nil
	}

	// 回退到原有的配置生成逻辑（用于兼容性）
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

// GetStartTime 获取进程启动时间
func (m *Manager) GetStartTime() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.startTime
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

// Initialize 初始化（首次启用时调用）
func (m *Manager) Initialize() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. 检测系统
	if err := m.sysManager.DetectSystem(); err != nil {
		return fmt.Errorf("failed to detect system: %w", err)
	}

	logger.Infof("[Recursor] System detected: OS=%s, Distro=%s", m.sysManager.osType, m.sysManager.distro)

	// 2. 检查 unbound 是否已安装
	if !m.sysManager.IsUnboundInstalled() {
		logger.Infof("[Recursor] Unbound not installed, installing...")
		// 3. 安装 unbound
		if err := m.sysManager.InstallUnbound(); err != nil {
			return fmt.Errorf("failed to install unbound: %w", err)
		}
		logger.Infof("[Recursor] Unbound installed successfully")
	} else {
		logger.Infof("[Recursor] Unbound already installed")
		// 4. 处理已存在的 unbound
		if err := m.sysManager.handleExistingUnbound(); err != nil {
			logger.Warnf("[Recursor] Failed to handle existing unbound: %v", err)
		}
	}

	// 5. 获取版本信息
	version, err := m.sysManager.GetUnboundVersion()
	if err != nil {
		return fmt.Errorf("failed to get unbound version: %w", err)
	}

	logger.Infof("[Recursor] Unbound version: %s", version)

	// 6. 获取 unbound 路径
	path, err := m.sysManager.getUnboundPath()
	if err != nil {
		return fmt.Errorf("failed to get unbound path: %w", err)
	}

	m.sysManager.unboundPath = path
	m.sysManager.unboundVer = version
	logger.Infof("[Recursor] Unbound path: %s", path)

	// 7. 创建配置生成器
	sysInfo := m.sysManager.GetSystemInfo()
	m.configGen = NewConfigGenerator(version, sysInfo, m.port)

	// 8. 验证配置
	if err := m.configGen.ValidateConfig(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// 9. 确定是否为系统级
	m.isSystemLevel = runtime.GOOS == "linux"

	logger.Infof("[Recursor] Initialization complete: OS=%s, Version=%s, SystemLevel=%v",
		sysInfo.OS, version, m.isSystemLevel)

	return nil
}

// Cleanup 清理（卸载时调用）
func (m *Manager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. 停止 unbound
	if m.enabled {
		m.mu.Unlock()
		err := m.Stop()
		m.mu.Lock()
		if err != nil {
			logger.Warnf("[Recursor] Failed to stop unbound: %v", err)
		}
	}

	// 2. 删除配置文件
	if m.configPath != "" {
		_ = os.Remove(m.configPath)
	}

	// 3. Linux: 卸载 unbound
	if runtime.GOOS == "linux" && m.sysManager != nil {
		if err := m.sysManager.UninstallUnbound(); err != nil {
			logger.Warnf("[Recursor] Failed to uninstall unbound: %v", err)
		}
	}

	logger.Infof("[Recursor] Cleanup complete")
	return nil
}

// GetSystemInfo 获取系统信息
func (m *Manager) GetSystemInfo() SystemInfo {
	if m.sysManager == nil {
		return SystemInfo{}
	}
	return m.sysManager.GetSystemInfo()
}

// GetUnboundVersion 获取 unbound 版本
func (m *Manager) GetUnboundVersion() string {
	if m.sysManager == nil {
		return ""
	}
	return m.sysManager.unboundVer
}

// GetInstallState 获取安装状态
func (m *Manager) GetInstallState() InstallState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.installState
}

// SetInstallState 设置安装状态
func (m *Manager) SetInstallState(state InstallState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installState = state
}
