package cache

import (
	"container/heap"
	"sync"
	"sync/atomic"
	"time"

	"smartdnssort/config"
)

// expireEntry 过期堆中的条目
type expireEntry struct {
	key    string
	expiry int64
}

// expireHeap 实现 container/heap.Interface
type expireHeap []expireEntry

func (h expireHeap) Len() int           { return len(h) }
func (h expireHeap) Less(i, j int) bool { return h[i].expiry < h[j].expiry }
func (h expireHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *expireHeap) Push(x interface{}) {
	*h = append(*h, x.(expireEntry))
}

func (h *expireHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// PrefetchChecker 定义了检查域名是否为热点域名的接口
// dnsserver 包中的 Prefetcher 将实现此接口
type PrefetchChecker interface {
	IsTopDomain(domain string) bool
}

// Cache DNS 缓存管理器
type Cache struct {
	mu sync.RWMutex // 保护以下字段

	// 缓存数据
	config       *config.CacheConfig           // 缓存配置
	maxEntries   int                           // 最大条目数
	rawCache     *ShardedCache                 // 原始缓存（使用分片 LRU 管理）
	sortedCache  *LRUCache                     // 排序缓存（使用 LRU 管理）
	sortingState map[string]*SortingState      // 排序任务状态
	errorCache   *LRUCache                     // 错误缓存（使用 LRU 管理）
	blockedCache map[string]*BlockedCacheEntry // 拦截缓存
	allowedCache map[string]*AllowedCacheEntry // 白名单缓存
	msgCache     *LRUCache                     // DNSSEC 消息缓存（存储完整的 DNS 响应）

	// 统计和其他字段
	prefetcher      PrefetchChecker        // Prefetcher 实例，用于热点域名保护
	recentlyBlocked RecentlyBlockedTracker // 最近被拦截的域名追踪器
	hits            int64                  // 缓存命中计数
	misses          int64                  // 缓存未命中计数
	evictions       int64                  // 驱逐计数（LRU驱逐 + 过期清理）

	// 过期数据堆（使用 container/heap）
	// 按过期时间排序，清理时只处理超过 Hard Limit 的数据
	expiredHeap expireHeap

	// 异步堆写入机制（消除 Set 路径上的全局锁）
	addHeapChan  chan expireEntry
	stopHeapChan chan struct{}
	heapWg       sync.WaitGroup

	// 持久化状态追踪
	lastSavedDirty uint64
}

// NewCache 创建新的缓存实例
func NewCache(cfg *config.CacheConfig) *Cache {
	maxEntries := cfg.CalculateMaxEntries()

	// 计算 msgCache 的最大条目数
	msgCacheEntries := 0
	if cfg.MsgCacheSizeMB > 0 {
		// 假设平均 DNS 消息 ~2KB，计算最大条目数
		msgCacheEntries = (cfg.MsgCacheSizeMB * 1024 * 1024) / 2048
		msgCacheEntries = max(msgCacheEntries, 10) // 最小 10 条
	}

	c := &Cache{
		config:          cfg,
		maxEntries:      maxEntries,
		rawCache:        NewShardedCache(maxEntries, 64), // 使用分片缓存获得 10x+ 性能提升
		sortedCache:     NewLRUCache(maxEntries),
		sortingState:    make(map[string]*SortingState),
		errorCache:      NewLRUCache(maxEntries),
		blockedCache:    make(map[string]*BlockedCacheEntry),
		allowedCache:    make(map[string]*AllowedCacheEntry),
		msgCache:        NewLRUCache(msgCacheEntries),
		recentlyBlocked: NewRecentlyBlockedTracker(),
		expiredHeap:     make(expireHeap, 0),
		addHeapChan:     make(chan expireEntry, 1000), // 缓冲 channel，避免阻塞
		stopHeapChan:    make(chan struct{}),
	}

	// 启动后台堆维护协程
	c.startHeapWorker()

	return c
}

// 启动后台堆维护协程
func (c *Cache) startHeapWorker() {
	c.heapWg.Add(1)
	go c.heapWorker()
}

// heapWorker 后台协程，负责异步维护过期堆
// 消除 Set 路径上的全局锁竞争
func (c *Cache) heapWorker() {
	defer c.heapWg.Done()

	for {
		select {
		case entry := <-c.addHeapChan:
			// 获取全局锁，添加到堆中
			c.mu.Lock()
			heap.Push(&c.expiredHeap, entry)
			c.mu.Unlock()

		case <-c.stopHeapChan:
			// 处理剩余的条目
			for {
				select {
				case entry := <-c.addHeapChan:
					c.mu.Lock()
					heap.Push(&c.expiredHeap, entry)
					c.mu.Unlock()
				default:
					return
				}
			}
		}
	}
}

// SetPrefetcher 设置 prefetcher 实例，用于解耦
func (c *Cache) SetPrefetcher(p PrefetchChecker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefetcher = p
}

// GetRecentlyBlocked 获取最近被拦截的域名追踪器
func (c *Cache) GetRecentlyBlocked() RecentlyBlockedTracker {
	return c.recentlyBlocked
}

// RecordAccess 记录缓存访问（兼容性方法）
// 在 LRUCache 中，Get 操作已经自动处理访问顺序更新，所以此方法不需要做任何事
// 保留此方法是为了兼容性，避免修改调用代码
func (c *Cache) RecordAccess(domain string, qtype uint16) {
	// LRUCache 的 Get 方法已经自动将访问的元素移动到链表头部
	// 所以这里不需要额外操作
}

// CleanExpired 清理过期缓存
// 新策略：压力驱动 + 尽量存储 (Keep as much as possible)
// 1. 获取当前内存使用率，并对比配置的压测阈值 (默认 0.9)
// 2. 高压力下 (>= 阈值)：积极清理所有已过期的数据 (entry.expiry <= now)
// 3. 低压力下 (< 阈值)：
//   - 如果开启了 KeepExpiredEntries (尽量存储)：完全不清理已过期的数据，直到内存压力升高
//   - 如果未开启 KeepExpiredEntries：仅清理超过 24 小时的极其古老的数据
//
// 4. 修复：在执行删除前检查缓存中的真实条目，防止由于域名刷新导致的新数据被旧索引误删
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

	// 堆是按过期时间排序的
	for len(c.expiredHeap) > 0 {
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

		// 情况 1 修复：由于域名被反复查询刷新，堆中可能存在同一个 key 的多个过期索引
		// 我们必须确保当前处理的这个索引确实对应缓存中已过期的数据，而不是一个正在活跃的新数据
		currentExpiry := currentEntry.AcquisitionTime.Unix() + int64(currentEntry.EffectiveTTL)
		if currentExpiry > entry.expiry {
			// 缓存中的数据比堆索引更早过期？不，这里是 cache 中的数据存活时间更长
			// 意味着堆中的 entry.expiry 是旧的，直接扔掉，等待堆中该 key 较新的索引浮上来
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
		} else {
			break
		}
	}

	// 清理辅助缓存（排序、错误等）
	c.cleanAuxiliaryCaches()
}

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

// GetExpiredEntries 统计已过期的条目数
func (c *Cache) GetExpiredEntries() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0
	// 由于 LRUCache 内部是锁定的，我们需要获取所有值并检查
	// 这里通过遍历实现（注意：这需要 LRUCache 提供迭代方法）
	// 为了简化，我们先获取所有项的快照
	entries := c.GetRawCacheSnapshot()
	for _, entry := range entries {
		if entry.IsExpired() {
			count++
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

// Clear 清空缓存
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, state := range c.sortingState {
		if state.InProgress && state.Done != nil {
			close(state.Done)
		}
	}

	c.rawCache.Clear()
	c.sortedCache.Clear()
	c.sortingState = make(map[string]*SortingState)
	c.errorCache.Clear()
	c.blockedCache = make(map[string]*BlockedCacheEntry)
	c.allowedCache = make(map[string]*AllowedCacheEntry)
	c.msgCache.Clear()
}

// Close 关闭缓存，清理资源
func (c *Cache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 关闭堆维护协程
	close(c.stopHeapChan)
	c.heapWg.Wait()

	// 关闭 ShardedCache 的异步处理
	if c.rawCache != nil {
		c.rawCache.Close()
	}

	// 关闭 LRUCache 的异步处理
	if c.sortedCache != nil {
		c.sortedCache.Close()
	}
	if c.errorCache != nil {
		c.errorCache.Close()
	}
	if c.msgCache != nil {
		c.msgCache.Close()
	}

	return nil
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

// timeNow 返回当前时间（便于测试 mock）
func timeNow() time.Time {
	return time.Now()
}

// addToExpiredHeap 将过期数据添加到堆中（异步化）
// 使用 channel 发送，避免 Set 路径上的全局锁
// 这是情况 1 的核心改进：消除高频操作上的全局锁
func (c *Cache) addToExpiredHeap(key string, expiryTime int64) {
	entry := expireEntry{
		key:    key,
		expiry: expiryTime,
	}

	// 非阻塞发送，如果 channel 满则丢弃
	// 这是可接受的，因为大多数条目会被记录
	select {
	case c.addHeapChan <- entry:
	default:
		// channel 满，丢弃此次记录
	}
}
