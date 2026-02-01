//go:build linux

package recursor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"smartdnssort/logger"
	"syscall"
)

// startPlatformSpecific Linux 特定的启动逻辑（已弃用，使用 startPlatformSpecificNoInit）
func (m *Manager) startPlatformSpecific() error {
	// 此方法已弃用，保留以兼容性
	return m.startPlatformSpecificNoInit()
}

// startPlatformSpecificNoInit Linux 特定的启动逻辑（不调用 Initialize）
func (m *Manager) startPlatformSpecificNoInit() error {
	// 1. 获取 unbound 路径
	if m.sysManager != nil {
		m.unboundPath = m.sysManager.unboundPath
		logger.Infof("[Recursor] Using system unbound: %s", m.unboundPath)
	}

	// 2. 生成配置文件
	configPath, err := m.generateConfigLinux()
	if err != nil {
		return fmt.Errorf("failed to generate unbound config: %w", err)
	}
	m.configPath = configPath
	logger.Infof("[Recursor] Generated config file: %s", configPath)

	// 验证配置文件
	if !fileExists(configPath) {
		return fmt.Errorf("config file not found after generation: %s", configPath)
	}

	return nil
}

// generateConfigLinux Linux 特定的配置生成
func (m *Manager) generateConfigLinux() (string, error) {
	configPath := "/etc/unbound/unbound.conf.d/smartdnssort.conf"

	// 确保目录存在
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	// 动态计算线程数
	numThreads := max(1, min(runtime.NumCPU(), 8))
	msgCacheSize := 50 + (25 * numThreads)
	rrsetCacheSize := 100 + (50 * numThreads)

	// Linux 上使用标准路径
	rootKeyPath := "/etc/unbound/root.key"

	// 生成配置内容
	config := fmt.Sprintf(`# SmartDNSSort Embedded Unbound Configuration (Linux)
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
    
    # 性能优化
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
    
    # 模块配置
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

// configureUnixProcessManagement 配置 Linux 进程管理
// 使用进程组确保 Ctrl+C 时能正确关闭子进程
func (m *Manager) configureUnixProcessManagement() {
	// 在 Linux 上，使用 SysProcAttr 设置进程组
	// 这样可以通过发送信号给进程组来关闭所有子进程
	if m.cmd.SysProcAttr == nil {
		m.cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	// 设置进程组 ID，使其成为新进程组的领导者
	// 这样可以通过 syscall.Kill(-pid, signal) 向整个进程组发送信号
	m.cmd.SysProcAttr.Setsid = true
}

// cleanupUnixProcessManagement Linux 进程清理（无需特殊处理）
func (m *Manager) cleanupUnixProcessManagement() {
	// Linux 进程组会自动清理，无需特殊处理
}

// configureProcessManagement 配置 Linux 进程管理
func (m *Manager) configureProcessManagement() {
	m.configureUnixProcessManagement()
}

// cleanupProcessManagement 清理 Linux 进程管理
func (m *Manager) cleanupProcessManagement() {
	m.cleanupUnixProcessManagement()
}

// postStartProcessManagement Linux 启动后的处理（无需特殊处理）
func (m *Manager) postStartProcessManagement() {
	// Linux 进程组已在 configureProcessManagement 中配置
}
