package cache

import (
	"sync/atomic"
)

// GetCurrentEntries 获取当前缓存的条目数（仅计算 rawCache）
func (c *Cache) GetCurrentEntries() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rawCache.Len()
}

// GetEvictions 获取驱逐计数
func (c *Cache) GetEvictions() int64 {
	return atomic.LoadInt64(&c.evictions)
}

// GetMemoryUsagePercent 获取当前内存使用百分比
func (c *Cache) GetMemoryUsagePercent() float64 {
	if c.maxEntries == 0 {
		return 0
	}
	return float64(c.GetCurrentEntries()) / float64(c.maxEntries)
}

// getMemoryUsagePercentLocked 获取当前内存使用百分比（已持有锁）
func (c *Cache) getMemoryUsagePercentLocked() float64 {
	if c.maxEntries == 0 {
		return 0
	}
	return float64(c.rawCache.Len()) / float64(c.maxEntries)
}

// GetExpiredEntries 统计已过期的条目数（快速估算）
// 使用过期堆快速估算，而不是遍历所有条目
// 时间复杂度从 O(n) 降低到 O(k)，其中 k 是堆中已过期的条目数
func (c *Cache) GetExpiredEntries() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := timeNow().Unix()
	count := 0

	// 遍历堆中的条目，统计已过期的数量
	// 由于堆是按过期时间排序的，一旦遇到未过期的条目就可以停止
	for _, entry := range c.expiredHeap {
		if entry.expiry <= now {
			count++
		} else {
			// 堆中后续条目都未过期，可以停止
			break
		}
	}

	return count
}

// GetProtectedEntries 统计受保护的条目数
func (c *Cache) GetProtectedEntries() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.prefetcher == nil || !c.config.ProtectPrefetchDomains {
		return 0
	}

	count := 0
	entries := c.getRawCacheKeysSnapshot()
	for _, key := range entries {
		domain := c.extractDomain(key)
		if c.isProtectedDomain(domain) {
			count++
		}
	}
	return count
}

// RecordHit 记录缓存命中
func (c *Cache) RecordHit() {
	atomic.AddInt64(&c.hits, 1)
}

// RecordMiss 记录缓存未命中
func (c *Cache) RecordMiss() {
	atomic.AddInt64(&c.misses, 1)
}

// GetStats 获取缓存统计
func (c *Cache) GetStats() (hits, misses int64) {
	hits = atomic.LoadInt64(&c.hits)
	misses = atomic.LoadInt64(&c.misses)
	return
}
