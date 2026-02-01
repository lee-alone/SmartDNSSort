package cache

// GetSorted 获取排序后的缓存
// 注意：sortedCache 内部已实现线程安全，无需全局锁
func (c *Cache) GetSorted(domain string, qtype uint16) (*SortedCacheEntry, bool) {
	key := cacheKey(domain, qtype)
	value, exists := c.sortedCache.Get(key)
	if !exists {
		return nil, false
	}

	entry, ok := value.(*SortedCacheEntry)
	if !ok {
		return nil, false
	}

	if entry.IsExpired() {
		return nil, false
	}

	return entry, true
}

// SetSorted 设置排序后的缓存
// 注意：sortedCache 内部已实现线程安全，无需全局锁
func (c *Cache) SetSorted(domain string, qtype uint16, entry *SortedCacheEntry) {
	key := cacheKey(domain, qtype)
	c.sortedCache.Set(key, entry)
}

// GetOrStartSort 获取排序状态，如果不存在则创建新的排序任务
func (c *Cache) GetOrStartSort(domain string, qtype uint16) (*SortingState, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)

	if state, exists := c.sortingState[key]; exists {
		return state, false // 已存在排序任务
	}

	// 创建新的排序任务
	newState := &SortingState{
		InProgress: true,
		Done:       make(chan struct{}),
	}
	c.sortingState[key] = newState
	return newState, true // 新创建的排序任务
}

// FinishSort 标记排序任务完成
// state 参数必须是 GetOrStartSort 返回的那个对象，确保操作的是正确的任务状态
func (c *Cache) FinishSort(domain string, qtype uint16, result *SortedCacheEntry, err error, state *SortingState) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 无论该状态是否仍在 map 中（可能已被 CancelSort 移除），我们都更新这个状态
	// 这样持有该状态引用的等待者（如果有）能正确收到通知

	state.InProgress = false
	state.Result = result
	state.Error = err

	// 安全地关闭 channel，防止重复关闭
	select {
	case <-state.Done:
		// 已经关闭，不做任何事
	default:
		close(state.Done)
	}
}

// CancelSort 取消排序任务，允许重新排序
// 用于在后台收集到更多 IP 时，取消旧的排序任务并启动新的
func (c *Cache) CancelSort(domain string, qtype uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)
	if state, exists := c.sortingState[key]; exists {
		// 如果排序任务还在进行中，标记为不再进行
		if state.InProgress {
			state.InProgress = false
			// 不关闭 Done channel，因为可能有其他 goroutine 在等待
		}
		// 删除排序状态，允许创建新的排序任务
		delete(c.sortingState, key)
	}
	// 同时清除旧的排序缓存
	c.sortedCache.Delete(key)
}

// cleanExpiredSortedCache 清理过期的排序缓存
func (c *Cache) cleanExpiredSortedCache() {
	c.sortedCache.CleanExpired(func(value any) bool {
		if entry, ok := value.(*SortedCacheEntry); ok {
			return entry.IsExpired()
		}
		return false
	})
}
