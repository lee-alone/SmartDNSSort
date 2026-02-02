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
	"strings"
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

// 常量定义 - 重启和超时配置
const (
	MaxRestartAttempts      = 5
	MaxBackoffDuration      = 30 * time.Second
	HealthCheckInterval     = 30 * time.Second
	ProcessStopTimeout      = 5 * time.Second
	WaitReadyTimeoutWindows = 30 * time.Second
	WaitReadyTimeoutLinux   = 20 * time.Second
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

	// 重启管理
	restartAttempts int       // 当前重启尝试次数
	lastRestartTime time.Time // 最后一次重启时间

	// 进程管理 - 平台特定
	jobObject interface{} // Windows Job Object 句柄

	// Goroutine 生命周期管理
	monitorCtx    context.Context
	monitorCancel context.CancelFunc
	healthCtx     context.Context
	healthCancel  context.CancelFunc
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
// 流程：
// 1. 首次启用时执行初始化（仅 Linux）
// 2. 启动 Unbound 进程
// 3. 等待进程启动完成（检查端口可达性）
// 4. 启动进程监控 goroutine
// 5. 启动健康检查循环
// 使用 context 管理 goroutine 生命周期，防止泄漏
// 创建新的 stopCh 支持多次启停
func (m *Manager) Start() error {
	m.mu.Lock()

	if m.enabled {
		m.mu.Unlock()
		return fmt.Errorf("recursor already running")
	}

	// 首次启用时执行初始化（仅 Linux）
	if m.installState == StateNotInstalled && runtime.GOOS == "linux" {
		m.installState = StateInstalling
		// 立即标记为启用，防止其他goroutine在Initialize期间调用Start
		m.enabled = true
		m.mu.Unlock()

		if err := m.Initialize(); err != nil {
			m.mu.Lock()
			m.enabled = false
			m.installState = StateError
			m.mu.Unlock()
			return err
		}

		m.mu.Lock()
		m.installState = StateInstalled
		m.mu.Unlock()
	} else {
		m.mu.Unlock()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 取消旧的监控 goroutine（如果存在）
	if m.monitorCancel != nil {
		m.monitorCancel()
	}
	if m.healthCancel != nil {
		m.healthCancel()
	}

	// 创建新的 context 用于管理 goroutine 生命周期
	m.monitorCtx, m.monitorCancel = context.WithCancel(context.Background())
	m.healthCtx, m.healthCancel = context.WithCancel(context.Background())

	// 创建新的 stopCh（旧的已关闭）
	m.stopCh = make(chan struct{})

	// 调用平台特定的启动逻辑（不再调用 Initialize，已在上面处理）
	if err := m.startPlatformSpecificNoInit(); err != nil {
		return err
	}

	// 3. 启动 Unbound 进程
	// -d: 前台运行（便于日志和进程管理）
	// -c: 指定配置文件
	m.cmd = exec.Command(m.unboundPath, "-c", m.configPath, "-d")

	// 设置输出（可选，用于调试）
	m.cmd.Stdout = os.Stdout
	m.cmd.Stderr = os.Stderr

	// 平台特定的进程管理配置
	m.configureProcessManagement()

	logger.Infof("[Recursor] Starting unbound: %s -c %s -d", m.unboundPath, m.configPath)

	if err := m.cmd.Start(); err != nil {
		// 提供更详细的错误信息
		logger.Errorf("[Recursor] Failed to start unbound process: %v", err)
		logger.Errorf("[Recursor] Unbound path: %s (exists: %v)", m.unboundPath, fileExists(m.unboundPath))
		logger.Errorf("[Recursor] Config path: %s (exists: %v)", m.configPath, fileExists(m.configPath))
		return fmt.Errorf("failed to start unbound process: %w", err)
	}

	// 启动后的平台特定处理（如 Windows Job Object 分配）
	m.postStartProcessManagement()

	m.enabled = true
	m.startTime = time.Now()
	m.lastHealthCheck = time.Now()
	logger.Infof("[Recursor] Unbound process started (PID: %d)", m.cmd.Process.Pid)

	// 4. 等待 Unbound 启动完成（检查端口是否可用）
	// 获取平台特定的启动超时
	waitTimeout := m.getWaitForReadyTimeout()

	if err := m.waitForReady(waitTimeout); err != nil {
		logger.Errorf("[Recursor] Unbound failed to be ready: %v", err)
		// 强制关闭进程
		if m.cmd != nil && m.cmd.Process != nil {
			_ = m.cmd.Process.Kill()
		}
		m.enabled = false
		return fmt.Errorf("unbound startup timeout: %w", err)
	}

	logger.Infof("[Recursor] Unbound is ready and listening on port %d", m.port)

	// 5. 启动进程监控 goroutine
	m.exitCh = make(chan error, 1)
	go func() {
		// 等待进程退出
		err := m.cmd.Wait()
		select {
		case m.exitCh <- err:
		case <-m.monitorCtx.Done():
			// Context 已取消，不发送错误
		}
	}()

	// 6. 启动健康检查/保活 loop
	go m.healthCheckLoop()

	// 7. 启动 root.key 定期更新任务（仅 Linux）
	if runtime.GOOS == "linux" && m.sysManager != nil {
		go m.updateRootKeyInBackground()
	}

	return nil
}

// Stop 停止 Unbound 进程
// 流程：
// 1. 标记为禁用，防止 healthCheckLoop 尝试重启
// 2. 取消所有 goroutine（通过 context）
// 3. 优雅停止进程（SIGTERM，超时后 SIGKILL）
// 4. 清理配置文件
// 5. 清理平台特定的进程管理资源
// 支持多次启停，每次 Stop 后可以再次 Start
func (m *Manager) Stop() error {
	m.mu.Lock()

	if !m.enabled {
		m.mu.Unlock()
		return nil
	}

	// 标记为禁用，防止 healthCheckLoop 尝试重启
	m.enabled = false

	// 取消所有 goroutine
	if m.monitorCancel != nil {
		m.monitorCancel()
	}
	if m.healthCancel != nil {
		m.healthCancel()
	}

	// 保存旧的 stopCh 用于关闭
	oldStopCh := m.stopCh
	m.mu.Unlock()

	// 1. 停止健康检查（关闭旧的 stopCh）
	close(oldStopCh)

	// 2. 优雅停止进程
	if m.cmd != nil && m.cmd.Process != nil {
		// 发送 SIGTERM 信号
		if err := m.cmd.Process.Signal(os.Interrupt); err != nil {
			// 如果发送信号失败，可能是进程已经不存在了
			// 继续尝试清理
		}

		// 等待进程退出（最多 5 秒）
		select {
		case <-m.exitCh:
			// 进程已退出
		case <-time.After(ProcessStopTimeout):
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
		if err := os.Remove(m.configPath); err != nil && !os.IsNotExist(err) {
			logger.Warnf("[Recursor] Failed to remove config file: %v", err)
		}
	}

	// 4. 清理平台特定的进程管理资源
	m.cleanupProcessManagement()

	m.mu.Lock()
	m.restartAttempts = 0     // 重置重启计数器
	m.startTime = time.Time{} // 重置启动时间
	m.mu.Unlock()

	logger.Infof("[Recursor] Unbound process stopped")
	return nil
}

// generateConfig 生成 Unbound 配置文件
// 根据运行时的机器情况动态调整参数：
// - 线程数：基于 CPU 核数（最多 8 个）
// - 缓存大小：基于线程数动态计算
// - 路径处理：Windows 使用正斜杠，Linux 使用标准路径
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
			// 在 Windows 上，使用绝对路径
			absPath, _ := filepath.Abs(configPath)
			configPath = absPath
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
	// 在 Windows 上，使用绝对路径
	if runtime.GOOS == "windows" {
		absPath, _ := filepath.Abs(configPath)
		configPath = absPath
	}

	// 动态计算线程数（基于 CPU 核数）
	// min(CPU, 8) 且至少为 1
	numThreads := max(1, min(runtime.NumCPU(), 8))

	// 根据线程数调整缓存大小 - 递归优化模式
	// 小型缓存，因为上层应用已有完整的缓存层
	msgCacheSize := 10 + (2 * numThreads)   // 10-26MB（原 50-250MB）
	rrsetCacheSize := 20 + (4 * numThreads) // 20-52MB（原 100-500MB）

	// 获取 root.key 路径
	rootKeyPath := filepath.Join(configDir, "root.key")
	// 在 Windows 上，unbound 配置文件中的路径需要使用正斜杠或转义反斜杠
	if runtime.GOOS == "windows" {
		rootKeyPath = strings.ReplaceAll(rootKeyPath, "\\", "/")
	}

	// 生成配置内容
	config := fmt.Sprintf(`# SmartDNSSort Embedded Unbound Configuration (Fallback)
# Auto-generated, do not edit manually
# Generated for %d CPU cores
# 
# 配置原则：Unbound 作为递归解析器，不重复缓存
# 上层 SmartDNSSort 应用已有完整的缓存层

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
    so-rcvbuf: 1m
    
    # 缓存策略 - 快速刷新，不重复缓存
    cache-max-ttl: 300
    cache-min-ttl: 0
    cache-max-negative-ttl: 60
    serve-expired: no
    serve-expired-ttl: 300
    serve-expired-reply-ttl: 30
    
    # 预取优化 - 禁用，因为上层已处理
    prefetch: no
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
// 通过检查 TCP 端口是否可达来判断服务是否就绪
// 使用指数退避策略，每次尝试间隔 50ms
func (m *Manager) waitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	attempts := 0
	lastLogTime := time.Now()

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for unbound to be ready after %d attempts", attempts)
		}

		// 尝试连接到 Unbound TCP 端口
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", m.port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			logger.Infof("[Recursor] Unbound is ready on port %d (after %d attempts, %.1fs)", m.port, attempts, time.Since(deadline.Add(-timeout)).Seconds())
			return nil
		}

		attempts++
		// 每 5 次尝试输出一次日志
		if time.Since(lastLogTime) > 500*time.Millisecond {
			logger.Debugf("[Recursor] Waiting for unbound to be ready... (attempt %d, elapsed: %.1fs)", attempts, time.Since(deadline.Add(-timeout)).Seconds())
			lastLogTime = time.Now()
		}

		// 更频繁地检查（50ms 而不是 100ms）
		time.Sleep(50 * time.Millisecond)
	}
}

// healthCheckLoop 监控进程状态并执行健康检查
// 注意：此方法已移至 manager_lifecycle.go

// performHealthCheck 执行一次健康检查
// 注意：此方法已移至 manager_lifecycle.go

// fileExists 检查文件是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// getWorkingDir 获取当前工作目录
func getWorkingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return wd
}

// getWaitForReadyTimeout 获取平台特定的启动超时
func (m *Manager) getWaitForReadyTimeout() time.Duration {
	if runtime.GOOS == "windows" {
		return m.waitForReadyTimeoutWindows()
	}
	return m.waitForReadyTimeoutLinux()
}

// updateRootKeyInBackground 后台定期更新 root.key（仅 Linux）
// 注意：此方法已移至 manager_lifecycle.go
