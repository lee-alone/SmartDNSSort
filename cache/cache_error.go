package cache

// GetError 获取错误缓存
// 注意：errorCache 内部已实现线程安全，无需全局锁
func (c *Cache) GetError(domain string, qtype uint16) (*ErrorCacheEntry, bool) {
	key := cacheKey(domain, qtype)
	value, exists := c.errorCache.Get(key)
	if !exists {
		return nil, false
	}

	entry, ok := value.(*ErrorCacheEntry)
	if !ok {
		return nil, false
	}

	if entry.IsExpired() {
		return nil, false
	}

	return entry, true
}

// SetError 设置错误缓存
// 注意：errorCache 内部已实现线程安全，无需全局锁
func (c *Cache) SetError(domain string, qtype uint16, rcode int, ttl int) {
	key := cacheKey(domain, qtype)
	entry := &ErrorCacheEntry{
		Rcode:    rcode,
		CachedAt: timeNow(),
		TTL:      ttl,
	}
	c.errorCache.Set(key, entry)
}

// cleanExpiredErrorCache 清理过期的错误缓存
func (c *Cache) cleanExpiredErrorCache() {
	c.errorCache.CleanExpired(func(value any) bool {
		if entry, ok := value.(*ErrorCacheEntry); ok {
			return entry.IsExpired()
		}
		return false
	})
}

// cleanCompletedSortingStates 清理已完成的排序任务
func (c *Cache) cleanCompletedSortingStates() {
	keysToRemove := make([]string, 0)
	for key, state := range c.sortingState {
		if !state.InProgress {
			keysToRemove = append(keysToRemove, key)
		}
	}
	for _, key := range keysToRemove {
		delete(c.sortingState, key)
	}
}
