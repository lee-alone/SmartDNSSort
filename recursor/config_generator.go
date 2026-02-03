package recursor

import (
	"fmt"
	"path/filepath"
	"runtime"
	"smartdnssort/logger"
	"strconv"
	"strings"
)

// ConfigGenerator 生成 unbound 配置
type ConfigGenerator struct {
	version     string
	sysInfo     SystemInfo
	port        int
	rootZoneMgr *RootZoneManager // 新增：root.zone管理器
}

// NewConfigGenerator 创建新的 ConfigGenerator
func NewConfigGenerator(version string, sysInfo SystemInfo, port int) *ConfigGenerator {
	return &ConfigGenerator{
		version:     version,
		sysInfo:     sysInfo,
		port:        port,
		rootZoneMgr: NewRootZoneManager(), // 初始化root.zone管理器
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
// 如果版本解析失败，返回保守的特性集合
func (cg *ConfigGenerator) GetVersionFeatures() VersionFeatures {
	ver, err := cg.parseVersion(cg.version)
	if err != nil {
		// 版本解析失败，使用保守的特性集合
		return VersionFeatures{
			ServeExpired:         false,
			ServeExpiredTTL:      false,
			ServeExpiredReplyTTL: false,
			PrefetchKey:          false,
			QnameMinimisation:    false,
			MinimalResponses:     false,
			UseCapsForID:         false,
			SoReuseport:          false,
		}
	}

	// Windows 不支持 so-reuseport
	soReuseportSupported := (ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 6)) && runtime.GOOS != "windows"

	return VersionFeatures{
		ServeExpired:         ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 9),
		ServeExpiredTTL:      ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 9),
		ServeExpiredReplyTTL: ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 9),
		PrefetchKey:          ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 9),
		QnameMinimisation:    ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 6),
		MinimalResponses:     ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 9),
		UseCapsForID:         ver.Major > 1 || (ver.Major == 1 && ver.Minor >= 6),
		SoReuseport:          soReuseportSupported,
	}
}

// parseVersion 解析版本号
// 返回错误如果版本字符串无效
func (cg *ConfigGenerator) parseVersion(version string) (struct {
	Major, Minor, Patch int
}, error) {
	if version == "" {
		return struct{ Major, Minor, Patch int }{}, fmt.Errorf("empty version string")
	}

	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return struct{ Major, Minor, Patch int }{}, fmt.Errorf("invalid version format: %s", version)
	}

	major, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return struct{ Major, Minor, Patch int }{}, fmt.Errorf("invalid major version: %w", err)
	}

	minor := 0
	if len(parts) > 1 {
		minor, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return struct{ Major, Minor, Patch int }{}, fmt.Errorf("invalid minor version: %w", err)
		}
	}

	patch := 0
	if len(parts) > 2 {
		patch, err = strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil {
			return struct{ Major, Minor, Patch int }{}, fmt.Errorf("invalid patch version: %w", err)
		}
	}

	return struct{ Major, Minor, Patch int }{major, minor, patch}, nil
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
// 根据 CPU 核数和内存大小动态计算参数
// 优化目标：降低内存占用，因为上层应用已有完整缓存
func (cg *ConfigGenerator) CalculateParams() ConfigParams {
	// CPU 线程数 - 保持现有策略以维持高速响应
	numThreads := max(1, min(cg.sysInfo.CPUCores, 8))

	// 缓存大小 - 大幅降低，因为上层 SmartDNSSort 已有优秀的缓存层
	// Unbound 只作为递归解析器，不承载缓存压力
	var msgCacheSize, rrsetCacheSize int

	if cg.sysInfo.MemoryGB > 0 {
		// 递归解析器模式：使用固定的小缓存
		// msg-cache: 10-20MB（原 25-500MB）
		// rrset-cache: 20-40MB（原 50-1000MB）
		msgCacheSize = 10 + (2 * numThreads)   // 10-26MB
		rrsetCacheSize = 20 + (4 * numThreads) // 20-52MB
	} else {
		// 无内存信息时使用保守的小缓存
		msgCacheSize = 10 + numThreads
		rrsetCacheSize = 20 + (2 * numThreads)
	}

	return ConfigParams{
		NumThreads:     numThreads,
		MsgCacheSize:   msgCacheSize,
		RRsetCacheSize: rrsetCacheSize,
		OutgoingRange:  4096,
		SoRcvbuf:       "1m", // 降低从 8m 到 1m
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
# 
# 配置原则：Unbound 作为递归解析器，不重复缓存
# 上层 SmartDNSSort 应用已有完整的缓存层

server:
    # 监听配置
    interface: 127.0.0.1@%d
    do-ip4: yes
    do-ip6: yes
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
    
    # 缓存策略 - 快速刷新，不重复缓存
    cache-max-ttl: 86400
    cache-min-ttl: 60
    cache-max-negative-ttl: 3600
    serve-expired: yes
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
	if features.ServeExpiredTTL {
		config += "    serve-expired-ttl: 3600\n"
	}
	if features.ServeExpiredReplyTTL {
		config += "    serve-expired-reply-ttl: 30\n"
	}
	if features.PrefetchKey {
		config += "    prefetch-key: yes\n"
	}

	config += `    
    # 预取优化 - 禁用，因为上层已处理
    prefetch: no
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

	// 添加root.zone配置（如果可用）
	if cg.rootZoneMgr != nil {
		rootZoneConfig, err := cg.rootZoneMgr.GetRootZoneConfig()
		if err == nil {
			config += rootZoneConfig
		} else {
			logger.Warnf("[Config] Failed to generate root.zone config: %v", err)
		}
	}

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
