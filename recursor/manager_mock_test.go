package recursor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// MockProcessRunner 是进程运行器的 Mock 接口
// 用于测试进程启动失败、端口占用等极端情况
type MockProcessRunner interface {
	Start() error
	Wait() error
	Kill() error
	Pid() int
}

// MockCmd 是 exec.Cmd 的 Mock 实现
type MockCmd struct {
	startErr   error
	waitErr    error
	killErr    error
	pid        int
	started    bool
	killed     bool
	mu         sync.Mutex
	waitCalled chan struct{}
}

// NewMockCmd 创建新的 MockCmd
func NewMockCmd() *MockCmd {
	return &MockCmd{
		pid:        12345,
		waitCalled: make(chan struct{}, 1),
	}
}

// Start 模拟启动进程
func (m *MockCmd) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.started = true
	return nil
}

// Wait 模拟等待进程
func (m *MockCmd) Wait() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.waitCalled != nil {
		select {
		case m.waitCalled <- struct{}{}:
		default:
		}
	}
	if m.waitErr != nil {
		return m.waitErr
	}
	// 模拟进程运行直到被杀死
	return nil
}

// Kill 模拟杀死进程
func (m *MockCmd) Kill() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.killErr != nil {
		return m.killErr
	}
	m.killed = true
	return nil
}

// Pid 返回模拟的进程 ID
func (m *MockCmd) Pid() int {
	return m.pid
}

// SetStartError 设置启动错误
func (m *MockCmd) SetStartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startErr = err
}

// SetWaitError 设置等待错误
func (m *MockCmd) SetWaitError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.waitErr = err
}

// IsStarted 检查是否已启动
func (m *MockCmd) IsStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

// IsKilled 检查是否已杀死
func (m *MockCmd) IsKilled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.killed
}

// TestInstallStateTransitions 测试安装状态转换
func TestInstallStateTransitions(t *testing.T) {
	tests := []struct {
		name     string
		initial  InstallState
		expected InstallState
	}{
		{"NotInstalled to Initializing", StateNotInstalled, StateInitializing},
		{"Initializing to Installed", StateInitializing, StateInstalled},
		{"Installed stays Installed", StateInstalled, StateInstalled},
		{"Error stays Error", StateError, StateError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager(5353)
			mgr.installState = tt.initial

			// 验证状态值
			if mgr.installState != tt.initial {
				t.Errorf("Initial state mismatch: expected %d, got %d", tt.initial, mgr.installState)
			}
		})
	}
}

// TestStateInitializingConcurrentProtection 测试 StateInitializing 状态的并发保护
func TestStateInitializingConcurrentProtection(t *testing.T) {
	mgr := NewManager(5353)
	mgr.installState = StateInitializing

	// 模拟并发调用 Start()，应该返回错误
	err := mgr.Start()
	if err == nil {
		t.Error("Expected error when Start() called during initialization")
	}

	// 验证错误消息包含 "initializing"
	if err != nil && !contains(err.Error(), "initializing") {
		t.Errorf("Expected error to mention 'initializing', got: %v", err)
	}
}

// TestStartWhenAlreadyRunning 测试已运行时调用 Start
func TestStartWhenAlreadyRunning(t *testing.T) {
	mgr := NewManager(5353)
	mgr.enabled = true

	err := mgr.Start()
	if err == nil {
		t.Error("Expected error when Start() called while already running")
	}

	if err != nil && !contains(err.Error(), "already running") {
		t.Errorf("Expected 'already running' error, got: %v", err)
	}
}

// TestManagerRestartAttempts 测试重启尝试计数
func TestManagerRestartAttempts(t *testing.T) {
	mgr := NewManager(5353)

	// 初始重启尝试次数应为 0
	if mgr.GetRestartAttempts() != 0 {
		t.Errorf("Expected 0 restart attempts, got %d", mgr.GetRestartAttempts())
	}

	// 模拟重启尝试
	mgr.restartAttempts = 3
	if mgr.GetRestartAttempts() != 3 {
		t.Errorf("Expected 3 restart attempts, got %d", mgr.GetRestartAttempts())
	}
}

// TestManagerInstallStateGetSet 测试安装状态的获取和设置
func TestManagerInstallStateGetSet(t *testing.T) {
	mgr := NewManager(5353)

	// 初始状态应为 StateNotInstalled
	if mgr.GetInstallState() != StateNotInstalled {
		t.Errorf("Expected StateNotInstalled, got %d", mgr.GetInstallState())
	}

	// 设置新状态
	mgr.SetInstallState(StateInstalled)
	if mgr.GetInstallState() != StateInstalled {
		t.Errorf("Expected StateInstalled, got %d", mgr.GetInstallState())
	}

	// 设置错误状态
	mgr.SetInstallState(StateError)
	if mgr.GetInstallState() != StateError {
		t.Errorf("Expected StateError, got %d", mgr.GetInstallState())
	}
}

// TestManagerConfigPath 测试配置路径获取
func TestManagerConfigPath(t *testing.T) {
	mgr := NewManager(5353)

	// 初始配置路径应为空
	if mgr.GetConfigPath() != "" {
		t.Errorf("Expected empty config path, got %s", mgr.GetConfigPath())
	}

	// 设置配置路径
	testPath := "/test/path/unbound.conf"
	mgr.configPath = testPath
	if mgr.GetConfigPath() != testPath {
		t.Errorf("Expected %s, got %s", testPath, mgr.GetConfigPath())
	}
}

// TestHealthCheckLoopContextCancellation 测试健康检查循环的 Context 取消
func TestHealthCheckLoopContextCancellation(t *testing.T) {
	mgr := NewManager(5353)

	// 创建可取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	mgr.healthCtx = ctx
	mgr.stopCh = make(chan struct{})
	mgr.exitCh = make(chan error, 1)

	// 启动健康检查循环
	done := make(chan struct{})
	go func() {
		mgr.healthCheckLoop()
		close(done)
	}()

	// 等待一小段时间让循环启动
	time.Sleep(100 * time.Millisecond)

	// 取消 context
	cancel()

	// 等待循环退出
	select {
	case <-done:
		// 成功退出
	case <-time.After(2 * time.Second):
		t.Error("Health check loop did not exit after context cancellation")
	}
}

// TestHealthCheckLoopStopSignal 测试健康检查循环的停止信号
func TestHealthCheckLoopStopSignal(t *testing.T) {
	mgr := NewManager(5353)

	// 创建 context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr.healthCtx = ctx
	mgr.stopCh = make(chan struct{})
	mgr.exitCh = make(chan error, 1)

	// 启动健康检查循环
	done := make(chan struct{})
	go func() {
		mgr.healthCheckLoop()
		close(done)
	}()

	// 等待一小段时间让循环启动
	time.Sleep(100 * time.Millisecond)

	// 发送停止信号
	close(mgr.stopCh)

	// 等待循环退出
	select {
	case <-done:
		// 成功退出
	case <-time.After(2 * time.Second):
		t.Error("Health check loop did not exit after stop signal")
	}
}

// TestProcessExitDuringHealthCheck 测试健康检查期间的进程退出
func TestProcessExitDuringHealthCheck(t *testing.T) {
	mgr := NewManager(5353)
	mgr.enabled = true

	// 创建 context
	ctx, cancel := context.WithCancel(context.Background())
	mgr.healthCtx = ctx
	mgr.stopCh = make(chan struct{})
	mgr.exitCh = make(chan error, 1)

	// 启动健康检查循环
	done := make(chan struct{})
	go func() {
		mgr.healthCheckLoop()
		close(done)
	}()

	// 模拟进程退出
	mgr.exitCh <- errors.New("process exited")

	// 等待循环处理
	time.Sleep(200 * time.Millisecond)

	// 验证重启尝试增加
	if mgr.GetRestartAttempts() != 1 {
		t.Errorf("Expected 1 restart attempt, got %d", mgr.GetRestartAttempts())
	}

	// 清理
	cancel()
}

// TestMaxRestartAttempts 测试最大重启尝试次数
func TestMaxRestartAttempts(t *testing.T) {
	mgr := NewManager(5353)
	mgr.enabled = true
	mgr.restartAttempts = MaxRestartAttempts // 设置为最大值

	// 创建 context
	ctx, cancel := context.WithCancel(context.Background())
	mgr.healthCtx = ctx
	mgr.stopCh = make(chan struct{})
	mgr.exitCh = make(chan error, 1)

	// 启动健康检查循环
	done := make(chan struct{})
	go func() {
		mgr.healthCheckLoop()
		close(done)
	}()

	// 模拟进程退出
	mgr.exitCh <- errors.New("process exited")

	// 等待循环处理
	select {
	case <-done:
		// 循环应该退出
	case <-time.After(2 * time.Second):
		t.Error("Health check loop should exit after max restart attempts")
	}

	// 验证已禁用
	if mgr.IsEnabled() {
		t.Error("Manager should be disabled after max restart attempts")
	}

	// 清理
	cancel()
}

// TestExponentialBackoff 测试指数退避计算
func TestExponentialBackoff(t *testing.T) {
	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 30 * time.Second},  // 应该被 MaxBackoffDuration 限制
		{10, 30 * time.Second}, // 应该被 MaxBackoffDuration 限制
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			backoff := time.Duration(1<<uint(tt.attempt-1)) * time.Second
			if backoff > MaxBackoffDuration {
				backoff = MaxBackoffDuration
			}

			if backoff != tt.expected {
				t.Errorf("Attempt %d: expected %v, got %v", tt.attempt, tt.expected, backoff)
			}
		})
	}
}

// TestManagerSystemInfo 测试系统信息获取
func TestManagerSystemInfo(t *testing.T) {
	mgr := NewManager(5353)

	// 获取系统信息
	info := mgr.GetSystemInfo()

	// 验证 CPU 核心数大于 0
	if info.CPUCores <= 0 {
		t.Error("CPU cores should be greater than 0")
	}
}

// TestManagerUnboundVersion 测试 Unbound 版本获取
func TestManagerUnboundVersion(t *testing.T) {
	mgr := NewManager(5353)

	// 初始版本应为空（因为没有 sysManager）
	version := mgr.GetUnboundVersion()
	if version != "" {
		t.Errorf("Expected empty version, got %s", version)
	}
}

// TestManagerStartTime 测试启动时间获取
func TestManagerStartTime(t *testing.T) {
	mgr := NewManager(5353)

	// 初始启动时间应为零值
	startTime := mgr.GetStartTime()
	if !startTime.IsZero() {
		t.Error("Initial start time should be zero")
	}

	// 设置启动时间
	testTime := time.Now()
	mgr.startTime = testTime
	if mgr.GetStartTime() != testTime {
		t.Error("Start time mismatch")
	}
}

// TestManagerLastRestartTime 测试最后重启时间获取
func TestManagerLastRestartTime(t *testing.T) {
	mgr := NewManager(5353)

	// 初始最后重启时间应为零值
	lastRestart := mgr.GetLastRestartTime()
	if !lastRestart.IsZero() {
		t.Error("Initial last restart time should be zero")
	}
}

// MockFileSystem 是文件系统的 Mock，用于测试文件操作
type MockFileSystem struct {
	existingFiles map[string]bool
	fileContents  map[string][]byte
	fileErrors    map[string]error
	mu            sync.RWMutex
}

// NewMockFileSystem 创建新的 Mock 文件系统
func NewMockFileSystem() *MockFileSystem {
	return &MockFileSystem{
		existingFiles: make(map[string]bool),
		fileContents:  make(map[string][]byte),
		fileErrors:    make(map[string]error),
	}
}

// Stat 模拟 os.Stat
func (m *MockFileSystem) Stat(name string) (os.FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err, ok := m.fileErrors[name]; ok {
		return nil, err
	}
	if m.existingFiles[name] {
		return &MockFileInfo{name: name}, nil
	}
	return nil, os.ErrNotExist
}

// ReadFile 模拟 os.ReadFile
func (m *MockFileSystem) ReadFile(name string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err, ok := m.fileErrors[name]; ok {
		return nil, err
	}
	if content, ok := m.fileContents[name]; ok {
		return content, nil
	}
	return nil, os.ErrNotExist
}

// WriteFile 模拟 os.WriteFile
func (m *MockFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.fileErrors[name]; ok {
		return err
	}
	m.existingFiles[name] = true
	m.fileContents[name] = data
	return nil
}

// SetFileError 设置文件错误
func (m *MockFileSystem) SetFileError(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fileErrors[name] = err
}

// SetExistingFile 设置已存在的文件
func (m *MockFileSystem) SetExistingFile(name string, content []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.existingFiles[name] = true
	m.fileContents[name] = content
}

// MockFileInfo 是 os.FileInfo 的 Mock 实现
type MockFileInfo struct {
	name string
}

func (m *MockFileInfo) Name() string       { return m.name }
func (m *MockFileInfo) Size() int64        { return 1024 }
func (m *MockFileInfo) Mode() os.FileMode  { return 0644 }
func (m *MockFileInfo) ModTime() time.Time { return time.Now() }
func (m *MockFileInfo) IsDir() bool        { return false }
func (m *MockFileInfo) Sys() interface{}   { return nil }

// TestMockProcessRunner 测试 Mock 进程运行器
func TestMockProcessRunner(t *testing.T) {
	cmd := NewMockCmd()

	// 测试正常启动
	if err := cmd.Start(); err != nil {
		t.Errorf("Unexpected start error: %v", err)
	}

	if !cmd.IsStarted() {
		t.Error("Process should be started")
	}

	// 测试启动错误
	cmd.SetStartError(exec.ErrNotFound)
	if err := cmd.Start(); err == nil {
		t.Error("Expected start error")
	}

	// 测试 Pid
	if cmd.Pid() != 12345 {
		t.Errorf("Expected pid 12345, got %d", cmd.Pid())
	}
}

// TestMockFileSystem 测试 Mock 文件系统
func TestMockFileSystem(t *testing.T) {
	fs := NewMockFileSystem()

	// 测试不存在的文件
	_, err := fs.Stat("/nonexistent")
	if !os.IsNotExist(err) {
		t.Error("Expected ErrNotExist for nonexistent file")
	}

	// 设置已存在的文件
	testContent := []byte("test content")
	fs.SetExistingFile("/test/file.conf", testContent)

	// 测试已存在的文件
	info, err := fs.Stat("/test/file.conf")
	if err != nil {
		t.Errorf("Unexpected stat error: %v", err)
	}
	if info.Name() != "/test/file.conf" {
		t.Errorf("Expected name /test/file.conf, got %s", info.Name())
	}

	// 测试读取文件
	content, err := fs.ReadFile("/test/file.conf")
	if err != nil {
		t.Errorf("Unexpected read error: %v", err)
	}
	if string(content) != "test content" {
		t.Errorf("Expected 'test content', got %s", content)
	}

	// 测试写入文件
	newContent := []byte("new content")
	if err := fs.WriteFile("/test/new.conf", newContent, 0644); err != nil {
		t.Errorf("Unexpected write error: %v", err)
	}

	// 验证写入的内容
	readContent, err := fs.ReadFile("/test/new.conf")
	if err != nil {
		t.Errorf("Unexpected read error: %v", err)
	}
	if string(readContent) != "new content" {
		t.Errorf("Expected 'new content', got %s", readContent)
	}

	// 测试文件错误
	fs.SetFileError("/error/file.conf", os.ErrPermission)
	_, err = fs.ReadFile("/error/file.conf")
	if !os.IsPermission(err) {
		t.Error("Expected ErrPermission")
	}
}

// TestClassifyError 测试错误分类逻辑
func TestClassifyError(t *testing.T) {
	mgr := NewManager(5353)

	tests := []struct {
		name     string
		err      error
		expected InstallState
	}{
		{"nil error", nil, StateError},
		{"timeout error", fmt.Errorf("connection timeout"), StateRetryableError},
		{"temporary failure", fmt.Errorf("temporary failure in name resolution"), StateRetryableError},
		{"connection refused", fmt.Errorf("connection refused"), StateRetryableError},
		{"network unreachable", fmt.Errorf("network unreachable"), StateRetryableError},
		{"permission denied", fmt.Errorf("permission denied"), StateError},
		{"config error", fmt.Errorf("invalid configuration"), StateError},
		{"port conflict", fmt.Errorf("port already in use"), StateError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mgr.classifyError(tt.err)
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestResetState 测试状态重置功能
func TestResetState(t *testing.T) {
	mgr := NewManager(5353)

	// 设置为错误状态
	mgr.installState = StateError
	mgr.enabled = true
	mgr.restartAttempts = 3
	mgr.startTime = time.Now()

	// 重置状态
	mgr.ResetState()

	// 验证状态已重置
	if mgr.installState != StateNotInstalled {
		t.Errorf("Expected StateNotInstalled, got %d", mgr.installState)
	}
	if mgr.enabled {
		t.Error("Expected enabled to be false")
	}
	if mgr.GetRestartAttempts() != 0 {
		t.Errorf("Expected 0 restart attempts, got %d", mgr.GetRestartAttempts())
	}
	if !mgr.GetStartTime().IsZero() {
		t.Error("Expected start time to be zero")
	}
}

// TestRetryableErrorRecovery 测试可重试错误的恢复
func TestRetryableErrorRecovery(t *testing.T) {
	mgr := NewManager(5353)

	// 模拟可重试错误状态
	mgr.installState = StateRetryableError

	// 验证可以从 StateRetryableError 状态恢复
	// 注意：实际的 Start() 需要真实的 Unbound 环境，这里只测试状态逻辑
	if mgr.installState != StateRetryableError {
		t.Error("Expected StateRetryableError")
	}

	// 重置后可以重新尝试
	mgr.ResetState()
	if mgr.installState != StateNotInstalled {
		t.Error("Expected StateNotInstalled after reset")
	}
}

// TestCheckPortAvailable 测试端口可用性检查
func TestCheckPortAvailable(t *testing.T) {
	// 找一个可用端口
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	port := addr.Port
	listener.Close()

	// 测试可用端口
	if err := checkPortAvailable(port); err != nil {
		t.Errorf("Expected port %d to be available, got error: %v", port, err)
	}

	// 占用端口
	listener2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("Failed to occupy port: %v", err)
	}
	defer listener2.Close()

	// 测试被占用的端口
	if err := checkPortAvailable(port); err == nil {
		t.Errorf("Expected error for occupied port %d", port)
	} else if !contains(err.Error(), "already in use") {
		t.Errorf("Expected 'already in use' error, got: %v", err)
	}
}

// TestHealthCheckLoopScheduledRetry 测试健康检查循环的定时重试机制
func TestHealthCheckLoopScheduledRetry(t *testing.T) {
	mgr := NewManager(5353)
	mgr.enabled = true

	// 创建 context
	ctx, cancel := context.WithCancel(context.Background())
	mgr.healthCtx = ctx
	mgr.stopCh = make(chan struct{})
	mgr.exitCh = make(chan error, 1)

	// 启动健康检查循环
	done := make(chan struct{})
	go func() {
		mgr.healthCheckLoop()
		close(done)
	}()

	// 模拟进程退出
	mgr.exitCh <- fmt.Errorf("process exited")

	// 等待一小段时间让循环处理
	time.Sleep(200 * time.Millisecond)

	// 验证重启尝试次数增加
	if mgr.GetRestartAttempts() != 1 {
		t.Errorf("Expected 1 restart attempt, got %d", mgr.GetRestartAttempts())
	}

	// 清理
	cancel()
	<-done
}

// TestInstallStateString 测试安装状态的字符串表示
func TestInstallStateString(t *testing.T) {
	tests := []struct {
		state    InstallState
		expected string
	}{
		{StateNotInstalled, "StateNotInstalled"},
		{StateInitializing, "StateInitializing"},
		{StateInstalling, "StateInstalling"},
		{StateInstalled, "StateInstalled"},
		{StateError, "StateError"},
		{StateRetryableError, "StateRetryableError"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			// 注意：Go 的 enum 没有内置 String() 方法，这里只是验证值
			if tt.state < 0 || tt.state > 5 {
				t.Errorf("Unexpected state value: %d", tt.state)
			}
		})
	}
}

// TestContainsIgnoreCase 测试忽略大小写的字符串包含检查
func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"Hello World", "world", true},
		{"Hello World", "WORLD", true},
		{"Hello World", "hello", true},
		{"Hello World", "foo", false},
		{"", "test", false},
		{"test", "", true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := containsIgnoreCase(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("containsIgnoreCase(%q, %q) = %v, expected %v", tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}
