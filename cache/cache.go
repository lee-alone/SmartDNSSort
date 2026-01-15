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

	// 过期数据堆（使用 container/heap）
	// 按过期时间排序，清理时只处理超过 Hard Limit 的数据
	expiredHeap expireHeap

	// 异步堆写入机制（消除 Set 路径上的全局锁）
	addHeapChan  chan expireEntry
	stopHeapChan chan struct{}
	heapWg       sync.WaitGroup
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
// 新策略：
// 1. 使用 container/heap 精确定位过期数据
// 2. 引入 minHardLimit 保证异步刷新有足够时间窗口
// 3. Get 负责刷新：Fresh → Stale 的转化由 Get 操作处理
func (c *Cache) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 计算 Hard Limit 容忍期（秒）
	// 过期数据在达到 Hard Limit 之前会被保留，以支持 Stale-While-Revalidate (SWR)
	// 默认保留 10 分钟，或者根据配置的 MaxTTL 的一定比例
	const minHardLimit = 600
	hardLimitBuffer := int64(c.config.MaxTTLSeconds) / 4 // 允许过期后保留 MaxTTL 的 25% 时间
	if hardLimitBuffer < minHardLimit {
		hardLimitBuffer = minHardLimit
	}

	now := timeNow().Unix()

	// 堆是按 EffectiveTTL 过期时间排序的
	// 我们只删除那些 (过期时间 + 容忍期) 已经早于当前时间的数据
	for len(c.expiredHeap) > 0 {
		entry := c.expiredHeap[0]

		// 如果该条目即便算上容忍期也还没到清理时间，后续条目（过期更晚）更不用处理
		if entry.expiry+hardLimitBuffer > now {
			break
		}

		// 彻底删除数据
		c.rawCache.Delete(entry.key)

		// 弹出堆顶
		heap.Pop(&c.expiredHeap)
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

// GetMemoryUsagePercent 获取当前内存使用百分比
func (c *Cache) GetMemoryUsagePercent() float64 {
	if c.maxEntries == 0 {
		return 0
	}
	return float64(c.GetCurrentEntries()) / float64(c.maxEntries)
}

// GetExpiredEntries 统计已过期的条目数
func (c *Cache) GetExpiredEntries() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	count := 0
	// 由于 LRUCache 内部是锁定的，我们需要获取所有值并检查
	// 这里通过遍历实现（注意：这需要 LRUCache 提供迭代方法）
	// 为了简化，我们先获取所有项的快照
	entries := c.getRawCacheSnapshot()
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
