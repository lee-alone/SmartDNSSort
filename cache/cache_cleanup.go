package cache

import (
	"container/heap"
	"sync/atomic"
	"time"
)

// 清理限制常量：防止单次清理耗时过长导致 DNS 查询延迟尖峰
const (
	// MaxCleanupBatchSize 单次清理最多处理的条目数
	// 防止在大量过期条目时，单次清理耗时过长
	MaxCleanupBatchSize = 200

	// MaxCleanupDuration 单次清理最多耗时
	// 防止清理操作阻塞 DNS 查询
	MaxCleanupDuration = 10 * time.Millisecond
)

// CleanupStats 清理统计信息
type CleanupStats struct {
	LastCleanupTime     time.Time     // 最后一次清理的时间
	LastCleanupCount    int           // 最后一次清理清理的条目数
	LastCleanupDuration time.Duration // 最后一次清理的耗时
	HeapSize            int           // 当前过期堆的大小
	MaxBatchSize        int           // 单次清理的最大条目数
	MaxDuration         time.Duration // 单次清理的最大耗时
}

// CleanExpired 清理过期缓存
// 新策略：压力驱动 + 尽量存储 (Keep as much as possible)
// 1. 获取当前内存使用率，并对比配置的压测阈值 (默认 0.9)
// 2. 高压力下 (>= 阈值)：积极清理所有已过期的数据 (entry.expiry <= now)
// 3. 低压力下 (< 阈值)：
//   - 如果开启了 KeepExpiredEntries (尽量存储)：完全不清理已过期的数据，直到内存压力升高
//   - 如果未开启 KeepExpiredEntries：仅清理超过 24 小时的极其古老的数据
//
// 4. 改进：使用版本号识别陈旧索引，避免被旧的堆索引误删新数据
// 5. 批量清理限制：防止单次清理耗时过长导致 DNS 查询延迟尖峰
func (c *Cache) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	usage := c.getMemoryUsagePercentLocked()
	now := timeNow().Unix()

	// 压力阈值：优先使用用户配置，若未设置则默认 0.9
	pressureThreshold := c.config.EvictionThreshold
	if pressureThreshold <= 0 {
		pressureThreshold = 0.9
	}
	isHighPressure := usage >= pressureThreshold

	// 古老数据的界限：24 小时（86400 秒）
	const ancientLimit = 86400

	// 批量清理限制
	startTime := timeNow()
	cleanedCount := 0

	// 堆是按过期时间排序的
	for len(c.expiredHeap) > 0 {
		// 检查是否超过清理限制
		if cleanedCount >= MaxCleanupBatchSize {
			// 已清理足够多的条目，停止本次清理
			break
		}
		if timeNow().Sub(startTime) > MaxCleanupDuration {
			// 本次清理已耗时过长，停止以避免阻塞 DNS 查询
			break
		}

		entry := c.expiredHeap[0]

		// 如果域名还没过期（语义上的过期），无论如何都不删，直接跳出（因为后续的更晚过期）
		if entry.expiry > now {
			break
		}

		// 获取缓存中该键的当前真实状态（使用 GetNoUpdate 避免干扰 LRU 统计）
		val, exists := c.rawCache.GetNoUpdate(entry.key)
		if !exists {
			// 缓存中已不存在（可能已被 LRU 驱逐），移除堆中的废弃索引
			heap.Pop(&c.expiredHeap)
			continue
		}

		currentEntry, ok := val.(*RawCacheEntry)
		if !ok {
			heap.Pop(&c.expiredHeap)
			continue
		}

		// 版本号检查：如果堆中的版本号与缓存中的不一致，说明这是一个陈旧索引
		// 直接丢弃，等待堆中该 key 较新的索引浮上来
		if currentEntry.QueryVersion != entry.queryVersion {
			heap.Pop(&c.expiredHeap)
			continue
		}

		// 判定是否应当删除
		shouldDelete := false
		if isHighPressure {
			// A. 高压状态下，清理所有已过期的数据
			shouldDelete = true
		} else if !c.config.KeepExpiredEntries {
			// B. 低压状态，但未开启过保存期保护，则清理超过 24 小时的古老数据
			if entry.expiry+ancientLimit <= now {
				shouldDelete = true
			} else {
				// 未及 24 小时，保留
				break
			}
		} else {
			// C. 低压状态且开启了 KeepExpiredEntries (尽量存储)
			// 哪怕数据已经物理过期了，也保留，以供 Stale-While-Revalidate 使用
			break
		}

		if shouldDelete {
			c.rawCache.Delete(entry.key)
			atomic.AddInt64(&c.evictions, 1) // 记录驱逐
			heap.Pop(&c.expiredHeap)
			cleanedCount++ // 增加清理计数
		} else {
			break
		}
	}

	// 记录清理统计信息
	c.lastCleanupTime = timeNow()
	c.lastCleanupCount = cleanedCount
	c.lastCleanupDuration = timeNow().Sub(startTime)

	// 清理辅助缓存（排序、错误等）
	c.cleanAuxiliaryCaches()
}

// GetCleanupStats 获取清理统计信息
func (c *Cache) GetCleanupStats() CleanupStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CleanupStats{
		LastCleanupTime:     c.lastCleanupTime,
		LastCleanupCount:    c.lastCleanupCount,
		LastCleanupDuration: c.lastCleanupDuration,
		HeapSize:            len(c.expiredHeap),
		MaxBatchSize:        MaxCleanupBatchSize,
		MaxDuration:         MaxCleanupDuration,
	}
}

// cleanAuxiliaryCaches 清理非核心缓存（sorted, sorting, error）
// 由于排序缓存和错误缓存现在由 LRUCache 管理，我们只需清理过期条目
func (c *Cache) cleanAuxiliaryCaches() {
	// 清理过期的排序缓存
	c.cleanExpiredSortedCache()
	// 清理过期的错误缓存
	c.cleanExpiredErrorCache()
	// 清理完成的排序任务
	c.cleanCompletedSortingStates()

	// 调用 adblock_cache.go 中的清理方法
	c.cleanAdBlockCaches()
}

// addToExpiredHeap 将过期数据添加到堆中（异步化）
// 使用 channel 发送，避免 Set 路径上的全局锁
// 这是情况 1 的核心改进：消除高频操作上的全局锁
func (c *Cache) addToExpiredHeap(key string, expiryTime int64, queryVersion int64) {
	entry := expireEntry{
		key:          key,
		expiry:       expiryTime,
		queryVersion: queryVersion,
	}

	// 非阻塞发送，如果 channel 满则丢弃
	// 这是可接受的，因为大多数条目会被记录
	select {
	case c.addHeapChan <- entry:
	default:
		// channel 满，记录监控指标
		c.mu.Lock()
		c.heapChannelFullCount++
		c.mu.Unlock()
	}
}
