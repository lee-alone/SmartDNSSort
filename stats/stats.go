package stats

import (
	"sync"
	"sync/atomic"
)

// Stats 运行统计
type Stats struct {
	mu               sync.RWMutex
	queries          int64
	cacheHits        int64
	cacheMisses      int64
	upstreamFailures int64
	pingSuccesses    int64
	pingFailures     int64
	totalRTT         int64
	failedNodes      map[string]int64
}

// NewStats 创建新的统计实例
func NewStats() *Stats {
	return &Stats{
		failedNodes: make(map[string]int64),
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

// IncUpstreamFailures 增加上游失败计数
func (s *Stats) IncUpstreamFailures() {
	atomic.AddInt64(&s.upstreamFailures, 1)
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
	s.mu.RLock()
	defer s.mu.RUnlock()

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

	failedNodesCopy := make(map[string]int64)
	for k, v := range s.failedNodes {
		failedNodesCopy[k] = v
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
	}
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
	defer s.mu.Unlock()
	s.failedNodes = make(map[string]int64)
}
