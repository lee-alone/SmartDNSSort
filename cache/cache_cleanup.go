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

	// MaxStaleHeapCleanupSize 单次清理幽灵索引的最大数量
	// 幽灵索引：堆中存在但缓存中不存在的索引
	MaxStaleHeapCleanupSize = 500

	// AncientLimitSeconds 古老数据的界限（秒）
	// 改为动态策略：低压力 ( usage < 0.5 ) 下延长至 24 小时，中压力 ( 0.5-0.8 ) 下保持 2 小时
	AncientLimitLowPressure  = 86400 // 24 小时
	AncientLimitMidPressure  = 7200  // 2 小时
	AncientLimitHighPressure = 0     // 立即清理
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
// 6. 两阶段清理：先清理幽灵索引，再清理实际过期条目
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

	// 动态调整过期界限 (Dynamic Ancient Limit)
	// 根据压力设置保留策略，实现在内存富余时“能留尽留”，在压力大时“断舍离”
	var ancientLimit int64
	if usage < 0.5 {
		ancientLimit = AncientLimitLowPressure
	} else if usage < pressureThreshold {
		ancientLimit = AncientLimitMidPressure
	} else {
		ancientLimit = AncientLimitHighPressure
	}

	// 批量清理限制
	startTime := timeNow()
	cleanedCount := 0
	staleCleanedCount := 0 // 幽灵索引清理计数

	// 第一阶段：清理幽灵索引（缓存中不存在的索引）
	// 无论压力状态如何，都优先清理幽灵索引，避免堆无限增长
	for len(c.expiredHeap) > 0 && staleCleanedCount < MaxStaleHeapCleanupSize {
		// 检查是否超过清理限制
		if timeNow().Sub(startTime) > MaxCleanupDuration {
			// 本次清理已耗时过长，停止以避免阻塞 DNS 查询
			break
		}

		entry := c.expiredHeap[0]

		// 获取缓存中该键的当前真实状态（使用 GetNoUpdate 避免干扰 LRU 统计）
		val, exists := c.rawCache.GetNoUpdate(entry.key)
		if !exists {
			// 缓存中已不存在（幽灵索引），移除堆中的废弃索引
			heap.Pop(&c.expiredHeap)
			staleCleanedCount++
			// 如果这个幽灵索引是过期的，需要递减 actualExpiredCount
			if entry.expiry <= now {
				c.actualExpiredCount--
			}
			continue
		}

		currentEntry, ok := val.(*RawCacheEntry)
		if !ok {
			heap.Pop(&c.expiredHeap)
			staleCleanedCount++
			// 如果这个幽灵索引是过期的，需要递减 actualExpiredCount
			if entry.expiry <= now {
				c.actualExpiredCount--
			}
			continue
		}

		// 版本号检查：如果堆中的版本号与缓存中的不一致，说明这是一个陈旧索引
		if currentEntry.QueryVersion != entry.queryVersion {
			heap.Pop(&c.expiredHeap)
			staleCleanedCount++
			// 如果这个陈旧索引是过期的，需要递减 actualExpiredCount
			if entry.expiry <= now {
				c.actualExpiredCount--
			}
			continue
		}

		// 如果索引有效（缓存中存在且版本号匹配），跳出第一阶段
		break
	}

	// 更新幽灵索引计数
	c.staleHeapCount = int64(len(c.expiredHeap))

	// 第二阶段：清理实际过期条目
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
			// 缓存中已不存在（幽灵索引），移除堆中的废弃索引
			heap.Pop(&c.expiredHeap)
			continue
		}

		currentEntry, ok := val.(*RawCacheEntry)
		if !ok {
			heap.Pop(&c.expiredHeap)
			continue
		}

		// 版本号检查：如果堆中的版本号与缓存中的不一致，说明这是一个陈旧索引
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
			c.actualExpiredCount-- // 增量更新：删除时递减
			cleanedCount++         // 增加清理计数
		} else {
			break
		}
	}

	// 重新计算实际过期条目计数（只统计堆中剩余的有效过期条目）
	// 使用增量更新后，这里只需要处理可能的新增过期条目
	c.recalculateActualExpiredCount(now)

	// 记录清理统计信息
	c.lastCleanupTime = timeNow()
	c.lastCleanupCount = cleanedCount
	c.lastCleanupDuration = timeNow().Sub(startTime)

	// 清理辅助缓存（排序、错误等）
	c.cleanAuxiliaryCaches(ancientLimit)
}

// recalculateActualExpiredCount 重新计算实际过期条目计数
// 遍历堆中剩余条目，统计缓存中存在且已过期的条目数
// 注意：由于使用了增量更新，这里只需要处理可能的新增过期条目
func (c *Cache) recalculateActualExpiredCount(now int64) {
	count := int64(0)
	for _, entry := range c.expiredHeap {
		if entry.expiry <= now {
			// 检查缓存中是否存在且版本号匹配
			if val, exists := c.rawCache.GetNoUpdate(entry.key); exists {
				if currentEntry, ok := val.(*RawCacheEntry); ok {
					if currentEntry.QueryVersion == entry.queryVersion {
						count++
					}
				}
			}
		} else {
			// 堆是按过期时间排序的，遇到未过期的就可以停止
			break
		}
	}
	c.actualExpiredCount = count
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

// cleanAuxiliaryCaches 清理非核心缓存（sorted, sorting, error, msgCache）
// 由于排序缓存和错误缓存现在由 LRUCache 管理，我们只需清理过期条目
func (c *Cache) cleanAuxiliaryCaches(ancientLimit int64) {
	// 清理过期的排序缓存
	c.cleanExpiredSortedCache(ancientLimit)
	// 清理过期的错误缓存
	c.cleanExpiredErrorCache()
	// 清理完成的排序任务
	c.cleanCompletedSortingStates()

	// 清理过期的 DNSSEC 消息缓存
	// 避免冷门域名的 DNSSEC 大包数据永久驻留内存
	c.cleanExpiredMsgCache()

	// 调用 adblock_cache.go 中的清理方法
	c.cleanAdBlockCaches()
}

// cleanExpiredMsgCache 清理过期的 DNSSEC 消息缓存
// 主动遍历清理，确保冷门域名的过期 DNSSEC 数据能被及时释放
func (c *Cache) cleanExpiredMsgCache() {
	if c.msgCache == nil {
		return
	}

	// 使用 LRUCache 的 CleanExpired 方法，传入过期判断函数
	expiredCount := c.msgCache.CleanExpired(func(value any) bool {
		entry, ok := value.(*DNSSECCacheEntry)
		if !ok {
			return true // 类型不匹配，视为过期
		}
		return entry.IsExpired()
	})

	// 更新驱逐统计（如果有过期条目）
	if expiredCount > 0 {
		c.mu.Lock()
		c.evictions += int64(expiredCount)
		c.mu.Unlock()
	}
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
		// channel 满，记录监控指标（原子操作，无需锁）
		atomic.AddInt64(&c.heapChannelFullCount, 1)
	}
}
