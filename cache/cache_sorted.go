package cache

import "time"

// GetSorted 获取排序后的缓存（仅限未过期的）
func (c *Cache) GetSorted(domain string, qtype uint16) (*SortedCacheEntry, bool) {
	return c.GetSortedWithStale(domain, qtype, false)
}

// GetSortedWithStale 获取排序后的缓存，支持获取过期（Stale）数据
// 当 rawCache 命中 Stale 时，应调用此方法以获取之前的排序结果，而不是回退到原始顺序
func (c *Cache) GetSortedWithStale(domain string, qtype uint16, includeStale bool) (*SortedCacheEntry, bool) {
	key := cacheKey(domain, qtype)
	value, exists := c.sortedCache.Get(key)
	if !exists {
		return nil, false
	}

	entry, ok := value.(*SortedCacheEntry)
	if !ok {
		return nil, false
	}

	// 如果不包含 Stale 数据且已过期，返回 false
	if !includeStale && entry.IsExpired() {
		return nil, false
	}

	// 记录 IP 访问热度（只记录第一个 IP，即最优 IP）
	if c.ipPoolUpdater != nil && len(entry.IPs) > 0 {
		c.ipPoolUpdater.RecordAccess(entry.IPs[0], domain)
	}

	return entry, true
}

// SetSorted 设置排序后的缓存
// 注意：sortedCache 内部已实现线程安全，无需全局锁
func (c *Cache) SetSorted(domain string, qtype uint16, entry *SortedCacheEntry) {
	key := cacheKey(domain, qtype)

	// 获取旧的 IP 列表，用于更新 IP 池引用计数
	var oldIPs []string
	if oldEntry, exists := c.sortedCache.Get(key); exists {
		if oldSorted, ok := oldEntry.(*SortedCacheEntry); ok {
			oldIPs = oldSorted.IPs
		}
	}

	// 设置新的排序缓存
	c.sortedCache.Set(key, entry)

	// 更新 IP 池引用计数
	if c.ipPoolUpdater != nil && entry != nil {
		c.ipPoolUpdater.UpdateDomainIPs(oldIPs, entry.IPs, domain)
	}
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
// 增加策略：与主缓存一致，仅清理超过 ancientLimit 的陈旧排序
func (c *Cache) cleanExpiredSortedCache(ancientLimit int64) {
	now := timeNow()
	c.sortedCache.CleanExpired(func(value any) bool {
		if entry, ok := value.(*SortedCacheEntry); ok {
			// 只有超过 AncientLimit 的才真正清理
			expiresAt := entry.Timestamp.Add(time.Duration(entry.TTL) * time.Second)
			return now.After(expiresAt.Add(time.Duration(ancientLimit) * time.Second))
		}
		return false
	})
}
