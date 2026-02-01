package recursor

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ConfigGenerator 生成 unbound 配置
type ConfigGenerator struct {
	version string
	sysInfo SystemInfo
	port    int
}

// NewConfigGenerator 创建新的 ConfigGenerator
func NewConfigGenerator(version string, sysInfo SystemInfo, port int) *ConfigGenerator {
	return &ConfigGenerator{
		version: version,
		sysInfo: sysInfo,
		port:    port,
	}
}

// VersionFeatures 版本特性
type VersionFeatures struct {
	ServeExpired         bool
	ServeExpiredTTL      bool
	ServeExpiredReplyTTL bool
	PrefetchKey          bool
	QnameMinimisation    bool
	MinimalResponses     bool
	UseCapsForID         bool
	SoReuseport          bool
}

// GetVersionFeatures 获取版本特性
func (cg *ConfigGenerator) GetVersionFeatures() VersionFeatures {
	ver := cg.parseVersion(cg.version)

	return VersionFeatures{
		ServeExpired:         ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 9),
		ServeExpiredTTL:      ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 9),
		ServeExpiredReplyTTL: ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 9),
		PrefetchKey:          ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 9),
		QnameMinimisation:    ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 6),
		MinimalResponses:     ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 9),
		UseCapsForID:         ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 6),
		SoReuseport:          ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 6),
	}
}

// parseVersion 解析版本号
func (cg *ConfigGenerator) parseVersion(version string) struct {
	Major, Minor, Patch int
} {
	parts := strings.Split(version, ".")
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	patch := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return struct {
		Major, Minor, Patch int
	}{major, minor, patch}
}

// ConfigParams 配置参数
type ConfigParams struct {
	NumThreads     int
	MsgCacheSize   int
	RRsetCacheSize int
	OutgoingRange  int
	SoRcvbuf       string
}

// CalculateParams 计算配置参数
func (cg *ConfigGenerator) CalculateParams() ConfigParams {
	// CPU 线程数
	numThreads := min(cg.sysInfo.CPUCores, 8)
	if numThreads < 1 {
		numThreads = 1
	}

	// 缓存大小（基于内存）
	// 如果无法获取内存信息，使用默认值
	msgCacheSize := 100
	rrsetCacheSize := 200

	if cg.sysInfo.MemoryGB > 0 {
		memMB := int(cg.sysInfo.MemoryGB * 1024)
		msgCacheSize = min(memMB*5/100, 500)
		rrsetCacheSize = min(memMB*10/100, 1000)
	}

	return ConfigParams{
		NumThreads:     numThreads,
		MsgCacheSize:   msgCacheSize,
		RRsetCacheSize: rrsetCacheSize,
		OutgoingRange:  4096,
		SoRcvbuf:       "8m",
	}
}

// GenerateConfig 生成配置文件内容
func (cg *ConfigGenerator) GenerateConfig() (string, error) {
	features := cg.GetVersionFeatures()
	params := cg.CalculateParams()

	// 获取 root.key 路径
	rootKeyPath := cg.getRootKeyPath()

	config := fmt.Sprintf(`# SmartDNSSort Embedded Unbound Configuration
# Auto-generated, do not edit manually
# Generated for %d CPU cores, %.1f GB memory
# Unbound version: %s

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
    outgoing-range: %d
    so-rcvbuf: %s
    
    # 缓存策略
    cache-max-ttl: 86400
    cache-min-ttl: 60
`,
		cg.sysInfo.CPUCores,
		cg.sysInfo.MemoryGB,
		cg.version,
		cg.port,
		params.NumThreads,
		params.MsgCacheSize,
		params.RRsetCacheSize,
		params.OutgoingRange,
		params.SoRcvbuf,
	)

	// 条件性添加特性
	if features.ServeExpired {
		config += "    serve-expired: yes\n"
	}
	if features.ServeExpiredTTL {
		config += "    serve-expired-ttl: 86400\n"
	}
	if features.ServeExpiredReplyTTL {
		config += "    serve-expired-reply-ttl: 30\n"
	}
	if features.PrefetchKey {
		config += "    prefetch-key: yes\n"
	}

	config += `    
    # 预取优化
    prefetch: yes
`

	if features.QnameMinimisation {
		config += "    qname-minimisation: yes\n"
	}
	if features.MinimalResponses {
		config += "    minimal-responses: yes\n"
	}
	if features.UseCapsForID {
		config += "    use-caps-for-id: yes\n"
	}

	config += `    
    # 安全加固
    harden-dnssec-stripped: yes
    harden-glue: yes
    harden-referral-path: yes
`

	if features.SoReuseport {
		config += "    so-reuseport: yes\n"
	}

	config += fmt.Sprintf(`    
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
`, rootKeyPath)

	return config, nil
}

// getRootKeyPath 获取 root.key 路径
func (cg *ConfigGenerator) getRootKeyPath() string {
	if runtime.GOOS == "linux" {
		return "/etc/unbound/root.key"
	}
	// Windows - 使用绝对路径
	configDir, _ := GetUnboundConfigDir()
	absPath, _ := filepath.Abs(filepath.Join(configDir, "root.key"))
	// 在 Windows 上，unbound 配置文件中的路径需要使用正斜杠或转义反斜杠
	return strings.ReplaceAll(absPath, "\\", "/")
}

// ValidateConfig 验证配置
func (cg *ConfigGenerator) ValidateConfig() error {
	// 基本验证
	if cg.port < 1024 || cg.port > 65535 {
		return fmt.Errorf("invalid port: %d", cg.port)
	}

	if cg.sysInfo.CPUCores < 1 {
		return fmt.Errorf("invalid CPU cores: %d", cg.sysInfo.CPUCores)
	}

	return nil
}

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
