package cache

import "time"

// BlockedCacheEntry 拦截缓存项
type BlockedCacheEntry struct {
	BlockType string    // 拦截类型 (nxdomain, refused, zero_ip)
	Rule      string    // 命中的规则
	ExpiredAt time.Time // 过期时间
}

// IsExpired 检查拦截缓存是否过期
func (e *BlockedCacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiredAt)
}

// AllowedCacheEntry 白名单缓存项
type AllowedCacheEntry struct {
	ExpiredAt time.Time // 过期时间
}

// IsExpired 检查白名单缓存是否过期
func (e *AllowedCacheEntry) IsExpired() bool {
	return time.Now().After(e.ExpiredAt)
}

// GetBlocked 获取拦截缓存
func (c *Cache) GetBlocked(domain string) (*BlockedCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.blockedCache[domain]
	if !exists {
		return nil, false
	}

	if entry.IsExpired() {
		return nil, false
	}

	return entry, true
}

// SetBlocked 设置拦截缓存
func (c *Cache) SetBlocked(domain string, entry *BlockedCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.blockedCache[domain] = entry
}

// GetAllowed 获取白名单缓存
func (c *Cache) GetAllowed(domain string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 白名单缓存通常不区分 qtype，因为域名是安全的
	// 但为了统一 key 格式，我们可能需要约定一个 qtype 或者只用域名
	// 这里为了简单，我们只用域名作为 key (需要修改 cacheKey 或者 map 定义)
	// 考虑到 cacheKey 包含 qtype，我们这里简单起见，假设白名单对所有 qtype 有效
	// 但我们的 map 是 string -> *AllowedCacheEntry
	// 如果我们想对所有 qtype 生效，key 应该只包含域名。
	// 但是 cacheKey 是 domain + "#" + qtype
	// 让我们在 Cache 结构体中定义 allowedCache 为 map[string]*AllowedCacheEntry，其中 key 只是 domain

	entry, exists := c.allowedCache[domain]
	if !exists {
		return false
	}

	if entry.IsExpired() {
		return false
	}

	return true
}

// SetAllowed 设置白名单缓存
func (c *Cache) SetAllowed(domain string, entry *AllowedCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.allowedCache[domain] = entry
}

// cleanAdBlockCaches 清理过期的 AdBlock 缓存
// 这个方法应该被 CleanExpired 调用 (在持有锁的情况下)
// 或者我们提供一个公开的方法
func (c *Cache) cleanAdBlockCaches() {
	// 注意：调用此方法前必须持有锁

	// 清理拦截缓存
	for key, entry := range c.blockedCache {
		if entry.IsExpired() {
			delete(c.blockedCache, key)
		}
	}

	// 清理白名单缓存
	for key, entry := range c.allowedCache {
		if entry.IsExpired() {
			delete(c.allowedCache, key)
		}
	}
}
