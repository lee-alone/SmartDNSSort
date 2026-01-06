package resolver

import (
	"container/list"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// CacheEntry 缓存条目
type CacheEntry struct {
	RRs       []dns.RR
	ExpiresAt time.Time
	Element   *list.Element // LRU 链表中的元素
}

// Cache DNS 缓存实现
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	lruList *list.List
	maxSize int
	expiry  bool
}

// NewCache 创建新的缓存
func NewCache(maxSize int, expiry bool) *Cache {
	return &Cache{
		entries: make(map[string]*CacheEntry),
		lruList: list.New(),
		maxSize: maxSize,
		expiry:  expiry,
	}
}

// Get 从缓存获取记录
func (c *Cache) Get(key string) ([]dns.RR, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil, false
	}

	// 检查是否过期
	if c.expiry && time.Now().After(entry.ExpiresAt) {
		return nil, false
	}

	return entry.RRs, true
}

// Set 设置缓存记录
func (c *Cache) Set(key string, rrs []dns.RR, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	expiresAt := time.Now().Add(ttl)

	// 如果条目已存在，更新它
	if entry, exists := c.entries[key]; exists {
		entry.RRs = rrs
		entry.ExpiresAt = expiresAt
		c.lruList.MoveToFront(entry.Element)
		return
	}

	// 检查缓存是否已满
	if len(c.entries) >= c.maxSize {
		c.evictLRU()
	}

	// 创建新条目
	element := c.lruList.PushFront(key)
	entry := &CacheEntry{
		RRs:       rrs,
		ExpiresAt: expiresAt,
		Element:   element,
	}
	c.entries[key] = entry
}

// Delete 删除缓存条目
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if !exists {
		return
	}

	delete(c.entries, key)
	c.lruList.Remove(entry.Element)
}

// Clear 清空缓存
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
	c.lruList = list.New()
}

// Size 获取缓存大小
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

// evictLRU 淘汰最少使用的条目
func (c *Cache) evictLRU() {
	if c.lruList.Len() == 0 {
		return
	}

	// 移除最后一个元素（最少使用）
	element := c.lruList.Back()
	if element != nil {
		key := element.Value.(string)
		delete(c.entries, key)
		c.lruList.Remove(element)
	}
}

// CleanupExpired 清理过期的条目
func (c *Cache) CleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, key)
			c.lruList.Remove(entry.Element)
		}
	}
}

// GetStats 获取缓存统计信息
func (c *Cache) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"size":     len(c.entries),
		"max_size": c.maxSize,
		"expiry":   c.expiry,
	}
}

// CacheKey 生成缓存键
func CacheKey(domain string, qtype uint16) string {
	return domain + ":" + dns.TypeToString[qtype]
}
