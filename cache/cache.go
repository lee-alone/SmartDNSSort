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

// Cache DNS 缓存管理器
type Cache struct {
	mu sync.RWMutex // 保护以下字段

	// 缓存数据
	config       *config.CacheConfig           // 缓存配置
	maxEntries   int                           // 最大条目数
	rawCache     *LRUCache                     // 原始缓存（使用 LRU 管理）
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

	return &Cache{
		config:          cfg,
		maxEntries:      maxEntries,
		rawCache:        NewLRUCache(maxEntries),
		sortedCache:     NewLRUCache(maxEntries),
		sortingState:    make(map[string]*SortingState),
		errorCache:      NewLRUCache(maxEntries),
		blockedCache:    make(map[string]*BlockedCacheEntry),
		allowedCache:    make(map[string]*AllowedCacheEntry),
		msgCache:        NewLRUCache(msgCacheEntries),
		recentlyBlocked: NewRecentlyBlockedTracker(),
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
// LRUCache 自动管理容量限制，这个方法仅清理辅助缓存（排序、错误等）中的过期项
func (c *Cache) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
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
