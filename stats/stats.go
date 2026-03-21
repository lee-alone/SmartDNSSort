package stats

import (
	"runtime"
	"smartdnssort/config"
	"smartdnssort/logger"
	"sync"
	"sync/atomic"
	"time"

	"smartdnssort/connectivity"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// Stats 运行统计
type Stats struct {
	mu                sync.RWMutex
	queries           int64
	effectiveQueries  int64 // 有效查询数（排除被广告拦截的查询）
	cacheHits         int64
	cacheMisses       int64
	cacheStaleRefresh int64 // 缓存更新：缓存已过期但返回给用户，同时向上游查询
	upstreamFailures  int64 // 总失败计数
	pingSuccesses     int64
	pingFailures      int64
	totalRTT          int64
	failedNodes       map[string]int64
	failedNodesTime   map[string]time.Time // 失败节点的时间戳，用于自动失效

	// 新增：Hot Domains 追踪器
	hotDomains *HotDomainsTracker

	// 新增：Blocked Domains 追踪器
	blockedDomains *BlockedDomainsTracker

	// 新增：通用统计的时间桶追踪器
	generalStatsTracker *GeneralStatsTracker

	// 缓存驱逐统计
	lastEvictionCount int64     // 上次记录的驱逐计数
	lastCheckTime     time.Time // 上次检查时间

	// 启动时间
	startTime time.Time

	// 网络健康检查器（用于断网时熔断外部行为统计）
	networkChecker connectivity.NetworkHealthChecker

	// 失败节点自动失效时间窗口（默认 24 小时）
	failedNodesTTL time.Duration

	// 缓存字段（用于减少 goroutine 创建）
	topDomainsCache      []DomainCount
	topBlockedCache      []BlockedDomainCount
	topDomainsUpdateTime time.Time
	topBlockedUpdateTime time.Time
	cacheMu              sync.RWMutex
	cacheTTL             time.Duration // 缓存有效期，如 5 秒

	// 停止通道，用于优雅停止后台协程
	stopChan chan struct{}
}

// NewStats 创建新的统计实例
func NewStats(cfg *config.StatsConfig) *Stats {
	// 初始化 gopsutil 的 CPU 使用率计算
	// 第一次调用 Percent 会返回 0，所以在这里预热一下
	go func() {
		_, err := cpu.Percent(time.Second, false)
		if err != nil {
			logger.Warnf("无法初始化 CPU 使用率统计: %v", err)
		}
	}()

	// 计算通用统计的桶数量
	retentionDays := cfg.GeneralStatsRetentionDays
	if retentionDays <= 0 {
		retentionDays = 7 // 默认 7 天
	}
	bucketMinutes := cfg.GeneralStatsBucketMinutes
	if bucketMinutes <= 0 {
		bucketMinutes = 60 // 默认 60 分钟
	}

	bucketCount := (retentionDays * 24 * 60) / bucketMinutes
	if bucketCount < 1 {
		bucketCount = 1
	}

	generalStatsTracker := NewGeneralStatsTracker(
		time.Duration(cfg.GeneralStatsBucketMinutes)*time.Minute,
		bucketCount,
	)

	s := &Stats{
		failedNodes:         make(map[string]int64),
		failedNodesTime:     make(map[string]time.Time),                       // 初始化时间戳 map
		hotDomains:          NewHotDomainsTrackerWithNetworkChecker(cfg, nil), // 初始为 nil，后续通过 SetNetworkChecker 设置
		blockedDomains:      NewBlockedDomainsTracker(cfg),
		generalStatsTracker: generalStatsTracker,
		startTime:           time.Now(),
		lastCheckTime:       time.Now(),
		failedNodesTTL:      24 * time.Hour,      // 默认 24 小时自动失效
		cacheTTL:            5 * time.Second,     // 缓存有效期 5 秒
		stopChan:            make(chan struct{}), // 初始化停止通道
	}

	// 启动后台定期更新协程（只启动一次）
	go s.startCacheRefresh()

	return s
}

// startCacheRefresh 后台定期更新缓存
func (s *Stats) startCacheRefresh() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.refreshCache()
		case <-s.stopChan: // 接收停止信号
			return // 退出协程
		}
	}
}

// refreshCache 在后台更新缓存
func (s *Stats) refreshCache() {
	// 在后台更新，不影响读请求
	topDomains := s.hotDomains.GetTopDomains(10)
	topBlocked := s.blockedDomains.GetTopBlockedDomains(10)

	s.cacheMu.Lock()
	s.topDomainsCache = topDomains
	s.topBlockedCache = topBlocked
	s.topDomainsUpdateTime = time.Now()
	s.topBlockedUpdateTime = time.Now()
	s.cacheMu.Unlock()
}

// IncQueries 增加查询计数
func (s *Stats) IncQueries() {
	atomic.AddInt64(&s.queries, 1)
	s.generalStatsTracker.RecordQuery()
}

// IncEffectiveQueries 增加有效查询计数（排除被广告拦截的查询）
func (s *Stats) IncEffectiveQueries() {
	atomic.AddInt64(&s.effectiveQueries, 1)
	s.generalStatsTracker.RecordEffectiveQuery()
}

// IncCacheHits 增加缓存命中计数
func (s *Stats) IncCacheHits() {
	atomic.AddInt64(&s.cacheHits, 1)
	s.generalStatsTracker.RecordCacheHit()
}

// IncCacheMisses 增加缓存未命中计数
func (s *Stats) IncCacheMisses() {
	atomic.AddInt64(&s.cacheMisses, 1)
	s.generalStatsTracker.RecordCacheMiss()
}

// IncCacheStaleRefresh 增加缓存更新计数（缓存已过期但返回给用户，同时向上游查询）
func (s *Stats) IncCacheStaleRefresh() {
	atomic.AddInt64(&s.cacheStaleRefresh, 1)
	s.generalStatsTracker.RecordCacheStaleRefresh()
}

// IncUpstreamFailures 增加上游失败计数 (总计)
// 熔断：断网时不记录，避免统计污染
func (s *Stats) IncUpstreamFailures() {
	// 断网时不记录上游失败，因为这是外部行为
	if s.networkChecker != nil && !s.networkChecker.IsNetworkHealthy() {
		return
	}
	atomic.AddInt64(&s.upstreamFailures, 1)
	s.generalStatsTracker.RecordUpstreamFailure()
}

// IncPingSuccesses 增加 ping 成功计数
// 熔断：断网时不记录，保持统计一致性
func (s *Stats) IncPingSuccesses() {
	// 断网时不记录 ping 成功，保持与 IncPingFailures 一致
	if s.networkChecker != nil && !s.networkChecker.IsNetworkHealthy() {
		return
	}
	atomic.AddInt64(&s.pingSuccesses, 1)
}

// IncPingFailures 增加 ping 失败计数
// 熔断：断网时不记录，避免统计污染
func (s *Stats) IncPingFailures() {
	// 断网时不记录 ping 失败，因为这是外部行为
	if s.networkChecker != nil && !s.networkChecker.IsNetworkHealthy() {
		return
	}
	atomic.AddInt64(&s.pingFailures, 1)
}

// AddRTT 增加总 RTT
// 熔断：断网时不记录，避免统计污染
func (s *Stats) AddRTT(rtt int64) {
	// 断网时不记录 RTT，因为这是外部行为
	if s.networkChecker != nil && !s.networkChecker.IsNetworkHealthy() {
		return
	}
	atomic.AddInt64(&s.totalRTT, rtt)
}

// RecordFailedNode 记录失败的节点
// 熔断：断网时不记录，避免统计污染
func (s *Stats) RecordFailedNode(node string) {
	// 断网时不记录失败节点，因为这是外部行为
	if s.networkChecker != nil && !s.networkChecker.IsNetworkHealthy() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// 自动清理过期的失败节点记录（惰性清理）
	now := time.Now()
	for ip, t := range s.failedNodesTime {
		if now.Sub(t) > s.failedNodesTTL {
			delete(s.failedNodes, ip)
			delete(s.failedNodesTime, ip)
		}
	}

	s.failedNodes[node]++
	s.failedNodesTime[node] = now
}

// SetNetworkChecker 设置网络健康检查器
func (s *Stats) SetNetworkChecker(checker connectivity.NetworkHealthChecker) {
	s.networkChecker = checker
	// 同时设置热门域名追踪器的网络检查器
	if s.hotDomains != nil {
		s.hotDomains.networkChecker = checker
	}
}

// GetStatsWithTimeRange 获取指定时间范围的统计数据
// days: 查询天数（1, 7, 30）
func (s *Stats) GetStatsWithTimeRange(days int) map[string]interface{} {
	// 参数验证
	if days < 1 || days > 90 {
		days = 7 // 默认7天
	}

	startTime := time.Now().AddDate(0, 0, -days)
	bucketStats := s.generalStatsTracker.Aggregate(startTime)

	// 计算缓存成功率
	totalCacheOps := bucketStats["cache_hits"] + bucketStats["cache_misses"]
	cacheSuccessRate := 0.0
	if totalCacheOps > 0 {
		cacheSuccessRate = float64(bucketStats["cache_hits"]) / float64(totalCacheOps) * 100
	}

	return map[string]interface{}{
		"total_queries":       bucketStats["queries"],
		"effective_queries":   bucketStats["effective_queries"],
		"cache_hits":          bucketStats["cache_hits"],
		"cache_misses":        bucketStats["cache_misses"],
		"cache_hit_rate":      cacheSuccessRate,
		"cache_stale_refresh": bucketStats["cache_stale_refresh"],
		"upstream_failures":   bucketStats["upstream_failures"],
		"time_range_days":     days,
	}
}

// GetStats 获取所有统计数据
// GetStats 获取所有统计数据（优化版本）
func (s *Stats) GetStats() map[string]interface{} {
	// 1. 先获取所有原子值（无锁）
	queries := atomic.LoadInt64(&s.queries)
	effectiveQueries := atomic.LoadInt64(&s.effectiveQueries)
	cacheHits := atomic.LoadInt64(&s.cacheHits)
	cacheMisses := atomic.LoadInt64(&s.cacheMisses)
	cacheStaleRefresh := atomic.LoadInt64(&s.cacheStaleRefresh)
	upstreamFailures := atomic.LoadInt64(&s.upstreamFailures)
	pingSuccesses := atomic.LoadInt64(&s.pingSuccesses)
	pingFailures := atomic.LoadInt64(&s.pingFailures)
	totalRTT := atomic.LoadInt64(&s.totalRTT)

	// 2. 快速获取失败节点快照（最小锁范围）
	s.mu.RLock()
	now := time.Now()
	failedNodesSnapshot := make(map[string]int64)
	for k, v := range s.failedNodes {
		if t, exists := s.failedNodesTime[k]; exists && now.Sub(t) <= s.failedNodesTTL {
			failedNodesSnapshot[k] = v
		}
	}
	s.mu.RUnlock()

	// 3. 直接读取缓存，不创建 goroutine
	s.cacheMu.RLock()
	topDomains := s.topDomainsCache
	topBlockedDomains := s.topBlockedCache
	cacheTime := s.topDomainsUpdateTime
	s.cacheMu.RUnlock()

	// 可选：如果缓存太旧，触发一次同步更新
	if time.Since(cacheTime) > 10*time.Second {
		go s.refreshCache() // 异步触发，不阻塞
	}

	// 4. 计算派生指标（无锁）
	var hitRate float64
	if effectiveQueries > 0 {
		hitRate = float64(cacheHits) / float64(effectiveQueries) * 100
	}

	var avgRTT int64
	if pingSuccesses > 0 {
		avgRTT = totalRTT / pingSuccesses
	}

	// 5. 获取系统状态（耗时操作，已在锁外）
	sysStats := s.getSystemStats()

	// 6. 构建返回结果
	return map[string]interface{}{
		"total_queries":       queries,
		"effective_queries":   effectiveQueries,
		"cache_hits":          cacheHits,
		"cache_misses":        cacheMisses,
		"cache_stale_refresh": cacheStaleRefresh,
		"cache_hit_rate":      hitRate,
		"upstream_failures":   upstreamFailures,
		"ping_successes":      pingSuccesses,
		"ping_failures":       pingFailures,
		"average_rtt_ms":      avgRTT,
		"failed_nodes":        failedNodesSnapshot,
		"system_stats":        sysStats,
		"top_domains":         topDomains,
		"top_blocked_domains": topBlockedDomains,
		"uptime_seconds":      time.Since(s.startTime).Seconds(),
		"evictions_per_min":   0.0,
	}
}

// getSystemStats 获取系统状态（提取为独立方法）
// 使用非阻塞采样，避免 goroutine 泄漏和死锁
func (s *Stats) getSystemStats() map[string]interface{} {
	// gopsutil 的 cpu.Percent 在 block=false 时立即返回
	// 第一次调用返回 0，后续调用返回上次的采样值
	var cpuUsage []float64
	usage, err := cpu.Percent(0, false) // 0 表示不阻塞
	if err != nil {
		logger.Warnf("无法获取 CPU 使用率: %v", err)
		cpuUsage = []float64{0.0}
	} else if len(usage) > 0 {
		cpuUsage = usage
	} else {
		cpuUsage = []float64{0.0}
	}

	memInfo, err := mem.VirtualMemory()
	if err != nil {
		logger.Warnf("无法获取内存信息: %v", err)
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	sysStats := map[string]interface{}{
		"cpu_cores":       runtime.NumCPU(),
		"cpu_usage_pct":   cpuUsage[0],
		"mem_total_mb":    0,
		"mem_used_mb":     0,
		"mem_usage_pct":   0.0,
		"go_mem_alloc_mb": memStats.Alloc / 1024 / 1024,
		"goroutines":      runtime.NumGoroutine(),
	}
	if memInfo != nil {
		sysStats["mem_total_mb"] = memInfo.Total / 1024 / 1024
		sysStats["mem_used_mb"] = memInfo.Used / 1024 / 1024
		sysStats["mem_usage_pct"] = memInfo.UsedPercent
	}

	return sysStats
}

// RecordDomainQuery 记录域名查询次数
func (s *Stats) RecordDomainQuery(domain string) {
	s.hotDomains.RecordQuery(domain)
}

// RecordBlockedDomain 记录被拦截的域名
func (s *Stats) RecordBlockedDomain(domain string) {
	s.blockedDomains.RecordBlock(domain)
}

// RecordCacheEviction 记录缓存驱逐事件
func (s *Stats) RecordCacheEviction(evictionCount int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(s.lastCheckTime).Seconds()

	// 如果距离上次检查超过1分钟，重置计数
	if elapsed >= 60 {
		s.lastEvictionCount = evictionCount
		s.lastCheckTime = now
	}
}

// GetEvictionsPerMinute 获取每分钟的驱逐率
func (s *Stats) GetEvictionsPerMinute(currentEvictionCount int64) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	elapsed := now.Sub(s.lastCheckTime).Seconds()

	if elapsed < 1 {
		return 0.0
	}

	evictionDelta := currentEvictionCount - s.lastEvictionCount
	evictionsPerMin := float64(evictionDelta) / elapsed * 60

	return evictionsPerMin
}

// DomainCount 用于排序的结构体
type DomainCount struct {
	Domain string `json:"Domain"`
	Count  int64  `json:"Count"`
}

// GetTopDomains 获取查询次数最多的域名
func (s *Stats) GetTopDomains(limit int) []DomainCount {
	return s.hotDomains.GetTopDomains(limit)
}

// GetTopBlockedDomains 获取被拦截最多的域名
func (s *Stats) GetTopBlockedDomains(limit int) []BlockedDomainCount {
	return s.blockedDomains.GetTopBlockedDomains(limit)
}

// Reset 重置统计
func (s *Stats) Reset() {
	atomic.StoreInt64(&s.queries, 0)
	atomic.StoreInt64(&s.effectiveQueries, 0)
	atomic.StoreInt64(&s.cacheHits, 0)
	atomic.StoreInt64(&s.cacheMisses, 0)
	atomic.StoreInt64(&s.cacheStaleRefresh, 0)
	atomic.StoreInt64(&s.upstreamFailures, 0)
	atomic.StoreInt64(&s.pingSuccesses, 0)
	atomic.StoreInt64(&s.pingFailures, 0)
	atomic.StoreInt64(&s.totalRTT, 0)

	s.mu.Lock()
	s.failedNodes = make(map[string]int64)
	s.mu.Unlock()

	s.hotDomains.Reset()
	s.blockedDomains.Reset()
	s.generalStatsTracker.Reset()
}

// Stop 停止统计服务
func (s *Stats) Stop() {
	// 先停止缓存刷新协程
	close(s.stopChan)

	// 再停止其他追踪器
	s.hotDomains.Stop()
	s.blockedDomains.Stop()
	s.generalStatsTracker.Stop()
}
