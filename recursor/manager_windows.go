//go:build windows

package recursor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"smartdnssort/logger"
	"strings"
)

// startPlatformSpecific Windows 特定的启动逻辑
func (m *Manager) startPlatformSpecific() error {
	// 1. 解压 Unbound 二进制文件
	unboundPath, err := ExtractUnboundBinary()
	if err != nil {
		logger.Errorf("[Recursor] Failed to extract unbound binary: %v", err)
		logger.Errorf("[Recursor] Diagnostic info:")
		logger.Errorf("[Recursor]   - OS: windows")
		logger.Errorf("[Recursor]   - Arch: %s", runtime.GOARCH)
		logger.Errorf("[Recursor]   - Working directory: %s", getWorkingDir())
		return fmt.Errorf("failed to extract unbound binary: %w", err)
	}
	m.unboundPath = unboundPath
	logger.Infof("[Recursor] Extracted unbound binary to: %s", unboundPath)

	// 验证二进制文件
	fileInfo, err := os.Stat(unboundPath)
	if err != nil {
		return fmt.Errorf("unbound binary not found after extraction: %w", err)
	}
	logger.Infof("[Recursor] Unbound binary size: %d bytes", fileInfo.Size())

	// 2. 提取 root.key 文件
	if err := extractRootKey(); err != nil {
		return fmt.Errorf("failed to extract root.key: %w", err)
	}

	// 3. 生成配置文件
	configPath, err := m.generateConfigWindows()
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

// generateConfigWindows Windows 特定的配置生成
func (m *Manager) generateConfigWindows() (string, error) {
	configDir, err := GetUnboundConfigDir()
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(configDir, "unbound.conf")
	// 在 Windows 上，使用绝对路径
	absPath, _ := filepath.Abs(configPath)
	configPath = absPath

	// 动态计算线程数
	numThreads := max(1, min(runtime.NumCPU(), 8))
	msgCacheSize := 50 + (25 * numThreads)
	rrsetCacheSize := 100 + (50 * numThreads)

	// 获取 root.key 路径
	rootKeyPath := filepath.Join(configDir, "root.key")
	// 在 Windows 上，unbound 配置文件中的路径需要使用正斜杠
	rootKeyPath = strings.ReplaceAll(rootKeyPath, "\\", "/")

	// 生成配置内容
	config := fmt.Sprintf(`# SmartDNSSort Embedded Unbound Configuration (Windows)
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
