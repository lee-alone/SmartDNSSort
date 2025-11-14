package cache

import (
	"sync"
	"time"
)

// CacheEntry 缓存项
type CacheEntry struct {
	IPs       []string  // 排序后的 IP 列表
	RTTs      []int     // 对应的 RTT（毫秒）
	Timestamp time.Time // 缓存时间
	TTL       int       // TTL（秒）
}

// IsExpired 检查缓存是否过期
func (e *CacheEntry) IsExpired() bool {
	return time.Since(e.Timestamp).Seconds() > float64(e.TTL)
}

// Cache DNS 缓存管理
type Cache struct {
	mu     sync.RWMutex
	data   map[string]*CacheEntry
	hits   int64
	misses int64
}

// NewCache 创建新的缓存实例
func NewCache() *Cache {
	return &Cache{
		data: make(map[string]*CacheEntry),
	}
}

// cacheKey 生成缓存键，包含查询类型
func cacheKey(domain string, qtype uint16) string {
	return domain + "#" + string(rune(qtype))
}

// Get 获取缓存
func (c *Cache) Get(domain string, qtype uint16) (*CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey(domain, qtype)
	entry, exists := c.data[key]
	if !exists {
		return nil, false
	}

	if entry.IsExpired() {
		return nil, false
	}

	return entry, true
}

// Set 设置缓存
func (c *Cache) Set(domain string, qtype uint16, entry *CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := cacheKey(domain, qtype)
	c.data[key] = entry
}

// Record 记录缓存命中
func (c *Cache) RecordHit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits++
}

// RecordMiss 记录缓存未命中
func (c *Cache) RecordMiss() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.misses++
}

// GetStats 获取缓存统计
func (c *Cache) GetStats() (hits, misses int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.misses
}

// Clear 清空缓存
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]*CacheEntry)
}

// CleanExpired 清理过期项
func (c *Cache) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for domain, entry := range c.data {
		if entry.IsExpired() {
			delete(c.data, domain)
		}
	}
}
