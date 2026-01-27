package upstream

import (
	"context"
	"errors"
	"runtime"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/stats"
	"strings"
	"sync"
	"time"

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
	// 动态参数优化
	dynamicParamOptimization *DynamicParamOptimization
	// 策略性能指标
	strategyMetrics *StrategyMetrics
}

// DynamicParamOptimization 动态参数优化
type DynamicParamOptimization struct {
	mu sync.RWMutex

	// EWMA 平滑因子
	ewmaAlpha float64

	// 最大步长（毫秒）
	maxStepMs int

	// 平均延迟（EWMA）
	avgLatency time.Duration

	// 上次调整时间
	lastAdjustTime time.Time

	// 调整历史
	adjustmentCount int
}

// StrategyMetrics 策略性能指标
type StrategyMetrics struct {
	mu sync.RWMutex

	// 策略性能统计
	strategyStats map[string]*StrategyStats

	// 上次评估时间
	lastEvalTime time.Time
}

// StrategyStats 策略统计信息
type StrategyStats struct {
	// 响应时间统计
	totalLatency time.Duration
	requestCount int64
	avgLatency   time.Duration

	// 错误统计
	errorCount int64
	errorRate  float64

	// 吞吐量统计
	successCount int64
	successRate  float64
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
	strategy := cfg.Strategy
	if strategy == "" || strategy == "auto" {
		switch {
		case numServers <= 1:
			strategy = "sequential"
		case numServers <= 3:
			strategy = "racing"
		default:
			strategy = "parallel"
		}
		logger.Infof("[Manager] Auto-selected strategy: %s (based on %d servers)", strategy, numServers)
	}

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

// RecordQueryLatency 记录查询延迟，用于动态参数优化
func (u *Manager) RecordQueryLatency(latency time.Duration) {
	if u.dynamicParamOptimization == nil {
		return
	}

	u.dynamicParamOptimization.mu.Lock()
	defer u.dynamicParamOptimization.mu.Unlock()

	// 使用 EWMA 更新平均延迟
	if u.dynamicParamOptimization.avgLatency == 0 {
		u.dynamicParamOptimization.avgLatency = latency
	} else {
		alpha := u.dynamicParamOptimization.ewmaAlpha
		newAvg := time.Duration(alpha*float64(latency) + (1.0-alpha)*float64(u.dynamicParamOptimization.avgLatency))
		u.dynamicParamOptimization.avgLatency = newAvg
	}
}

// GetAverageLatency 获取平均延迟
func (u *Manager) GetAverageLatency() time.Duration {
	if u.dynamicParamOptimization == nil {
		return 200 * time.Millisecond
	}

	u.dynamicParamOptimization.mu.RLock()
	defer u.dynamicParamOptimization.mu.RUnlock()

	return u.dynamicParamOptimization.avgLatency
}

// GetAdaptiveRacingDelay 获取自适应竞速延迟
func (u *Manager) GetAdaptiveRacingDelay() time.Duration {
	avgLatency := u.GetAverageLatency()

	// 竞速延迟 = 平均延迟的 10%
	delay := avgLatency / 10

	// 限制范围：50-200ms
	if delay < 50*time.Millisecond {
		delay = 50 * time.Millisecond
	}
	if delay > 200*time.Millisecond {
		delay = 200 * time.Millisecond
	}

	return delay
}

// GetAdaptiveSequentialTimeout 获取自适应顺序查询超时
func (u *Manager) GetAdaptiveSequentialTimeout() time.Duration {
	avgLatency := u.GetAverageLatency()

	// 顺序查询超时 = 平均延迟 * 1.5
	timeout := time.Duration(float64(avgLatency) * 1.5)

	// 限制范围：500ms-2s
	if timeout < 500*time.Millisecond {
		timeout = 500 * time.Millisecond
	}
	if timeout > 2*time.Second {
		timeout = 2 * time.Second
	}

	return timeout
}

// GetDynamicParamStats 获取动态参数优化的统计信息
func (u *Manager) GetDynamicParamStats() map[string]interface{} {
	if u.dynamicParamOptimization == nil {
		return map[string]interface{}{}
	}

	u.dynamicParamOptimization.mu.RLock()
	defer u.dynamicParamOptimization.mu.RUnlock()

	return map[string]interface{}{
		"avg_latency_ms":        float64(u.dynamicParamOptimization.avgLatency.Microseconds()) / 1000.0,
		"ewma_alpha":            u.dynamicParamOptimization.ewmaAlpha,
		"max_step_ms":           u.dynamicParamOptimization.maxStepMs,
		"adjustment_count":      u.dynamicParamOptimization.adjustmentCount,
		"racing_delay_ms":       float64(u.GetAdaptiveRacingDelay().Microseconds()) / 1000.0,
		"sequential_timeout_ms": float64(u.GetAdaptiveSequentialTimeout().Microseconds()) / 1000.0,
	}
}

// RecordStrategyResult 记录查询结果用于策略评估
func (u *Manager) RecordStrategyResult(strategy string, latency time.Duration, success bool) {
	if u.strategyMetrics == nil {
		return
	}

	u.strategyMetrics.mu.Lock()
	defer u.strategyMetrics.mu.Unlock()

	stats, ok := u.strategyMetrics.strategyStats[strategy]
	if !ok {
		stats = &StrategyStats{}
		u.strategyMetrics.strategyStats[strategy] = stats
	}

	stats.totalLatency += latency
	stats.requestCount++

	if success {
		stats.successCount++
	} else {
		stats.errorCount++
	}
}

// SelectOptimalStrategy 基于历史性能数据选择最优策略
func (u *Manager) SelectOptimalStrategy() string {
	if u.strategyMetrics == nil {
		return u.strategy
	}

	u.strategyMetrics.mu.RLock()
	defer u.strategyMetrics.mu.RUnlock()

	var bestStrategy string
	var bestScore float64

	for strategy, stats := range u.strategyMetrics.strategyStats {
		// 需要足够的样本数据
		if stats.requestCount < 10 {
			continue
		}

		// 计算综合评分
		// 评分 = 成功率 * 100 - 平均延迟 / 10
		successRate := float64(stats.successCount) / float64(stats.requestCount)
		avgLatency := float64(stats.totalLatency.Milliseconds()) / float64(stats.requestCount)
		score := successRate*100 - avgLatency/10

		if score > bestScore {
			bestScore = score
			bestStrategy = strategy
		}
	}

	// 如果没有找到最优策略，返回当前策略
	if bestStrategy == "" {
		return u.strategy
	}

	return bestStrategy
}

// EvaluateStrategyPerformance 定期评估策略性能
func (u *Manager) EvaluateStrategyPerformance() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if u.strategyMetrics == nil {
			continue
		}

		u.strategyMetrics.mu.Lock()

		// 计算每个策略的平均延迟和成功率
		for _, stats := range u.strategyMetrics.strategyStats {
			if stats.requestCount > 0 {
				stats.avgLatency = time.Duration(stats.totalLatency.Nanoseconds() / stats.requestCount)
				stats.errorRate = float64(stats.errorCount) / float64(stats.requestCount)
				stats.successRate = 1.0 - stats.errorRate
			}
		}

		u.strategyMetrics.lastEvalTime = time.Now()
		u.strategyMetrics.mu.Unlock()

		logger.Debugf("[Manager] 策略性能评估完成")
	}
}

// GetStrategyMetrics 获取策略性能指标
func (u *Manager) GetStrategyMetrics() map[string]interface{} {
	if u.strategyMetrics == nil {
		return map[string]interface{}{}
	}

	u.strategyMetrics.mu.RLock()
	defer u.strategyMetrics.mu.RUnlock()

	metrics := make(map[string]interface{})

	for strategy, stats := range u.strategyMetrics.strategyStats {
		if stats.requestCount == 0 {
			continue
		}

		avgLatency := float64(stats.totalLatency.Milliseconds()) / float64(stats.requestCount)
		successRate := float64(stats.successCount) / float64(stats.requestCount)

		metrics[strategy] = map[string]interface{}{
			"request_count":  stats.requestCount,
			"success_count":  stats.successCount,
			"error_count":    stats.errorCount,
			"avg_latency_ms": avgLatency,
			"success_rate":   successRate,
			"error_rate":     stats.errorRate,
		}
	}

	return metrics
}

// PerformanceMetrics 性能指标
type PerformanceMetrics struct {
	mu sync.RWMutex

	// 响应时间分布
	latencyHistogram map[string]int64 // 延迟分布（毫秒）

	// 吞吐量统计
	throughputCounter int64

	// 错误统计
	errorCounter int64

	// 可用性统计
	availabilityCounter int64

	// 时间戳
	lastResetTime time.Time
}

// GetPerformanceMetrics 获取性能指标
func (u *Manager) GetPerformanceMetrics() map[string]interface{} {
	// 计算 P50、P95、P99 延迟
	// 计算吞吐量
	// 计算错误率
	// 计算可用性

	u.strategyMetrics.mu.RLock()
	defer u.strategyMetrics.mu.RUnlock()

	totalRequests := int64(0)
	totalErrors := int64(0)
	totalLatency := time.Duration(0)

	for _, stats := range u.strategyMetrics.strategyStats {
		totalRequests += stats.requestCount
		totalErrors += stats.errorCount
		totalLatency += stats.totalLatency
	}

	var avgLatency float64
	if totalRequests > 0 {
		avgLatency = float64(totalLatency.Milliseconds()) / float64(totalRequests)
	}

	var errorRate float64
	if totalRequests > 0 {
		errorRate = float64(totalErrors) / float64(totalRequests) * 100
	}

	availability := 100.0 - errorRate

	return map[string]interface{}{
		"total_requests": totalRequests,
		"total_errors":   totalErrors,
		"avg_latency_ms": avgLatency,
		"error_rate":     errorRate,
		"availability":   availability,
	}
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
