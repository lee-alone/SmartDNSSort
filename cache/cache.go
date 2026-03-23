package cache

import (
	"sync"
	"sync/atomic"
	"time"

	"smartdnssort/config"
)

// PrefetchChecker 定义了检查域名是否为热点域名的接口
// dnsserver 包中的 Prefetcher 将实现此接口
type PrefetchChecker interface {
	IsTopDomain(domain string) bool
}

// IPPoolUpdater 定义了 IP 池更新接口
// 用于在域名 IP 列表变化时更新 IP 池的引用计数
type IPPoolUpdater interface {
	UpdateDomainIPs(oldIPs, newIPs []string, domain string)
	RecordAccess(ip, domain string)
}

// Cache DNS 缓存管理器
type Cache struct {
	mu sync.RWMutex // 保护以下字段

	// 缓存数据
	config       *config.CacheConfig           // 缓存配置
	maxEntries   int                           // 最大条目数
	rawCache     *ShardedCache                 // 原始缓存（使用分片 LRU 管理）
	sortedCache  *ShardedLRUCache              // 排序缓存（使用分片 LRU 管理，消除全局锁瓶颈）
	sortingState map[string]*SortingState      // 排序任务状态
	errorCache   *LRUCache                     // 错误缓存（使用 LRU 管理）
	blockedCache map[string]*BlockedCacheEntry // 拦截缓存
	allowedCache map[string]*AllowedCacheEntry // 白名单缓存
	msgCache     *LRUCache                     // DNSSEC 消息缓存（存储完整的 DNS 响应）

	// 统计和其他字段
	prefetcher      PrefetchChecker        // Prefetcher 实例，用于热点域名保护
	ipPoolUpdater   IPPoolUpdater          // IP 池更新器，用于维护全局 IP 资源
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

	// 监控指标
	heapChannelFullCount int64 // channel 满的次数（原子操作）

	// 过期统计（用于UI展示）
	actualExpiredCount int64 // 实际过期条目计数（缓存中存在且已过期的条目）
	staleHeapCount     int64 // 幽灵索引计数（堆中存在但缓存中不存在的索引）
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
		rawCache:        NewShardedCache(maxEntries, 64),    // 使用分片缓存获得 10x+ 性能提升
		sortedCache:     NewShardedLRUCache(maxEntries, 64), // 使用分片 LRU 缓存，消除全局锁瓶颈
		sortingState:    make(map[string]*SortingState),
		errorCache:      NewLRUCache(maxEntries),
		blockedCache:    make(map[string]*BlockedCacheEntry),
		allowedCache:    make(map[string]*AllowedCacheEntry),
		msgCache:        NewLRUCache(msgCacheEntries),
		recentlyBlocked: NewRecentlyBlockedTracker(),
		expiredHeap:     make(expireHeap, 0),
		addHeapChan:     make(chan expireEntry, 10000), // 增加缓冲至 10000，消除突发流量下的阻塞点
		stopHeapChan:    make(chan struct{}),
	}

	// 设置 sortedCache 的驱逐回调，用于更新 IP 池引用计数
	c.sortedCache.SetOnEvict(func(key string, value any) {
		if c.ipPoolUpdater != nil {
			if entry, ok := value.(*SortedCacheEntry); ok {
				// 解析 key 获取域名
				domain, _ := parseCacheKey(key)
				if domain != "" {
					// 通知 IP 池移除该域名的 IP 引用
					c.ipPoolUpdater.UpdateDomainIPs(entry.IPs, nil, domain)
				}
			}
		}
	})

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

// SetIPPoolUpdater 设置 IP 池更新器，用于维护全局 IP 资源
func (c *Cache) SetIPPoolUpdater(u IPPoolUpdater) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ipPoolUpdater = u
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

	// 清空过期堆和统计
	c.expiredHeap = make(expireHeap, 0)
	c.actualExpiredCount = 0
	c.staleHeapCount = 0
	atomic.StoreInt64(&c.heapChannelFullCount, 0)
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

	// 关闭 ShardedLRUCache 的异步处理
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

// GetHeapChannelFullCount 获取 channel 满的次数（用于监控）
func (c *Cache) GetHeapChannelFullCount() int64 {
	return atomic.LoadInt64(&c.heapChannelFullCount)
}

// GetExpiredHeapSize 获取过期堆的大小（用于测试）
// 此方法使用读锁保护，确保线程安全
func (c *Cache) GetExpiredHeapSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.expiredHeap)
}

// timeNow 返回当前时间（便于测试 mock）
func timeNow() time.Time {
	return time.Now()
}
