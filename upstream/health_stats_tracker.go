package upstream

import (
	"sync"
	"sync/atomic"
	"time"
)

// UpstreamStatsBucket 上游统计时间桶
type UpstreamStatsBucket struct {
	timestamp time.Time
	success   int64
	failure   int64
}

// UpstreamStatsTracker 上游统计时间桶追踪器
type UpstreamStatsTracker struct {
	mu          sync.RWMutex
	address     string
	buckets     []*UpstreamStatsBucket
	current     int
	stopChan    chan struct{}
	bucketSize  time.Duration
	bucketCount int
}

func NewUpstreamStatsTracker(address string, bucketSize time.Duration, bucketCount int) *UpstreamStatsTracker {
	if bucketCount <= 0 {
		bucketCount = 1
	}

	tracker := &UpstreamStatsTracker{
		address:     address,
		buckets:     make([]*UpstreamStatsBucket, bucketCount),
		stopChan:    make(chan struct{}),
		bucketSize:  bucketSize,
		bucketCount: bucketCount,
	}

	now := time.Now()
	for i := 0; i < bucketCount; i++ {
		tracker.buckets[i] = &UpstreamStatsBucket{
			timestamp: now,
		}
	}

	go tracker.startRotation()
	return tracker
}

// RecordSuccess 记录成功
func (t *UpstreamStatsTracker) RecordSuccess() {
	bucket := t.buckets[t.current]
	atomic.AddInt64(&bucket.success, 1)
}

// RecordFailure 记录失败
func (t *UpstreamStatsTracker) RecordFailure() {
	bucket := t.buckets[t.current]
	atomic.AddInt64(&bucket.failure, 1)
}

// Aggregate 聚合指定时间范围内的数据
func (t *UpstreamStatsTracker) Aggregate(startTime time.Time) (success, failure int64) {
	t.mu.RLock()
	buckets := make([]*UpstreamStatsBucket, len(t.buckets))
	copy(buckets, t.buckets)
	t.mu.RUnlock()

	var s, f int64
	for _, bucket := range buckets {
		if bucket.timestamp.After(startTime) || bucket.timestamp.Equal(startTime) {
			s += atomic.LoadInt64(&bucket.success)
			f += atomic.LoadInt64(&bucket.failure)
		}
	}

	return s, f
}

// rotateBucket 旋转时间桶
func (t *UpstreamStatsTracker) rotateBucket() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.current = (t.current + 1) % t.bucketCount
	bucket := t.buckets[t.current]
	bucket.timestamp = time.Now()
	atomic.StoreInt64(&bucket.success, 0)
	atomic.StoreInt64(&bucket.failure, 0)
}

// startRotation 启动时间桶旋转
func (t *UpstreamStatsTracker) startRotation() {
	ticker := time.NewTicker(t.bucketSize)
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
func (t *UpstreamStatsTracker) Stop() {
	close(t.stopChan)
}

// Reset 重置所有统计数据
func (t *UpstreamStatsTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, bucket := range t.buckets {
		atomic.StoreInt64(&bucket.success, 0)
		atomic.StoreInt64(&bucket.failure, 0)
	}
}
