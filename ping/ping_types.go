package ping

import (
	"sync"
	"time"

	"smartdnssort/connectivity"

	"golang.org/x/net/icmp"
	"golang.org/x/sync/singleflight"
)

// LogicDeadRTT 逻辑失效阈值（毫秒）
// 用于统一全系统的"判死标准"
// 当 RTT >= LogicDeadRTT 时，认为 IP 不可达或极不稳定
const LogicDeadRTT = 9000

// Result 表示单个 IP 的 ping 结果
type Result struct {
	IP          string
	RTT         int // 毫秒，LogicDeadRTT 表示不可达
	Loss        float64
	ProbeMethod string // 探测方法：icmp（纯 ICMP 模式）
	FastFail    bool   // 标记是否触发了快速失败（避免两重记录）
}

// rttCacheEntry 缓存条目
type rttCacheEntry struct {
	rtt       int
	loss      float64   // 丢包率，用于负向缓存
	staleAt   time.Time // 软过期时间：超过此时间，缓存变为陈旧，返回旧数据并异步刷新
	expiresAt time.Time // 硬过期时间：超过此时间，缓存完全失效并被清理
}

// Pinger DNS IP 延迟测量和排序工具
// 纯 ICMP 探测模式：只使用 ICMP echo request/reply 测试 IP 可达性
type Pinger struct {
	// === 基础探测配置 ===
	count              int    // 每个 IP 的探测次数，默认 3
	timeoutMs          int    // 单次探测超时时间（毫秒），默认 800ms
	concurrency        int    // 并发探测的 IP 数量，默认 8
	strategy           string // 已弃用：保留用于向后兼容
	maxTestIPs         int    // 最多测试的 IP 数量，0 表示全部
	rttCacheTtlSeconds int    // RTT 缓存基础 TTL（秒）

	// === 缓存与资源管理 ===
	rttCache         *shardedRttCache                  // 分片 RTT 缓存，减少锁竞争（32 分片）
	stopChan         chan struct{}                     // 停止信号通道，用于优雅关闭
	failureWeightMgr *IPFailureWeightManager           // IP 失效权重管理器，用于排序惩罚
	probeFlight      *singleflight.Group               // 请求合并，避免重复探测同一 IP
	ipPool           *IPPool                           // 全局 IP 资源管理器，用于 IP 监控器获取 IP 列表
	healthChecker    connectivity.NetworkHealthChecker // 网络健康检查器，用于断网时防止缓存污染

	// === Stale-While-Revalidate 相关 ===
	staleRevalidateMu sync.Mutex      // 保护 staleRevalidating 的互斥锁
	staleRevalidating map[string]bool // 记录正在异步更新的 IP，避免重复触发
	staleGracePeriod  time.Duration   // 软过期容忍期，默认 30 秒

	// === 全局 ICMP 调度器相关字段 ===
	pendingProbes sync.Map         // 键为 ID (uint16)，值为回调 chan time.Time
	idCounter     uint32           // 循环生成唯一序列号，用于 ICMP Echo ID
	v4Conn        *icmp.PacketConn // IPv4 单例监听器
	v6Conn        *icmp.PacketConn // IPv6 单例监听器
	icmpReady     chan struct{}    // ICMP 监听器就绪信号
	v4IsUDP       bool             // IPv4 是否使用 UDP 模式（否则为 RAW 模式）
	v6IsUDP       bool             // IPv6 是否使用 UDP 模式（否则为 RAW 模式）

	// === TCP 回退探测配置（用于解决 ICMP 被限速/丢弃环境） ===
	enableTCPFallback bool  // 是否启用 TCP 补全，默认 true
	tcpFallbackPorts  []int // 补全探测端口，默认 [443, 80]
	tcpThresholdMs    int   // 触发补全的 ICMP 延迟阈值（毫秒），默认 1000ms

	// === TTL 阈值配置（修复 #7：使系统能够自适应不同延迟基准的网络环境） ===
	rttThresholdExcellent int // 极优 IP 的 RTT 阈值（毫秒），默认 50ms
	rttThresholdGood      int // 优质 IP 的 RTT 阈值（毫秒），默认 100ms

	// === EWMA 平滑系数配置（第四阶段：参数外移配置化） ===
	deadThresholdMs int     // 逻辑失效阈值（毫秒），对应 LogicDeadRTT，默认 9000ms
	alphaOnline     float64 // 在线时的 EWMA 系数，默认 0.3
	alphaOffline    float64 // 断网时的 EWMA 系数，默认 0.1
}
