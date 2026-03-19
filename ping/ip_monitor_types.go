package ping

import (
	"sync"
	"time"
)

// IPMonitorConfig IP 监控器配置
type IPMonitorConfig struct {
	// T0 核心池刷新间隔（秒）
	T0RefreshInterval int
	// T1 活跃池刷新间隔（秒）
	T1RefreshInterval int
	// T2 淘汰池刷新间隔（秒）
	T2RefreshInterval int
	// 权重计算参数：引用计数权重
	RefCountWeight float64
	// 权重计算参数：访问热度权重
	AccessHeatWeight float64
	// 每次刷新的最大 IP 数量
	MaxRefreshPerCycle int
	// 并发测速数量
	RefreshConcurrency int
	// 是否启用监控
	Enabled bool
	// IP 池清理间隔（秒），默认 12 小时
	CleanupInterval int

	// === 优化配置：探测冷却时间（Cooldown / TTL Padding） ===
	// 启用探测冷却时间：如果缓存剩余 TTL 超过刷新周期的此比例，则跳过探测
	// 例如：T0 周期 120s，比例 0.5，则剩余 TTL > 60s 时跳过探测
	EnableCooldown bool
	CooldownRatio  float64 // 默认 0.5（50%）

	// === 优化配置：稳定性退避策略（Stability Backoff） ===
	// 启用稳定性退避：连续稳定的 IP 降级到低频池
	EnableStabilityBackoff bool
	StabilityThreshold     int     // 连续稳定次数阈值，默认 10
	StabilityRTTVariance   float64 // RTT 波动阈值（百分比），默认 0.05（5%）

	// === 优化配置：滑动窗口式巡检 ===
	// 启用滑动窗口：使用 "权重 + 时间" 单调增量逻辑选择 IP
	EnableSlidingWindow bool

	// === 优化配置：全局熔断与配额（Global Quota） ===
	// 每小时最大探测次数限制
	MaxPingsPerHour int
}

// DefaultIPMonitorConfig 默认配置
func DefaultIPMonitorConfig() IPMonitorConfig {
	return IPMonitorConfig{
		T0RefreshInterval:  120,  // 2 分钟
		T1RefreshInterval:  900,  // 15 分钟
		T2RefreshInterval:  3600, // 1 小时
		RefCountWeight:     1.0,
		AccessHeatWeight:   0.5,
		MaxRefreshPerCycle: 50,
		RefreshConcurrency: 10, // 并发测速数量
		Enabled:            true,
		CleanupInterval:    3600, // 1 小时（优化：从 12 小时缩短为 1 小时）

		// 优化配置默认值
		EnableCooldown:         true, // 启用探测冷却时间
		CooldownRatio:          0.5,  // 50% 剩余 TTL 时跳过
		EnableStabilityBackoff: true, // 启用稳定性退避
		StabilityThreshold:     10,   // 连续 10 次稳定
		StabilityRTTVariance:   0.05, // 5% RTT 波动阈值
		EnableSlidingWindow:    true, // 启用滑动窗口
		MaxPingsPerHour:        5000, // 每小时最大 5000 次探测
	}
}

// IPMonitorStats 监控器统计信息
type IPMonitorStats struct {
	TotalRefreshes    int64 // 扫描周期数（原来的）
	TotalPlannedPings int64 // 计划测速总数（原来的 TotalIPsRefreshed）
	TotalActualPings  int64 // 真正发出的 ICMP 包数量（新）
	TotalSkippedPings int64 // 被探测冷却/策略拦截的数量（新）
	LastRefreshTime   time.Time

	T0PoolSize int
	T1PoolSize int
	T2PoolSize int

	// 动态指标
	DowngradedIPs    int // 当前处于"稳定性降级"状态的 IP 总数（新）
	HourlyQuotaUsed  int // 本小时配额已使用量（新）
	HourlyQuotaLimit int // 本小时配额上限（新）
}

// weightedIP 带权重的 IP 结构体
type weightedIP struct {
	ip     string
	weight float64
}

// IPStabilityRecord IP 稳定性记录（用于稳定性退避策略）
type IPStabilityRecord struct {
	StableCount  int       // 连续稳定次数
	LastCheck    time.Time // 最后检查时间
	LastRTT      int       // 最后一次 RTT 值
	IsDowngraded bool      // 是否已降级到低频池
}

// IPMonitor IP 主动巡检调度器
// 实现三级分步刷新机制，根据权重优先级调度 IP 测速
type IPMonitor struct {
	pinger *Pinger
	config IPMonitorConfig
	stats  IPMonitorStats
	mu     sync.RWMutex
	stopCh chan struct{}

	// 优化功能相关字段
	stabilityRecords map[string]*IPStabilityRecord // IP 稳定性记录
	hourlyPingCount  int64                         // 本小时探测次数
	hourlyResetTime  time.Time                     // 小时重置时间
}

// NewIPMonitor 创建新的 IP 监控器
func NewIPMonitor(pinger *Pinger, config IPMonitorConfig) *IPMonitor {
	if config.T0RefreshInterval <= 0 {
		config.T0RefreshInterval = 120
	}
	if config.T1RefreshInterval <= 0 {
		config.T1RefreshInterval = 900
	}
	if config.T2RefreshInterval <= 0 {
		config.T2RefreshInterval = 3600
	}
	if config.RefCountWeight <= 0 {
		config.RefCountWeight = 1.0
	}
	if config.AccessHeatWeight <= 0 {
		config.AccessHeatWeight = 0.5
	}
	if config.MaxRefreshPerCycle <= 0 {
		config.MaxRefreshPerCycle = 50
	}
	if config.RefreshConcurrency <= 0 {
		config.RefreshConcurrency = 10
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 43200 // 默认 12 小时
	}
	if config.CooldownRatio <= 0 {
		config.CooldownRatio = 0.5
	}
	if config.StabilityThreshold <= 0 {
		config.StabilityThreshold = 10
	}
	if config.StabilityRTTVariance <= 0 {
		config.StabilityRTTVariance = 0.05
	}
	if config.MaxPingsPerHour <= 0 {
		config.MaxPingsPerHour = 5000
	}

	return &IPMonitor{
		pinger:           pinger,
		config:           config,
		stopCh:           make(chan struct{}),
		stabilityRecords: make(map[string]*IPStabilityRecord),
		hourlyResetTime:  time.Now(),
	}
}
