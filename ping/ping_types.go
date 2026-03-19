package ping

import (
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/sync/singleflight"
	"smartdnssort/connectivity"
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
	count              int
	timeoutMs          int
	concurrency        int
	strategy           string // 已弃用：保留用于向后兼容
	maxTestIPs         int
	rttCacheTtlSeconds int

	rttCache         *shardedRttCache // 改为分片缓存，减少锁竞争
	stopChan         chan struct{}
	failureWeightMgr *IPFailureWeightManager
	probeFlight      *singleflight.Group  // 请求合并，避免重复探测同一 IP
	ipPool           *IPPool              // 全局 IP 资源管理器，用于 IP 监控器获取 IP 列表
	healthChecker    connectivity.NetworkHealthChecker // 网络健康检查器，用于断网时防止缓存污染

	// Stale-While-Revalidate 相关
	staleRevalidateMu sync.Mutex
	staleRevalidating map[string]bool // 记录正在异步更新的 IP，避免重复触发
	staleGracePeriod  time.Duration   // 软过期容忍期（默认 30 秒）

	// 全局 ICMP 调度器相关字段
	pendingProbes sync.Map         // 键为 ID (uint16)，值为回调 chan time.Time
	idCounter     uint32           // 循环生成唯一序列号
	v4Conn        *icmp.PacketConn // IPv4 单例监听器
	v6Conn        *icmp.PacketConn // IPv6 单例监听器
	icmpReady     chan struct{}    // ICMP 监听器就绪信号
	v4IsUDP       bool             // IPv4 是否使用 UDP 模式
	v6IsUDP       bool             // IPv6 是否使用 UDP 模式
}
