package cache

import "github.com/miekg/dns"

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
		Records:           nil, // 向后兼容，暂时保持为 nil
		IPs:               ips,
		CNAMEs:            cnames,
		UpstreamTTL:       upstreamTTL,
		AcquisitionTime:   timeNow(),
		AuthenticatedData: authData,
	}
	c.rawCache.Set(key, entry)
}

// SetRawRecords 设置通用记录的原始缓存
func (c *Cache) SetRawRecords(domain string, qtype uint16, records []dns.RR, cnames []string, upstreamTTL uint32) {
	c.SetRawRecordsWithDNSSEC(domain, qtype, records, cnames, upstreamTTL, false)
}

// SetRawRecordsWithDNSSEC 设置带 DNSSEC 标记的通用记录原始缓存
// 注意：IPs 字段会从 records 中自动派生（A/AAAA 记录的物化视图）
func (c *Cache) SetRawRecordsWithDNSSEC(domain string, qtype uint16, records []dns.RR, cnames []string, upstreamTTL uint32, authData bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 从 records 中提取 A/AAAA 记录的 IP 字符串
	var ips []string
	for _, r := range records {
		switch rec := r.(type) {
		case *dns.A:
			ips = append(ips, rec.A.String())
		case *dns.AAAA:
			ips = append(ips, rec.AAAA.String())
		}
	}

	key := cacheKey(domain, qtype)
	entry := &RawCacheEntry{
		Records:           records,
		IPs:               ips, // 从 records 派生
		CNAMEs:            cnames,
		UpstreamTTL:       upstreamTTL,
		AcquisitionTime:   timeNow(),
		AuthenticatedData: authData,
	}
	c.rawCache.Set(key, entry)
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
