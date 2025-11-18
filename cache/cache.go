package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

// RawCacheEntry 原始缓存项（上游 DNS 的原始响应）
type RawCacheEntry struct {
	IPs       []string  // 原始 IP 列表
	TTL       uint32    // 上游 DNS 返回的 TTL
	Timestamp time.Time // 缓存时间
}


// IsExpired 检查原始缓存是否过期
func (e *RawCacheEntry) IsExpired() bool {
	return time.Since(e.Timestamp).Seconds() > float64(e.TTL)
}

// SortedCacheEntry 排序后的缓存项
type SortedCacheEntry struct {
	IPs       []string  // 排序后的 IP 列表
	RTTs      []int     // 对应的 RTT（毫秒）
	Timestamp time.Time // 排序完成时间
	TTL       int       // TTL（秒）
	IsValid   bool      // 排序是否有效
}

// IsExpired 检查排序缓存是否过期
func (e *SortedCacheEntry) IsExpired() bool {
	if !e.IsValid {
		return true
	}
	return time.Since(e.Timestamp).Seconds() > float64(e.TTL)
}

// SortingState 表示某个域名的排序状态
type SortingState struct {
	InProgress bool              // 是否正在排序
	Done       chan struct{}     // 排序完成信号
	Result     *SortedCacheEntry // 排序结果
	Error      error             // 排序错误
}

// Cache DNS 缓存管理器（双层缓存：原始 + 排序）
type Cache struct {
	mu sync.RWMutex // 保护以下字段

	// 第一层：原始缓存（上游 DNS 响应）
	rawCache map[string]*RawCacheEntry

	// 第二层：排序后的缓存（排序后的 IP 列表）
	sortedCache map[string]*SortedCacheEntry

	// 第三层：排序队列状态（追踪当前正在排序的域名，防止重复）
	sortingState map[string]*SortingState

	// 统计信息（原子操作）
	hits   int64
	misses int64
}

// NewCache 创建新的缓存实例
func NewCache() *Cache {
	return &Cache{
		rawCache:     make(map[string]*RawCacheEntry),
		sortedCache:  make(map[string]*SortedCacheEntry),
		sortingState: make(map[string]*SortingState),
	}
}

// cacheKey 生成缓存键，包含查询类型
func cacheKey(domain string, qtype uint16) string {
	return domain + "#" + string(rune(qtype))
}

// GetRaw 获取原始缓存（上游 DNS 响应）
func (c *Cache) GetRaw(domain string, qtype uint16) (*RawCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey(domain, qtype)
	entry, exists := c.rawCache[key]
	if !exists {
		return nil, false
	}

	if entry.IsExpired() {
		return nil, false
	}

	return entry, true
}

// SetRaw 设置原始缓存（上游 DNS 响应）
// 返回是否设置成功（过期检查）
func (c *Cache) SetRaw(domain string, qtype uint16, ips []string, ttl uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)
	c.rawCache[key] = &RawCacheEntry{
		IPs:       ips,
		TTL:       ttl,
		Timestamp: time.Now(),
	}
}

// GetSorted 获取排序后的缓存
func (c *Cache) GetSorted(domain string, qtype uint16) (*SortedCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey(domain, qtype)
	entry, exists := c.sortedCache[key]
	if !exists {
		return nil, false
	}

	if entry.IsExpired() {
		return nil, false
	}

	return entry, true
}

// SetSorted 设置排序后的缓存
func (c *Cache) SetSorted(domain string, qtype uint16, entry *SortedCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)
	c.sortedCache[key] = entry
}

// GetOrStartSort 获取排序状态，如果不存在则创建新的排序任务
// 返回：排序状态、是否是新创建的
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
func (c *Cache) FinishSort(domain string, qtype uint16, result *SortedCacheEntry, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)
	if state, exists := c.sortingState[key]; exists {
		state.InProgress = false
		state.Result = result
		state.Error = err
		close(state.Done) // 发送完成信号
	}
}

// ClearSort 清理排序状态（排序任务完成后调用）
func (c *Cache) ClearSort(domain string, qtype uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)
	delete(c.sortingState, key)
}

// Record 记录缓存命中（已废弃，改用 atomic 操作）
func (c *Cache) RecordHit() {
	atomic.AddInt64(&c.hits, 1)
}

// RecordMiss 记录缓存未命中（已废弃，改用 atomic 操作）
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

    // 首先关闭所有进行中的排序任务的 Done 通道，避免 goroutine 泄漏
    for _, state := range c.sortingState {
        if state.InProgress && state.Done != nil {
            close(state.Done)
        }
    }

    c.rawCache = make(map[string]*RawCacheEntry)
    c.sortedCache = make(map[string]*SortedCacheEntry)
    c.sortingState = make(map[string]*SortingState)
}

// CleanExpired 清理过期项
func (c *Cache) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 清理过期的原始缓存
	for domain, entry := range c.rawCache {
		if entry.IsExpired() {
			delete(c.rawCache, domain)
		}
	}

	// 清理过期的排序缓存
	for domain, entry := range c.sortedCache {
		if entry.IsExpired() {
			delete(c.sortedCache, domain)
		}
	}

	// 清理已完成的排序状态（可选）
	for domain, state := range c.sortingState {
		if !state.InProgress {
			delete(c.sortingState, domain)
		}
	}
}
