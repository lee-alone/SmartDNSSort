package cache

import (
	"sync"
	"time"

	"smartdnssort/config"
)

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

	// 清理统计（用于监控）
	lastCleanupTime     time.Time     // 最后一次清理的时间
	lastCleanupCount    int           // 最后一次清理清理的条目数
	lastCleanupDuration time.Duration // 最后一次清理的耗时

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

// timeNow 返回当前时间（便于测试 mock）
func timeNow() time.Time {
	return time.Now()
}
