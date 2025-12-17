package cache

import (
	"container/list"
	"encoding/json"
	"os"
	"smartdnssort/config"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LRUCache 标准的 LRU 缓存实现
// 使用哈希表 + 双向链表实现，O(1) 时间复杂度的 Get 和 Set 操作
type LRUCache struct {
	mu       sync.RWMutex
	capacity int
	cache    map[string]*list.Element // key -> list.Element
	list     *list.List               // 双向链表，头部为最新，尾部为最旧
}

// lruNode 链表中的节点
type lruNode struct {
	key   string
	value interface{}
}

// NewLRUCache 创建一个容量限制的 LRU 缓存
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 10000 // 默认容量
	}
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Get 获取一个值，并将其移动到链表头部（标记为最新）
func (lru *LRUCache) Get(key string) (interface{}, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	elem, exists := lru.cache[key]
	if !exists {
		return nil, false
	}

	// 将访问的元素移动到链表头部（最新）
	lru.list.MoveToFront(elem)
	return elem.Value.(*lruNode).value, true
}

// Set 添加或更新一个值
// 新条目添加到链表头部，如果超过容量则删除尾部元素（最久未使用）
func (lru *LRUCache) Set(key string, value interface{}) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	// 如果 key 已存在，更新值并移到头部
	if elem, exists := lru.cache[key]; exists {
		elem.Value.(*lruNode).value = value
		lru.list.MoveToFront(elem)
		return
	}

	// 创建新节点
	node := &lruNode{key: key, value: value}
	elem := lru.list.PushFront(node)
	lru.cache[key] = elem

	// 如果超过容量，删除尾部元素（最久未使用）
	if lru.list.Len() > lru.capacity {
		lru.evictOne()
	}
}

// evictOne 删除链表尾部的元素（最久未使用）
func (lru *LRUCache) evictOne() {
	elem := lru.list.Back()
	if elem != nil {
		lru.list.Remove(elem)
		key := elem.Value.(*lruNode).key
		delete(lru.cache, key)
	}
}

// Len 返回当前缓存中的元素个数
func (lru *LRUCache) Len() int {
	lru.mu.RLock()
	defer lru.mu.RUnlock()
	return lru.list.Len()
}

// Delete 从缓存中删除一个键
func (lru *LRUCache) Delete(key string) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if elem, exists := lru.cache[key]; exists {
		lru.list.Remove(elem)
		delete(lru.cache, key)
	}
}

// Clear 清空缓存
func (lru *LRUCache) Clear() {
	lru.mu.Lock()
	defer lru.mu.Unlock()
	lru.cache = make(map[string]*list.Element)
	lru.list = list.New()
}

// RawCacheEntry 原始缓存项（上游 DNS 的原始响应）
type RawCacheEntry struct {
	IPs               []string  // 原始 IP 列表
	CNAMEs            []string  // CNAME 记录列表（支持多级 CNAME）
	UpstreamTTL       uint32    // 上游 DNS 返回的原始 TTL（秒）
	AcquisitionTime   time.Time // 从上游获取的时间
	AuthenticatedData bool      // DNSSEC 验证标记 (AD flag)
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
	rawCache     *LRUCache                     // 原始缓存（使用 LRU 管理）
	sortedCache  *LRUCache                     // 排序缓存（使用 LRU 管理）
	sortingState map[string]*SortingState      // 排序任务状态
	errorCache   *LRUCache                     // 错误缓存（使用 LRU 管理）
	blockedCache map[string]*BlockedCacheEntry // 拦截缓存
	allowedCache map[string]*AllowedCacheEntry // 白名单缓存

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
	maxEntries := cfg.CalculateMaxEntries()
	return &Cache{
		config:       cfg,
		maxEntries:   maxEntries,
		rawCache:     NewLRUCache(maxEntries),
		sortedCache:  NewLRUCache(maxEntries),
		sortingState: make(map[string]*SortingState),
		errorCache:   NewLRUCache(maxEntries),
		blockedCache: make(map[string]*BlockedCacheEntry),
		allowedCache: make(map[string]*AllowedCacheEntry),
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
	value, exists := c.rawCache.Get(key)
	if !exists {
		return nil, false
	}

	entry, ok := value.(*RawCacheEntry)
	return entry, ok
}

// SetRaw 设置原始缓存（上游 DNS 响应）
func (c *Cache) SetRaw(domain string, qtype uint16, ips []string, cnames []string, upstreamTTL uint32) {
	c.SetRawWithDNSSEC(domain, qtype, ips, cnames, upstreamTTL, false)
}

// SetRawWithDNSSEC 设置带 DNSSEC 标记的原始缓存
func (c *Cache) SetRawWithDNSSEC(domain string, qtype uint16, ips []string, cnames []string, upstreamTTL uint32, authData bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)
	entry := &RawCacheEntry{
		IPs:               ips,
		CNAMEs:            cnames,
		UpstreamTTL:       upstreamTTL,
		AcquisitionTime:   time.Now(),
		AuthenticatedData: authData,
	}
	c.rawCache.Set(key, entry)
}

// SetPrefetcher 设置 prefetcher 实例，用于解耦
func (c *Cache) SetPrefetcher(p PrefetchChecker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefetcher = p
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

// getRawCacheSnapshot 获取 rawCache 中所有值的快照（仅供内部使用）
func (c *Cache) getRawCacheSnapshot() []*RawCacheEntry {
	c.rawCache.mu.RLock()
	defer c.rawCache.mu.RUnlock()

	entries := make([]*RawCacheEntry, 0, len(c.rawCache.cache))
	for elem := c.rawCache.list.Front(); elem != nil; elem = elem.Next() {
		if node, ok := elem.Value.(*lruNode); ok {
			if entry, ok := node.value.(*RawCacheEntry); ok {
				entries = append(entries, entry)
			}
		}
	}
	return entries
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

// getRawCacheKeysSnapshot 获取 rawCache 中所有键的快照（仅供内部使用）
func (c *Cache) getRawCacheKeysSnapshot() []string {
	c.rawCache.mu.RLock()
	defer c.rawCache.mu.RUnlock()

	keys := make([]string, 0, len(c.rawCache.cache))
	for elem := c.rawCache.list.Front(); elem != nil; elem = elem.Next() {
		if node, ok := elem.Value.(*lruNode); ok {
			keys = append(keys, node.key)
		}
	}
	return keys
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
func (c *Cache) SetSorted(domain string, qtype uint16, entry *SortedCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

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

// GetError 获取错误缓存
func (c *Cache) GetError(domain string, qtype uint16) (*ErrorCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

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
func (c *Cache) SetError(domain string, qtype uint16, rcode int, ttl int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)
	entry := &ErrorCacheEntry{
		Rcode:    rcode,
		CachedAt: time.Now(),
		TTL:      ttl,
	}
	c.errorCache.Set(key, entry)
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

// cleanExpiredSortedCache 清理过期的排序缓存
func (c *Cache) cleanExpiredSortedCache() {
	c.sortedCache.mu.Lock()
	defer c.sortedCache.mu.Unlock()

	elemsToRemove := make([]*list.Element, 0)
	for elem := c.sortedCache.list.Front(); elem != nil; elem = elem.Next() {
		if node, ok := elem.Value.(*lruNode); ok {
			if entry, ok := node.value.(*SortedCacheEntry); ok && entry.IsExpired() {
				elemsToRemove = append(elemsToRemove, elem)
			}
		}
	}

	for _, elem := range elemsToRemove {
		c.sortedCache.list.Remove(elem)
		key := elem.Value.(*lruNode).key
		delete(c.sortedCache.cache, key)
	}
}

// cleanExpiredErrorCache 清理过期的错误缓存
func (c *Cache) cleanExpiredErrorCache() {
	c.errorCache.mu.Lock()
	defer c.errorCache.mu.Unlock()

	elemsToRemove := make([]*list.Element, 0)
	for elem := c.errorCache.list.Front(); elem != nil; elem = elem.Next() {
		if node, ok := elem.Value.(*lruNode); ok {
			if entry, ok := node.value.(*ErrorCacheEntry); ok && entry.IsExpired() {
				elemsToRemove = append(elemsToRemove, elem)
			}
		}
	}

	for _, elem := range elemsToRemove {
		c.errorCache.list.Remove(elem)
		key := elem.Value.(*lruNode).key
		delete(c.errorCache.cache, key)
	}
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

// PersistentCacheEntry 用于持久化的缓存项
type PersistentCacheEntry struct {
	Domain string   `json:"domain"`
	QType  uint16   `json:"qtype"`
	IPs    []string `json:"ips"`
	CNAME  string   `json:"cname,omitempty"`  // 旧版本兼容
	CNAMEs []string `json:"cnames,omitempty"` // 新版本字段
}

// SaveToDisk 将缓存保存到磁盘
// 采用原子写入策略：先写入临时文件，再重命名，防止写入中断导致文件损坏
func (c *Cache) SaveToDisk(filename string) error {
	c.mu.RLock()
	// 从 LRUCache 获取所有原始缓存项的快照
	cacheSnapshot := c.getRawCacheSnapshot()
	allKeys := c.getRawCacheKeysSnapshot()
	c.mu.RUnlock()

	// 构建条目列表
	var entries []PersistentCacheEntry
	for i, key := range allKeys {
		if i >= len(cacheSnapshot) {
			break
		}
		entry := cacheSnapshot[i]

		domain := c.extractDomain(key)
		// Extract QType from key (format: domain#qtype_char)
		parts := strings.Split(key, "#")
		if len(parts) != 2 {
			continue
		}
		// Convert string back to rune then to uint16
		qtype := uint16([]rune(parts[1])[0])

		// 优先写入 CNAMEs
		entryCNAMEs := entry.CNAMEs
		var legacyCNAME string
		if len(entryCNAMEs) > 0 {
			legacyCNAME = entryCNAMEs[0]
		}

		entries = append(entries, PersistentCacheEntry{
			Domain: domain,
			QType:  qtype,
			IPs:    entry.IPs,
			CNAME:  legacyCNAME, // 写入旧字段以保持兼容性
			CNAMEs: entryCNAMEs,
		})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}

	// 写入临时文件
	tempFile := filename + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return err
	}

	// 原子替换（在 Windows 上 Go 的 os.Rename 会尝试覆盖目标文件）
	return os.Rename(tempFile, filename)
}

// LoadFromDisk 从磁盘加载缓存
func (c *Cache) LoadFromDisk(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, nothing to load
		}
		return err
	}

	var entries []PersistentCacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range entries {
		key := cacheKey(entry.Domain, entry.QType)

		// 迁移逻辑：如果 CNAMEs 为空但 CNAME 不为空，则转换
		cnames := entry.CNAMEs
		if len(cnames) == 0 && entry.CNAME != "" {
			cnames = []string{entry.CNAME}
		}

		cacheEntry := &RawCacheEntry{
			IPs:             entry.IPs,
			CNAMEs:          cnames,
			UpstreamTTL:     300, // Default 5 minutes as we don't persist TTL
			AcquisitionTime: time.Now(),
		}
		c.rawCache.Set(key, cacheEntry)
	}
	return nil
}
