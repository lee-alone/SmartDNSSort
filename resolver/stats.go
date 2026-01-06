package resolver

import (
	"sync"
	"sync/atomic"
	"time"
)

// Stats 统计模块
type Stats struct {
	mu             sync.RWMutex
	totalQueries   int64
	successQueries int64
	failedQueries  int64
	totalLatency   int64 // 纳秒
	minLatency     int64 // 纳秒
	maxLatency     int64 // 纳秒
	cacheHits      int64
	cacheMisses    int64
	startTime      time.Time
	lastResetTime  time.Time
}

// NewStats 创建新的统计模块
func NewStats() *Stats {
	now := time.Now()
	return &Stats{
		startTime:     now,
		lastResetTime: now,
		minLatency:    int64(^uint64(0) >> 1), // 最大 int64
		maxLatency:    0,
	}
}

// RecordQuery 记录查询
func (s *Stats) RecordQuery(latency time.Duration, success bool) {
	latencyNs := latency.Nanoseconds()

	atomic.AddInt64(&s.totalQueries, 1)

	if success {
		atomic.AddInt64(&s.successQueries, 1)
	} else {
		atomic.AddInt64(&s.failedQueries, 1)
	}

	// 更新延迟统计
	atomic.AddInt64(&s.totalLatency, latencyNs)

	// 更新最小延迟
	for {
		currentMin := atomic.LoadInt64(&s.minLatency)
		if latencyNs >= currentMin {
			break
		}
		if atomic.CompareAndSwapInt64(&s.minLatency, currentMin, latencyNs) {
			break
		}
	}

	// 更新最大延迟
	for {
		currentMax := atomic.LoadInt64(&s.maxLatency)
		if latencyNs <= currentMax {
			break
		}
		if atomic.CompareAndSwapInt64(&s.maxLatency, currentMax, latencyNs) {
			break
		}
	}
}

// RecordCacheHit 记录缓存命中
func (s *Stats) RecordCacheHit() {
	atomic.AddInt64(&s.cacheHits, 1)
}

// RecordCacheMiss 记录缓存未命中
func (s *Stats) RecordCacheMiss() {
	atomic.AddInt64(&s.cacheMisses, 1)
}

// GetStats 获取统计信息
func (s *Stats) GetStats() map[string]interface{} {
	totalQueries := atomic.LoadInt64(&s.totalQueries)
	successQueries := atomic.LoadInt64(&s.successQueries)
	failedQueries := atomic.LoadInt64(&s.failedQueries)
	totalLatency := atomic.LoadInt64(&s.totalLatency)
	minLatency := atomic.LoadInt64(&s.minLatency)
	maxLatency := atomic.LoadInt64(&s.maxLatency)
	cacheHits := atomic.LoadInt64(&s.cacheHits)
	cacheMisses := atomic.LoadInt64(&s.cacheMisses)

	// 计算平均延迟
	var avgLatency float64
	if totalQueries > 0 {
		avgLatency = float64(totalLatency) / float64(totalQueries) / 1e6 // 转换为毫秒
	}

	// 计算成功率
	var successRate float64
	if totalQueries > 0 {
		successRate = float64(successQueries) / float64(totalQueries) * 100
	}

	// 计算缓存命中率
	var cacheHitRate float64
	totalCacheAccess := cacheHits + cacheMisses
	if totalCacheAccess > 0 {
		cacheHitRate = float64(cacheHits) / float64(totalCacheAccess) * 100
	}

	// 计算运行时间
	uptime := time.Since(s.startTime)

	// 处理最小延迟（如果没有查询，设置为 0）
	minLatencyMs := float64(0)
	if totalQueries > 0 && minLatency != int64(^uint64(0)>>1) {
		minLatencyMs = float64(minLatency) / 1e6
	}

	return map[string]interface{}{
		"total_queries":   totalQueries,
		"success_queries": successQueries,
		"failed_queries":  failedQueries,
		"success_rate":    successRate,
		"avg_latency_ms":  avgLatency,
		"min_latency_ms":  minLatencyMs,
		"max_latency_ms":  float64(maxLatency) / 1e6,
		"cache_hits":      cacheHits,
		"cache_misses":    cacheMisses,
		"cache_hit_rate":  cacheHitRate,
		"uptime":          uptime.String(),
		"uptime_seconds":  uptime.Seconds(),
	}
}

// Reset 重置统计信息
func (s *Stats) Reset() {
	atomic.StoreInt64(&s.totalQueries, 0)
	atomic.StoreInt64(&s.successQueries, 0)
	atomic.StoreInt64(&s.failedQueries, 0)
	atomic.StoreInt64(&s.totalLatency, 0)
	atomic.StoreInt64(&s.minLatency, int64(^uint64(0)>>1))
	atomic.StoreInt64(&s.maxLatency, 0)
	atomic.StoreInt64(&s.cacheHits, 0)
	atomic.StoreInt64(&s.cacheMisses, 0)

	s.mu.Lock()
	s.lastResetTime = time.Now()
	s.mu.Unlock()
}

// GetTotalQueries 获取总查询数
func (s *Stats) GetTotalQueries() int64 {
	return atomic.LoadInt64(&s.totalQueries)
}

// GetSuccessQueries 获取成功查询数
func (s *Stats) GetSuccessQueries() int64 {
	return atomic.LoadInt64(&s.successQueries)
}

// GetFailedQueries 获取失败查询数
func (s *Stats) GetFailedQueries() int64 {
	return atomic.LoadInt64(&s.failedQueries)
}

// GetCacheHits 获取缓存命中数
func (s *Stats) GetCacheHits() int64 {
	return atomic.LoadInt64(&s.cacheHits)
}

// GetCacheMisses 获取缓存未命中数
func (s *Stats) GetCacheMisses() int64 {
	return atomic.LoadInt64(&s.cacheMisses)
}

// GetAverageLatency 获取平均延迟（毫秒）
func (s *Stats) GetAverageLatency() float64 {
	totalQueries := atomic.LoadInt64(&s.totalQueries)
	if totalQueries == 0 {
		return 0
	}
	totalLatency := atomic.LoadInt64(&s.totalLatency)
	return float64(totalLatency) / float64(totalQueries) / 1e6
}

// GetSuccessRate 获取成功率（百分比）
func (s *Stats) GetSuccessRate() float64 {
	totalQueries := atomic.LoadInt64(&s.totalQueries)
	if totalQueries == 0 {
		return 0
	}
	successQueries := atomic.LoadInt64(&s.successQueries)
	return float64(successQueries) / float64(totalQueries) * 100
}
