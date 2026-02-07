package stats

import (
	"runtime"
	"smartdnssort/config"
	"smartdnssort/logger"
	"sync"
	"sync/atomic"
	"time"

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
	bucketCount := (cfg.GeneralStatsRetentionDays * 24 * 60) / cfg.GeneralStatsBucketMinutes
	if bucketCount < 1 {
		bucketCount = 1
	}

	generalStatsTracker := NewGeneralStatsTracker(
		time.Duration(cfg.GeneralStatsBucketMinutes)*time.Minute,
		bucketCount,
	)

	return &Stats{
		failedNodes:         make(map[string]int64),
		hotDomains:          NewHotDomainsTracker(cfg),
		blockedDomains:      NewBlockedDomainsTracker(cfg),
		generalStatsTracker: generalStatsTracker,
		startTime:           time.Now(),
		lastCheckTime:       time.Now(),
	}
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
func (s *Stats) IncUpstreamFailures() {
	atomic.AddInt64(&s.upstreamFailures, 1)
	s.generalStatsTracker.RecordUpstreamFailure()
}

// IncPingSuccesses 增加 ping 成功计数
func (s *Stats) IncPingSuccesses() {
	atomic.AddInt64(&s.pingSuccesses, 1)
}

// IncPingFailures 增加 ping 失败计数
func (s *Stats) IncPingFailures() {
	atomic.AddInt64(&s.pingFailures, 1)
}

// AddRTT 增加总 RTT
func (s *Stats) AddRTT(rtt int64) {
	atomic.AddInt64(&s.totalRTT, rtt)
}

// RecordFailedNode 记录失败的节点
func (s *Stats) RecordFailedNode(node string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failedNodes[node]++
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
		"cache_success_rate":  cacheSuccessRate,
		"cache_stale_refresh": bucketStats["cache_stale_refresh"],
		"upstream_failures":   bucketStats["upstream_failures"],
		"time_range_days":     days,
	}
}

// GetStats 获取所有统计数据
func (s *Stats) GetStats() map[string]interface{} {
	// 1. 快速获取所有需要锁定的数据
	s.mu.RLock()
	failedNodesCopy := make(map[string]int64, len(s.failedNodes))
	for k, v := range s.failedNodes {
		failedNodesCopy[k] = v
	}
	s.mu.RUnlock() // 尽快释放锁

	// 2. 在锁之外执行耗时操作
	topDomains := s.GetTopDomains(10) // 这个函数有自己的锁
	topBlockedDomains := s.GetTopBlockedDomains(10)

	queries := atomic.LoadInt64(&s.queries)
	effectiveQueries := atomic.LoadInt64(&s.effectiveQueries)
	var hitRate float64
	if effectiveQueries > 0 {
		hits := atomic.LoadInt64(&s.cacheHits)
		hitRate = float64(hits) / float64(effectiveQueries) * 100
	}

	var avgRTT int64
	pings := atomic.LoadInt64(&s.pingSuccesses)
	if pings > 0 {
		avgRTT = atomic.LoadInt64(&s.totalRTT) / pings
	}

	// 获取系统状态 (使用 gopsutil)
	// 使用非阻塞方式获取CPU使用率，避免阻塞统计调用
	var cpuUsage []float64
	cpuUsageCh := make(chan []float64, 1)
	go func() {
		usage, err := cpu.Percent(time.Millisecond*200, false)
		if err != nil {
			logger.Warnf("无法获取 CPU 使用率: %v", err)
			cpuUsageCh <- []float64{0.0}
			return
		}
		cpuUsageCh <- usage
	}()

	// 等待CPU使用率结果，但设置超时避免长时间阻塞
	select {
	case cpuUsage = <-cpuUsageCh:
	case <-time.After(100 * time.Millisecond):
		// 超时，使用默认值
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

	return map[string]interface{}{
		"total_queries":       queries,
		"effective_queries":   effectiveQueries,
		"cache_hits":          atomic.LoadInt64(&s.cacheHits),
		"cache_misses":        atomic.LoadInt64(&s.cacheMisses),
		"cache_stale_refresh": atomic.LoadInt64(&s.cacheStaleRefresh),
		"cache_hit_rate":      hitRate,
		"upstream_failures":   atomic.LoadInt64(&s.upstreamFailures),
		"ping_successes":      pings,
		"ping_failures":       atomic.LoadInt64(&s.pingFailures),
		"average_rtt_ms":      avgRTT,
		"failed_nodes":        failedNodesCopy,
		"system_stats":        sysStats,
		"top_domains":         topDomains,
		"top_blocked_domains": topBlockedDomains,
		"uptime_seconds":      time.Since(s.startTime).Seconds(),
		"evictions_per_min":   0.0, // 占位符，由 API 处理器计算
	}
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
	s.hotDomains.Stop()
	s.blockedDomains.Stop()
	s.generalStatsTracker.Stop()
}
