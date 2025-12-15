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
	mu               sync.RWMutex
	queries          int64
	cacheHits        int64
	cacheMisses      int64
	upstreamFailures int64 // 总失败计数
	pingSuccesses    int64
	pingFailures     int64
	totalRTT         int64
	failedNodes      map[string]int64

	// 新增：按上游服务器统计
	upstreamSuccess map[string]*int64
	upstreamFailure map[string]*int64

	// 新增：Hot Domains 追踪器
	hotDomains *HotDomainsTracker

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

	return &Stats{
		failedNodes:     make(map[string]int64),
		upstreamSuccess: make(map[string]*int64),
		upstreamFailure: make(map[string]*int64),
		hotDomains:      NewHotDomainsTracker(cfg),
		startTime:       time.Now(),
	}
}

// IncQueries 增加查询计数
func (s *Stats) IncQueries() {
	atomic.AddInt64(&s.queries, 1)
}

// IncCacheHits 增加缓存命中计数
func (s *Stats) IncCacheHits() {
	atomic.AddInt64(&s.cacheHits, 1)
}

// IncCacheMisses 增加缓存未命中计数
func (s *Stats) IncCacheMisses() {
	atomic.AddInt64(&s.cacheMisses, 1)
}

// IncUpstreamFailures 增加上游失败计数 (总计)
func (s *Stats) IncUpstreamFailures() {
	atomic.AddInt64(&s.upstreamFailures, 1)
}

// getOrCreateCounter 安全地获取或创建计数器
func (s *Stats) getOrCreateCounter(server string, counterMap map[string]*int64) *int64 {
	s.mu.RLock()
	counter, ok := counterMap[server]
	s.mu.RUnlock()

	if ok {
		return counter
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// 再次检查，防止在获取写锁期间其他 goroutine 已经创建
	if counter, ok := counterMap[server]; ok {
		return counter
	}
	newCounter := int64(0)
	counterMap[server] = &newCounter
	return &newCounter
}

// IncUpstreamSuccess 增加指定上游服务器的成功计数
func (s *Stats) IncUpstreamSuccess(server string) {
	counter := s.getOrCreateCounter(server, s.upstreamSuccess)
	atomic.AddInt64(counter, 1)
}

// IncUpstreamFailure 增加指定上游服务器的失败计数
func (s *Stats) IncUpstreamFailure(server string) {
	counter := s.getOrCreateCounter(server, s.upstreamFailure)
	atomic.AddInt64(counter, 1)
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

// GetStats 获取所有统计数据
func (s *Stats) GetStats() map[string]interface{} {
	// 1. 快速获取所有需要锁定的数据
	s.mu.RLock()
	failedNodesCopy := make(map[string]int64, len(s.failedNodes))
	for k, v := range s.failedNodes {
		failedNodesCopy[k] = v
	}

	// 复制上游统计数据
	upstreamStats := make(map[string]map[string]int64)
	allUpstreams := make(map[string]bool)
	for server := range s.upstreamSuccess {
		allUpstreams[server] = true
	}
	for server := range s.upstreamFailure {
		allUpstreams[server] = true
	}

	for server := range allUpstreams {
		upstreamStats[server] = map[string]int64{
			"success": 0,
			"failure": 0,
		}
		if counter, ok := s.upstreamSuccess[server]; ok {
			upstreamStats[server]["success"] = atomic.LoadInt64(counter)
		}
		if counter, ok := s.upstreamFailure[server]; ok {
			upstreamStats[server]["failure"] = atomic.LoadInt64(counter)
		}
	}
	s.mu.RUnlock() // 尽快释放锁

	// 2. 在锁之外执行耗时操作
	topDomains := s.GetTopDomains(10) // 这个函数有自己的锁

	queries := atomic.LoadInt64(&s.queries)
	var hitRate float64
	if queries > 0 {
		hits := atomic.LoadInt64(&s.cacheHits)
		hitRate = float64(hits) / float64(queries) * 100
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
		"total_queries":     queries,
		"cache_hits":        atomic.LoadInt64(&s.cacheHits),
		"cache_misses":      atomic.LoadInt64(&s.cacheMisses),
		"cache_hit_rate":    hitRate,
		"upstream_failures": atomic.LoadInt64(&s.upstreamFailures),
		"ping_successes":    pings,
		"ping_failures":     atomic.LoadInt64(&s.pingFailures),
		"average_rtt_ms":    avgRTT,
		"failed_nodes":      failedNodesCopy,
		"upstream_stats":    upstreamStats,
		"system_stats":      sysStats,
		"top_domains":       topDomains,
		"uptime_seconds":    time.Since(s.startTime).Seconds(),
	}
}

// RecordDomainQuery 记录域名查询次数
func (s *Stats) RecordDomainQuery(domain string) {
	s.hotDomains.RecordQuery(domain)
}

// DomainCount 用于排序的结构体
type DomainCount struct {
	Domain string
	Count  int64
}

// GetTopDomains 获取查询次数最多的域名
func (s *Stats) GetTopDomains(limit int) []DomainCount {
	return s.hotDomains.GetTopDomains(limit)
}

// Reset 重置统计
func (s *Stats) Reset() {
	atomic.StoreInt64(&s.queries, 0)
	atomic.StoreInt64(&s.cacheHits, 0)
	atomic.StoreInt64(&s.cacheMisses, 0)
	atomic.StoreInt64(&s.upstreamFailures, 0)
	atomic.StoreInt64(&s.pingSuccesses, 0)
	atomic.StoreInt64(&s.pingFailures, 0)
	atomic.StoreInt64(&s.totalRTT, 0)

	s.mu.Lock()
	s.failedNodes = make(map[string]int64)
	s.upstreamSuccess = make(map[string]*int64)
	s.upstreamFailure = make(map[string]*int64)
	s.mu.Unlock()

	s.hotDomains.Reset()
}

// Stop 停止统计服务
func (s *Stats) Stop() {
	s.hotDomains.Stop()
}
