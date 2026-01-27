package upstream

import (
	"context"
	"errors"
	"smartdnssort/logger"
	"smartdnssort/stats"
	"strings"

	"github.com/miekg/dns"
)

// QueryResult 查询结果
type QueryResult struct {
	Records           []dns.RR // 通用记录列表（所有类型的 DNS 记录）
	IPs               []string
	CNAMEs            []string // 支持多 CNAME 记录
	TTL               uint32   // 上游 DNS 返回的 TTL（对所有 IP 取最小值）
	Error             error
	Server            string   // 添加服务器字段
	Rcode             int      // DNS 响应代码
	AuthenticatedData bool     // DNSSEC 验证标记 (AD flag)
	DnsMsg            *dns.Msg // 原始 DNS 消息（包含完整的 RRSIG 等 DNSSEC 数据）
}

// QueryResultWithTTL 带 TTL 信息的查询结果
type QueryResultWithTTL struct {
	Records           []dns.RR // 通用记录列表（所有类型的 DNS 记录）
	IPs               []string
	CNAMEs            []string // 支持多 CNAME 记录
	TTL               uint32   // 上游 DNS 返回的 TTL
	AuthenticatedData bool     // DNSSEC 验证标记 (AD flag)
	DnsMsg            *dns.Msg // 原始 DNS 消息（包含完整的 RRSIG 等 DNSSEC 数据）
}

// Manager 上游 DNS 查询管理器
type Manager struct {
	servers     []*HealthAwareUpstream // 带健康检查的上游服务器列表
	strategy    string                 // parallel, random, sequential, racing
	timeoutMs   int
	concurrency int // 并行查询时的并发数
	stats       *stats.Stats
	// racing 策略配置
	racingDelayMs       int // 竞速策略的起始延迟（毫秒）
	racingMaxConcurrent int // 竞速策略中同时发起的最大请求数
	// sequential 策略配置
	sequentialTimeoutMs int // 顺序尝试的单次超时
	// 缓存更新回调函数，用于在 parallel 模式下后台收集完所有响应后更新缓存
	cacheUpdateCallback func(domain string, qtype uint16, records []dns.RR, cnames []string, ttl uint32)
}

// NewManager 创建上游 DNS 管理器
func NewManager(servers []Upstream, strategy string, timeoutMs int, concurrency int, s *stats.Stats, healthConfig *HealthCheckConfig, racingDelayMs int, racingMaxConcurrent int, sequentialTimeoutMs int) *Manager {
	if strategy == "" {
		strategy = "random"
	}
	if timeoutMs <= 0 {
		timeoutMs = 5000 // 统一默认 5s，给递归服务器留够时间
	}
	// 优化：并发数至少应等于服务器数量，除非外部显式限制
	if concurrency < len(servers) {
		concurrency = len(servers)
	}
	if racingDelayMs <= 0 {
		racingDelayMs = 100 // 默认 100ms
	}
	// 优化：竞速并发数至少应等于服务器数量
	if racingMaxConcurrent < len(servers) {
		racingMaxConcurrent = len(servers)
	}
	if sequentialTimeoutMs <= 0 {
		sequentialTimeoutMs = 1500 // 默认 1.5s
	}

	// 将普通 Upstream 包装为 HealthAwareUpstream
	healthAwareServers := make([]*HealthAwareUpstream, len(servers))
	for i, server := range servers {
		healthAwareServers[i] = NewHealthAwareUpstream(server, healthConfig)
	}

	return &Manager{
		servers:             healthAwareServers,
		strategy:            strategy,
		timeoutMs:           timeoutMs,
		concurrency:         concurrency,
		stats:               s,
		racingDelayMs:       racingDelayMs,
		racingMaxConcurrent: racingMaxConcurrent,
		sequentialTimeoutMs: sequentialTimeoutMs,
	}
}

// SetCacheUpdateCallback 设置缓存更新回调函数
// 用于在 parallel 模式下后台收集完所有响应后更新缓存
func (u *Manager) SetCacheUpdateCallback(callback func(domain string, qtype uint16, records []dns.RR, cnames []string, ttl uint32)) {
	u.cacheUpdateCallback = callback
}

// GetServers 返回所有上游服务器列表
func (u *Manager) GetServers() []Upstream {
	result := make([]Upstream, len(u.servers))
	for i, server := range u.servers {
		result[i] = server
	}
	return result
}

// GetHealthyServerCount 返回当前健康的服务器数量
// 用于计算动态超时时间
func (u *Manager) GetHealthyServerCount() int {
	count := 0
	for _, server := range u.servers {
		if !server.ShouldSkipTemporarily() {
			count++
		}
	}
	return count
}

// GetTotalServerCount 返回总服务器数量
func (u *Manager) GetTotalServerCount() int {
	return len(u.servers)
}

// Query 查询域名，返回 IP 列表和 TTL
func (u *Manager) Query(ctx context.Context, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	if len(r.Question) == 0 {
		return nil, errors.New("query message has no questions")
	}
	question := r.Question[0]
	domain := strings.TrimRight(question.Name, ".")
	qtype := question.Qtype

	switch u.strategy {
	case "parallel":
		return u.queryParallel(ctx, domain, qtype, r, dnssec)
	case "sequential":
		return u.querySequential(ctx, domain, qtype, r, dnssec)
	case "racing":
		return u.queryRacing(ctx, domain, qtype, r, dnssec)
	default:
		return u.queryRandom(ctx, domain, qtype, r, dnssec)
	}
}

// Close 关闭所有上游连接池
func (u *Manager) Close() error {
	for _, server := range u.servers {
		// 尝试关闭底层上游的连接池
		if upstream, ok := server.upstream.(interface{ Close() error }); ok {
			if err := upstream.Close(); err != nil {
				logger.Warnf("[Manager] Failed to close upstream %s: %v", server.Address(), err)
			}
		}
	}
	return nil
}
