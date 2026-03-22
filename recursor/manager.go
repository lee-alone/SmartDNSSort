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
	StateInitializing              // 正在初始化（新增状态，用于解决竞态条件）
	StateInstalling
	StateInstalled
	StateError
	StateRetryableError // 可重试的错误状态（允许重新尝试初始化）
)

// 常量定义 - 重启和超时配置
const (
	MaxRestartAttempts      = 5
	MaxBackoffDuration      = 30 * time.Second
	HealthCheckInterval     = 30 * time.Second
	ProcessStopTimeout      = 2 * time.Second
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

	// root.zone 管理
	rootZoneMgr    *RootZoneManager
	rootZoneStopCh chan struct{}
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

	// 检查是否正在初始化（防止并发调用 Start）
	if m.installState == StateInitializing {
		m.mu.Unlock()
		return fmt.Errorf("recursor is initializing, please wait")
	}

	// 首次启用时执行初始化（仅 Linux）
	// 允许在 StateNotInstalled 或 StateRetryableError 时重新尝试初始化
	if (m.installState == StateNotInstalled || m.installState == StateRetryableError) && runtime.GOOS == "linux" {
		// 使用 StateInitializing 状态，避免在初始化期间设置 enabled = true
		// 这样其他 goroutine 可以通过检查 installState 来判断是否正在初始化
		m.installState = StateInitializing
		m.mu.Unlock()

		if err := m.Initialize(); err != nil {
			m.mu.Lock()
			// 区分可重试和不可重试的错误
			m.installState = m.classifyError(err)
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

	// 预检查：验证端口是否可用（提前发现端口冲突）
	if err := checkPortAvailable(m.port); err != nil {
		return fmt.Errorf("port %d is not available: %w", m.port, err)
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

	// 7. 初始化并管理 root.zone
	// 注意：不再启动定期更新任务
	// Unbound 会通过 auth-zone 配置自动从根服务器同步 root.zone
	// 这样更高效，无需我们手动更新
	m.rootZoneMgr = NewRootZoneManager()
	logger.Infof("[Recursor] Ensuring root.zone file...")
	rootZonePath, isNew, err := m.rootZoneMgr.EnsureRootZone()
	if err != nil {
		logger.Warnf("[Recursor] Failed to ensure root.zone file: %v", err)
		// 非致命错误，继续启动
	} else {
		if isNew {
			logger.Infof("[Recursor] New root.zone file created: %s", rootZonePath)
		} else {
			logger.Infof("[Recursor] Using existing root.zone file: %s", rootZonePath)
		}
		logger.Infof("[Recursor] Unbound will automatically sync root.zone from root servers")
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

	// 停止 root.zone 定期更新任务（如果存在）
	// 注意：现在 unbound 会自动从根服务器同步 root.zone，
	// 所以这个任务通常不会被启动，但保留这段代码以保持兼容性
	if m.rootZoneStopCh != nil {
		close(m.rootZoneStopCh)
		m.rootZoneStopCh = nil
	}

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

	// 3. 不删除配置文件
	// 注意：配置文件应该被保留，以便用户可以编辑和重启
	// 配置文件只在用户明确删除或卸载时才应该被删除
	// 这样可以支持"保存配置 -> 重启"的工作流程

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
// 使用 ConfigGenerator 统一生成配置，确保参数一致性
//
// 智能生成策略：
// - 如果配置文件已存在，则跳过生成（允许用户编辑和保存）
// - 如果文件不存在，则生成默认配置
// - 首次启动时会生成配置，之后用户可以自由编辑
func (m *Manager) generateConfig() (string, error) {
	// 必须使用 ConfigGenerator 生成配置
	// 确保所有缓存参数计算逻辑统一
	if m.configGen == nil {
		return "", fmt.Errorf("config generator not initialized, call Initialize() first")
	}

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

	// 检查配置文件是否已存在
	if fileExists(configPath) {
		logger.Infof("[Recursor] Using existing config file: %s", configPath)
		return configPath, nil
	}

	// 写入配置文件
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return "", fmt.Errorf("failed to write config file: %w", err)
	}

	logger.Infof("[Recursor] Generated new config file: %s", configPath)
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

// checkPortAvailable 检查端口是否可用
// 通过尝试绑定端口来检测是否已被其他进程占用
// 返回错误如果端口已被占用
func checkPortAvailable(port int) error {
	// 尝试绑定端口
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		// 端口绑定失败，通常意味着已被占用
		if addrErr, ok := err.(*net.OpError); ok {
			if addrErr.Op == "listen" {
				return fmt.Errorf("port already in use by another process")
			}
		}
		return err
	}
	// 立即关闭，释放端口
	listener.Close()
	return nil
}

// classifyError 根据错误类型分类，判断是否为可重试错误
// 可重试错误：临时网络故障、资源暂时不可用
// 不可重试错误：配置错误、权限永久拒绝、端口冲突
func (m *Manager) classifyError(err error) InstallState {
	if err == nil {
		return StateError
	}

	errStr := err.Error()

	// 可重试的临时错误
	retryableErrors := []string{
		"timeout",
		"temporary failure",
		"connection refused",
		"network unreachable",
		"resource temporarily unavailable",
		"i/o timeout",
	}

	for _, pattern := range retryableErrors {
		if containsIgnoreCase(errStr, pattern) {
			return StateRetryableError
		}
	}

	// 默认为不可重试错误
	return StateError
}

// ResetState 重置管理器状态，允许在错误后重新尝试初始化
// 调用此方法后，可以再次调用 Start() 尝试启动
func (m *Manager) ResetState() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.installState = StateNotInstalled
	m.enabled = false
	m.restartAttempts = 0
	m.startTime = time.Time{}
	logger.Infof("[Recursor] Manager state reset, can attempt re-initialization")
}

// containsIgnoreCase 检查字符串是否包含子串（忽略大小写）
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
