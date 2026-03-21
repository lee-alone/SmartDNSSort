package stats

import (
	"sync"
	"sync/atomic"
	"time"
)

// GeneralStatsBucket 通用统计时间桶
type GeneralStatsBucket struct {
	timestamp         time.Time
	queries           int64
	effectiveQueries  int64
	cacheHits         int64
	cacheMisses       int64
	cacheStaleRefresh int64
	upstreamFailures  int64
}

// GeneralStatsTracker 通用统计时间桶追踪器
type GeneralStatsTracker struct {
	mu          sync.RWMutex
	buckets     []*GeneralStatsBucket
	current     int
	stopChan    chan struct{}
	bucketSize  time.Duration
	bucketCount int
}

func NewGeneralStatsTracker(bucketSize time.Duration, bucketCount int) *GeneralStatsTracker {
	if bucketCount <= 0 {
		bucketCount = 1
	}
	if bucketSize <= 0 {
		bucketSize = time.Hour
	}

	tracker := &GeneralStatsTracker{
		buckets:     make([]*GeneralStatsBucket, bucketCount),
		stopChan:    make(chan struct{}),
		bucketSize:  bucketSize,
		bucketCount: bucketCount,
	}

	// 初始化桶
	now := time.Now()
	for i := 0; i < bucketCount; i++ {
		tracker.buckets[i] = &GeneralStatsBucket{
			timestamp: now,
		}
	}

	go tracker.startRotation()
	return tracker
}

// getCurrentBucket 获取当前桶（无锁版本，调用者需确保安全）
func (t *GeneralStatsTracker) getCurrentBucket() *GeneralStatsBucket {
	return t.buckets[t.current]
}

// RecordQuery 记录查询
func (t *GeneralStatsTracker) RecordQuery() {
	bucket := t.getCurrentBucket()
	atomic.AddInt64(&bucket.queries, 1)
}

// RecordEffectiveQuery 记录有效查询
func (t *GeneralStatsTracker) RecordEffectiveQuery() {
	bucket := t.getCurrentBucket()
	atomic.AddInt64(&bucket.effectiveQueries, 1)
}

// RecordCacheHit 记录缓存命中
func (t *GeneralStatsTracker) RecordCacheHit() {
	bucket := t.getCurrentBucket()
	atomic.AddInt64(&bucket.cacheHits, 1)
}

// RecordCacheMiss 记录缓存未命中
func (t *GeneralStatsTracker) RecordCacheMiss() {
	bucket := t.getCurrentBucket()
	atomic.AddInt64(&bucket.cacheMisses, 1)
}

// RecordCacheStaleRefresh 记录缓存过期刷新
func (t *GeneralStatsTracker) RecordCacheStaleRefresh() {
	bucket := t.getCurrentBucket()
	atomic.AddInt64(&bucket.cacheStaleRefresh, 1)
}

// RecordUpstreamFailure 记录上游失败
func (t *GeneralStatsTracker) RecordUpstreamFailure() {
	bucket := t.getCurrentBucket()
	atomic.AddInt64(&bucket.upstreamFailures, 1)
}

// Aggregate 聚合指定时间范围内的数据
// 注意：此方法会复制桶数组，避免长时间持锁
func (t *GeneralStatsTracker) Aggregate(startTime time.Time) map[string]int64 {
	result := make(map[string]int64)

	// 快速获取桶数组快照
	t.mu.RLock()
	buckets := make([]*GeneralStatsBucket, len(t.buckets))
	copy(buckets, t.buckets)
	currentIdx := t.current // 记录当前桶索引
	t.mu.RUnlock()

	// 在锁外遍历和聚合
	for i, bucket := range buckets {
		// 特殊处理当前活跃桶：始终包含在内
		if i == currentIdx {
			result["queries"] += atomic.LoadInt64(&bucket.queries)
			result["effective_queries"] += atomic.LoadInt64(&bucket.effectiveQueries)
			result["cache_hits"] += atomic.LoadInt64(&bucket.cacheHits)
			result["cache_misses"] += atomic.LoadInt64(&bucket.cacheMisses)
			result["cache_stale_refresh"] += atomic.LoadInt64(&bucket.cacheStaleRefresh)
			result["upstream_failures"] += atomic.LoadInt64(&bucket.upstreamFailures)
			continue
		}

		// 其他桶使用正常的时间过滤
		if bucket.timestamp.After(startTime) || bucket.timestamp.Equal(startTime) {
			result["queries"] += atomic.LoadInt64(&bucket.queries)
			result["effective_queries"] += atomic.LoadInt64(&bucket.effectiveQueries)
			result["cache_hits"] += atomic.LoadInt64(&bucket.cacheHits)
			result["cache_misses"] += atomic.LoadInt64(&bucket.cacheMisses)
			result["cache_stale_refresh"] += atomic.LoadInt64(&bucket.cacheStaleRefresh)
			result["upstream_failures"] += atomic.LoadInt64(&bucket.upstreamFailures)
		}
	}

	return result
}

// rotateBucket 旋转时间桶
// 基于物理时钟校正，允许跳过多个桶
func (t *GeneralStatsTracker) rotateBucket() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()

	// 计算应该旋转多少个桶
	timeDiff := now.Sub(t.buckets[t.current].timestamp)
	bucketsToRotate := int(timeDiff / t.bucketSize)

	if bucketsToRotate < 1 {
		// 还没到旋转时间，跳过
		return
	}

	if bucketsToRotate > t.bucketCount {
		// 系统暂停太久，所有桶都失效了，重置
		for i := 0; i < t.bucketCount; i++ {
			bucket := t.buckets[i]
			atomic.StoreInt64(&bucket.queries, 0)
			atomic.StoreInt64(&bucket.effectiveQueries, 0)
			atomic.StoreInt64(&bucket.cacheHits, 0)
			atomic.StoreInt64(&bucket.cacheMisses, 0)
			atomic.StoreInt64(&bucket.cacheStaleRefresh, 0)
			atomic.StoreInt64(&bucket.upstreamFailures, 0)
		}
		t.current = 0
		t.buckets[0].timestamp = now
		return
	}

	// 旋转多个桶（如果跳过了多个间隔）
	for i := 0; i < bucketsToRotate; i++ {
		t.current = (t.current + 1) % t.bucketCount
		bucket := t.buckets[t.current]

		// 重置桶数据
		atomic.StoreInt64(&bucket.queries, 0)
		atomic.StoreInt64(&bucket.effectiveQueries, 0)
		atomic.StoreInt64(&bucket.cacheHits, 0)
		atomic.StoreInt64(&bucket.cacheMisses, 0)
		atomic.StoreInt64(&bucket.cacheStaleRefresh, 0)
		atomic.StoreInt64(&bucket.upstreamFailures, 0)
	}

	// 设置当前桶的时间为物理时钟对齐的时间
	alignedTime := now.Truncate(t.bucketSize)
	t.buckets[t.current].timestamp = alignedTime
}

// startRotation 启动时间桶旋转
func (t *GeneralStatsTracker) startRotation() {
	ticker := time.NewTicker(t.bucketSize / 2) // 检查频率是桶间隔的一半
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.rotateBucket()
		case <-t.stopChan:
			return
		}
	}
}

// Stop 停止追踪器
func (t *GeneralStatsTracker) Stop() {
	close(t.stopChan)
}

// Reset 重置所有统计数据
func (t *GeneralStatsTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, bucket := range t.buckets {
		atomic.StoreInt64(&bucket.queries, 0)
		atomic.StoreInt64(&bucket.effectiveQueries, 0)
		atomic.StoreInt64(&bucket.cacheHits, 0)
		atomic.StoreInt64(&bucket.cacheMisses, 0)
		atomic.StoreInt64(&bucket.cacheStaleRefresh, 0)
		atomic.StoreInt64(&bucket.upstreamFailures, 0)
	}
}
