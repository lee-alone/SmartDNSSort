package cache

import (
	"container/list"
	"strings"

	"github.com/miekg/dns"
)

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
		AcquisitionTime:   timeNow(),
		AuthenticatedData: authData,
	}
	c.rawCache.Set(key, entry)
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
		CachedAt: timeNow(),
		TTL:      ttl,
	}
	c.errorCache.Set(key, entry)
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

func getMinTTL(msg *dns.Msg) uint32 {
	minTTL := uint32(0xFFFFFFFF) // Max possible TTL

	// Check Answer section
	for _, rr := range msg.Answer {
		if rr.Header().Ttl < minTTL {
			minTTL = rr.Header().Ttl
		}
	}
	// Check Ns section
	for _, rr := range msg.Ns {
		if rr.Header().Ttl < minTTL {
			minTTL = rr.Header().Ttl
		}
	}
	// Check Extra section
	for _, rr := range msg.Extra {
		if rr.Header().Ttl < minTTL {
			minTTL = rr.Header().Ttl
		}
	}

	if minTTL == uint32(0xFFFFFFFF) {
		return 60 // Default TTL if no records or all have max TTL
	}
	return minTTL
}

// GetDNSSECMsg 从 msgCache 获取完整的 DNSSEC 消息及其缓存信息
func (c *Cache) GetDNSSECMsg(domain string, qtype uint16) (*DNSSECCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey(domain, qtype)
	value, exists := c.msgCache.Get(key)
	if !exists {
		return nil, false
	}

	entry, ok := value.(*DNSSECCacheEntry)
	if !ok {
		return nil, false
	}

	// 检查 DNSSEC 消息是否过期
	if entry.IsExpired() {
		c.msgCache.Delete(key) // 如果过期，从 LRU 缓存中移除
		return nil, false
	}

	return entry, true
}

// SetDNSSECMsg 将完整的 DNSSEC 消息及其缓存信息存储到 msgCache
func (c *Cache) SetDNSSECMsg(domain string, qtype uint16, msg *dns.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := cacheKey(domain, qtype)

	// Filter out DNSKEY and DS records before caching
	filteredMsg := msg.Copy() // Work on a copy to avoid modifying the original
	filteredMsg.Answer = filterRecords(filteredMsg.Answer)
	filteredMsg.Ns = filterRecords(filteredMsg.Ns)
	filteredMsg.Extra = filterRecords(filteredMsg.Extra)

	// 获取消息中所有记录的最小 TTL
	minMsgTTL := getMinTTL(filteredMsg)

	// 结合配置的 DNSSEC 消息缓存 TTL
	// 如果配置为 0，表示不限制，则使用 minMsgTTL
	effectiveTTL := minMsgTTL
	if c.config.DNSSECMsgCacheTTLSeconds > 0 {
		effectiveTTL = uint32(min(int(minMsgTTL), c.config.DNSSECMsgCacheTTLSeconds))
	}

	entry := &DNSSECCacheEntry{
		Message:         filteredMsg,
		AcquisitionTime: timeNow(),
		TTL:             effectiveTTL,
	}
	c.msgCache.Set(key, entry)
}

// filterRecords 过滤掉 DNSKEY 和 DS 记录
func filterRecords(rrs []dns.RR) []dns.RR {
	var filtered []dns.RR
	for _, rr := range rrs {
		if rr.Header().Rrtype != dns.TypeDNSKEY && rr.Header().Rrtype != dns.TypeDS {
			filtered = append(filtered, rr)
		}
	}
	return filtered
}
