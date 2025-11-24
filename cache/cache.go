package cache

import (
	"smartdnssort/config"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RawCacheEntry 原始缓存项（上游 DNS 的原始响应）
type RawCacheEntry struct {
	IPs             []string  // 原始 IP 列表
	CNAME           string    // CNAME 记录（如果有）
	UpstreamTTL     uint32    // 上游 DNS 返回的原始 TTL（秒）
	AcquisitionTime time.Time // 从上游获取的时间

	// 新增LRU所需字段
	LastAccessTime time.Time // 最后访问时间
	AccessCount    int       // 访问次数统计
}

// IsExpired 检查原始缓存是否过期
func (e *RawCacheEntry) IsExpired() bool {
	elapsed := time.Since(e.AcquisitionTime).Seconds()
	return elapsed > float64(e.UpstreamTTL)
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

// ErrorCacheEntry 错误响应缓存项（用于缓存 DNS 错误响应）
type ErrorCacheEntry struct {
	Rcode    int       // DNS 错误码（SERVFAIL, REFUSED 等）
	CachedAt time.Time // 缓存时间
	TTL      int       // 缓存 TTL（秒）
}

// IsExpired 检查错误缓存是否过期
func (e *ErrorCacheEntry) IsExpired() bool {
	return time.Since(e.CachedAt).Seconds() > float64(e.TTL)
}

// PrefetchChecker 定义了检查域名是否为热点域名的接口
// dnsserver 包中的 Prefetcher 将实现此接口
type PrefetchChecker interface {
	IsTopDomain(domain string) bool
}

// Cache DNS 缓存管理器
type Cache struct {
	mu sync.RWMutex // 保护以下字段

	// 缓存数据
	rawCache     map[string]*RawCacheEntry
	sortedCache  map[string]*SortedCacheEntry
	sortingState map[string]*SortingState
	errorCache   map[string]*ErrorCacheEntry

	// 内存管理
	config     *config.CacheConfig // 缓存配置
	maxEntries int                 // 根据内存估算的最大条目数
	prefetcher PrefetchChecker     // 用于检查是否为受保护的域名

	// 统计信息（原子操作）
	hits   int64
	misses int64
}

// NewCache 创建新的缓存实例
func NewCache(cfg *config.CacheConfig) *Cache {
	return &Cache{
		config:       cfg,
		maxEntries:   cfg.CalculateMaxEntries(),
		rawCache:     make(map[string]*RawCacheEntry),
		sortedCache:  make(map[string]*SortedCacheEntry),
		sortingState: make(map[string]*SortingState),
		errorCache:   make(map[string]*ErrorCacheEntry),
	}
}

// cacheKey 生成缓存键，包含查询类型
func cacheKey(domain string, qtype uint16) string {
	return domain + "#" + string(rune(qtype))
}

// GetRaw 获取原始缓存（上游 DNS 响应）
// 注意:此方法不检查过期,调用方需要自行判断是否过期
// 即使过期也返回缓存,用于阶段三:返回旧数据+异步刷新
func (c *Cache) GetRaw(domain string, qtype uint16) (*RawCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey(domain, qtype)
	entry, exists := c.rawCache[key]
	if !exists {
		return nil, false
	}

	return entry, true
}

// SetRaw 设置原始缓存（上游 DNS 响应）
func (c *Cache) SetRaw(domain string, qtype uint16, ips []string, cname string, upstreamTTL uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)
	c.rawCache[key] = &RawCacheEntry{
		IPs:             ips,
		CNAME:           cname,
		UpstreamTTL:     upstreamTTL,
		AcquisitionTime: time.Now(),
		LastAccessTime:  time.Now(), // 初始化访问时间
	}
}

// SetPrefetcher 设置 prefetcher 实例，用于解耦
func (c *Cache) SetPrefetcher(p PrefetchChecker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefetcher = p
}

// RecordAccess 记录缓存访问，更新 LRU 时间
func (c *Cache) RecordAccess(domain string, qtype uint16) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)
	if entry, exists := c.rawCache[key]; exists {
		entry.LastAccessTime = time.Now()
		entry.AccessCount++
	}
}

// GetCurrentEntries 获取当前缓存的条目数（仅计算 rawCache）
func (c *Cache) GetCurrentEntries() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.rawCache)
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
	for _, entry := range c.rawCache {
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
	for key := range c.rawCache {
		domain := c.extractDomain(key)
		if c.isProtectedDomain(domain) {
			count++
		}
	}
	return count
}

// extractDomain 从缓存键中提取域名
func (c *Cache) extractDomain(key string) string {
	parts := strings.Split(key, "#")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// isProtectedDomain 检查域名是否受保护（例如，是热点域名）
func (c *Cache) isProtectedDomain(domain string) bool {
	if c.prefetcher == nil || !c.config.ProtectPrefetchDomains {
		return false
	}
	return c.prefetcher.IsTopDomain(domain)
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

// GetError 获取错误缓存
func (c *Cache) GetError(domain string, qtype uint16) (*ErrorCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey(domain, qtype)
	entry, exists := c.errorCache[key]
	if !exists {
		return nil, false
	}

	if entry.IsExpired() {
		return nil, false
	}

	return entry, true
}

// SetError 设置错误缓存
func (c *Cache) SetError(domain string, qtype uint16, rcode int, ttl int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)
	c.errorCache[key] = &ErrorCacheEntry{
		Rcode:    rcode,
		CachedAt: time.Now(),
		TTL:      ttl,
	}
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

	c.rawCache = make(map[string]*RawCacheEntry)
	c.sortedCache = make(map[string]*SortedCacheEntry)
	c.sortingState = make(map[string]*SortingState)
	c.errorCache = make(map[string]*ErrorCacheEntry)
}

// CleanExpired 智能清理缓存。
func (c *Cache) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxEntries == 0 || (float64(len(c.rawCache))/float64(c.maxEntries)) < c.config.EvictionThreshold {
		c.cleanAuxiliaryCaches()
		return
	}
	// 内存压力大时才淘汰（LRU会优先淘汰过期条目）
	c.evictLRU()
	c.cleanAuxiliaryCaches()
}

// evictLRU 执行 LRU 淘汰算法
func (c *Cache) evictLRU() {
	type entryMeta struct {
		key         string
		isProtected bool
		isExpired   bool
		accessTime  time.Time
	}

	entries := make([]entryMeta, 0, len(c.rawCache))
	for key, entry := range c.rawCache {
		domain := c.extractDomain(key)
		entries = append(entries, entryMeta{
			key:         key,
			isProtected: c.isProtectedDomain(domain),
			isExpired:   entry.IsExpired(),
			accessTime:  entry.LastAccessTime,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isProtected != entries[j].isProtected {
			return !entries[i].isProtected
		}
		if entries[i].isExpired != entries[j].isExpired {
			return entries[i].isExpired
		}
		return entries[i].accessTime.Before(entries[j].accessTime)
	})

	totalEntries := len(c.rawCache)
	evictCount := int(float64(totalEntries) * c.config.EvictionBatchPercent)
	if evictCount == 0 && totalEntries > 0 {
		evictCount = 1
	}

	for i := 0; i < evictCount && i < len(entries); i++ {
		c.deleteByKey(entries[i].key)
	}
}

// deleteByKey 从所有缓存中删除一个键
func (c *Cache) deleteByKey(key string) {
	delete(c.rawCache, key)
	delete(c.sortedCache, key)
	delete(c.sortingState, key)
	delete(c.errorCache, key)
}

// cleanAuxiliaryCaches 清理非核心缓存（sorted, sorting, error）
func (c *Cache) cleanAuxiliaryCaches() {
	for key, entry := range c.sortedCache {
		if entry.IsExpired() {
			delete(c.sortedCache, key)
		}
	}
	for key, state := range c.sortingState {
		if !state.InProgress {
			delete(c.sortingState, key)
		}
	}
	for key, entry := range c.errorCache {
		if entry.IsExpired() {
			delete(c.errorCache, key)
		}
	}
}
