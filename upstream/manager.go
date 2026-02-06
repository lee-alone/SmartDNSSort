package upstream

import (
	"context"
	"errors"
	"runtime"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/stats"
	"strings"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
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
	// queryVersion 参数用于防止旧的后台补全覆盖新的缓存
	cacheUpdateCallback func(domain string, qtype uint16, records []dns.RR, cnames []string, ttl uint32, queryVersion int64)
	// 动态参数优化
	dynamicParamOptimization *DynamicParamOptimization
	// 策略性能指标
	strategyMetrics *StrategyMetrics
	// 请求去重组
	requestGroup singleflight.Group
	// 两阶段并行配置
	activeTierSize      int           // 第一梯队并发数（默认 2）
	fallbackTimeout     time.Duration // 第一梯队未响应时提早启动第二梯队的等待时间（默认 300ms）
	batchSize           int           // 第二梯队每批次启动的数量（默认 2）
	staggerDelay        time.Duration // 批次间的步进延迟（默认 50ms）
	totalCollectTimeout time.Duration // 背景补全的最大总时长（默认 3s）
}

// QueryPriority 查询优先级
type QueryPriority int

const (
	PriorityHigh   QueryPriority = 3 // 热点域名
	PriorityNormal QueryPriority = 2 // 普通查询
	PriorityLow    QueryPriority = 1 // 后台查询
)

// NewManager 创建上游 DNS 管理器
func NewManager(cfg *config.UpstreamConfig, servers []Upstream, s *stats.Stats) *Manager {
	numServers := len(servers)

	// 1. 策略自适应选择
	strategy := selectInitialStrategy(cfg, numServers)

	// 2. 参数三层优先级逻辑：用户配置 > 自动计算 > 硬编码默认值
	timeoutMs := cfg.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 5000 // 默认 5s
	}

	concurrency := derefOrDefault(cfg.Concurrency, runtime.NumCPU()*10) // 用户配置 > CPU 核数 * 10
	if concurrency < numServers {
		concurrency = numServers // 确保并发数至少等于服务器数量
	}

	sequentialTimeoutMs := derefOrDefault(cfg.SequentialTimeout, 1000) // 用户配置 > 默认 1.0s（优化：从 1.5s 改为 1.0s）

	racingDelayMs := derefOrDefault(cfg.RacingDelay, 100) // 用户配置 > 默认 100ms

	racingMaxConcurrent := derefOrDefault(cfg.RacingMaxConcurrent, numServers) // 用户配置 > 服务器数量
	if racingMaxConcurrent < 2 {
		racingMaxConcurrent = 2 // 竞速模式至少需要2个并发
	}
	if racingMaxConcurrent > numServers {
		racingMaxConcurrent = numServers
	}

	// 将普通 Upstream 包装为 HealthAwareUpstream
	healthAwareServers := make([]*HealthAwareUpstream, len(servers))
	for i, server := range servers {
		healthAwareServers[i] = NewHealthAwareUpstream(server, convertConfigHealthCheck(&cfg.HealthCheck))
	}

	// 初始化动态参数优化
	ewmaAlpha := 0.2 // 默认 EWMA 因子
	if cfg.DynamicParamOptimization.EWMAAlpha != nil {
		ewmaAlpha = *cfg.DynamicParamOptimization.EWMAAlpha
	}

	maxStepMs := 10 // 默认最大步长 10ms
	if cfg.DynamicParamOptimization.MaxStepMs != nil {
		maxStepMs = *cfg.DynamicParamOptimization.MaxStepMs
	}

	dynamicOpt := &DynamicParamOptimization{
		ewmaAlpha:      ewmaAlpha,
		maxStepMs:      maxStepMs,
		avgLatency:     200 * time.Millisecond, // 初始平均延迟
		lastAdjustTime: time.Now(),
	}

	logger.Infof(
		"[Manager] Initialized with strategy: %s, timeout: %dms, concurrency: %d, racingDelay: %dms, racingMaxConcurrent: %d, sequentialTimeout: %dms",
		strategy, timeoutMs, concurrency, racingDelayMs, racingMaxConcurrent, sequentialTimeoutMs,
	)

	// 初始化策略性能指标
	strategyMetrics := &StrategyMetrics{
		strategyStats: make(map[string]*StrategyStats),
		lastEvalTime:  time.Now(),
	}

	// 初始化所有策略的统计信息
	for _, s := range []string{"sequential", "parallel", "racing", "random"} {
		strategyMetrics.strategyStats[s] = &StrategyStats{}
	}

	return &Manager{
		servers:                  healthAwareServers,
		strategy:                 strategy,
		timeoutMs:                timeoutMs,
		concurrency:              concurrency,
		stats:                    s,
		racingDelayMs:            racingDelayMs,
		racingMaxConcurrent:      racingMaxConcurrent,
		sequentialTimeoutMs:      sequentialTimeoutMs,
		dynamicParamOptimization: dynamicOpt,
		strategyMetrics:          strategyMetrics,
		// 两阶段并行配置
		activeTierSize:      2,
		fallbackTimeout:     300 * time.Millisecond,
		batchSize:           2,
		staggerDelay:        50 * time.Millisecond,
		totalCollectTimeout: 3 * time.Second,
	}
}

// convertConfigHealthCheck 将 config.HealthCheckConfig 转换为 upstream.HealthCheckConfig
func convertConfigHealthCheck(cfg *config.HealthCheckConfig) *HealthCheckConfig {
	return &HealthCheckConfig{
		FailureThreshold:        cfg.FailureThreshold,
		CircuitBreakerThreshold: cfg.CircuitBreakerThreshold,
		CircuitBreakerTimeout:   cfg.CircuitBreakerTimeout,
		SuccessThreshold:        cfg.SuccessThreshold,
	}
}

// derefOrDefault 解引用指针，如果指针为空则返回默认值
func derefOrDefault(ptr *int, defaultValue int) int {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}

// SetCacheUpdateCallback 设置缓存更新回调函数
// 用于在 parallel 模式下后台收集完所有响应后更新缓存
// queryVersion 参数用于防止旧的后台补全覆盖新的缓存
func (u *Manager) SetCacheUpdateCallback(callback func(domain string, qtype uint16, records []dns.RR, cnames []string, ttl uint32, queryVersion int64)) {
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

// rawQuery 内部实际执行查询逻辑（不带去重）
func (u *Manager) rawQuery(ctx context.Context, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	if len(r.Question) == 0 {
		return nil, errors.New("query message has no questions")
	}
	question := r.Question[0]
	domain := strings.TrimRight(question.Name, ".")
	qtype := question.Qtype

	// 记录查询开始时间
	startTime := time.Now()

	// 选择最优策略（如果有足够的样本数据）
	queryStrategy := u.strategy
	optimalStrategy := u.SelectOptimalStrategy()
	if optimalStrategy != "" && optimalStrategy != u.strategy {
		queryStrategy = optimalStrategy
		logger.Debugf("[Manager] 使用自适应策略: %s (原策略: %s)", queryStrategy, u.strategy)
	}

	// 执行查询
	var result *QueryResultWithTTL
	var err error

	switch queryStrategy {
	case "parallel":
		result, err = u.queryParallel(ctx, domain, qtype, r, dnssec)
	case "sequential":
		result, err = u.querySequential(ctx, domain, qtype, r, dnssec)
	case "racing":
		result, err = u.queryRacing(ctx, domain, qtype, r, dnssec)
	default:
		result, err = u.queryRandom(ctx, domain, qtype, r, dnssec)
	}

	// 记录查询结果用于策略评估
	latency := time.Since(startTime)
	success := err == nil
	u.RecordStrategyResult(queryStrategy, latency, success)

	return result, err
}

// QueryWithPriority 根据优先级执行查询
func (u *Manager) QueryWithPriority(ctx context.Context, r *dns.Msg, priority QueryPriority, dnssec bool) (*QueryResultWithTTL, error) {
	// 根据优先级调整超时
	timeout := u.timeoutMs
	switch priority {
	case PriorityHigh:
		// 高优先级：缩短超时到 75%
		timeout = timeout * 3 / 4
	case PriorityLow:
		// 低优先级：延长超时到 200%
		timeout = timeout * 2
	}

	// 创建新的 context 并应用优先级超时
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	// 执行查询
	return u.Query(ctx, r, dnssec)
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

// SelectServerByLatency 基于延迟选择服务器
func (u *Manager) SelectServerByLatency() Upstream {
	var selectedServer *HealthAwareUpstream
	var minLatency time.Duration = time.Duration(1<<63 - 1) // MaxInt64

	for _, server := range u.servers {
		if server.ShouldSkipTemporarily() {
			continue
		}

		latency := server.GetHealth().GetLatency()
		if latency < minLatency {
			minLatency = latency
			selectedServer = server
		}
	}

	if selectedServer != nil {
		return selectedServer
	}

	// 如果没有可用的服务器，返回第一个
	if len(u.servers) > 0 {
		return u.servers[0]
	}

	return nil
}

// CalculateServerWeight 计算服务器权重
func (u *Manager) CalculateServerWeight(server *HealthAwareUpstream) float64 {
	if server == nil {
		return 0
	}

	latency := server.GetHealth().GetLatency()
	health := server.GetHealth()

	// 根据健康状态计算错误率
	var errorRate float64
	switch health.GetStatus() {
	case HealthStatusHealthy:
		errorRate = 0.0
	case HealthStatusDegraded:
		errorRate = 0.3
	case HealthStatusUnhealthy:
		errorRate = 1.0
	}

	// 权重 = (1 - 错误率) / (延迟 + 1)
	weight := (1.0 - errorRate) / (float64(latency.Milliseconds()) + 1)

	return weight
}

// ClearStats 清除所有上游服务器的统计数据
func (u *Manager) ClearStats() {
	for _, server := range u.servers {
		if server != nil && server.GetHealth() != nil {
			server.GetHealth().ClearStats()
		}
	}
	logger.Info("Cleared statistics for all upstream servers")
}
