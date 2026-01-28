package cache

import "github.com/miekg/dns"

// GetRaw 获取原始缓存（上游 DNS 响应）
// 注意:此方法不检查过期,调用方需要自行判断是否过期
// 即使过期也返回缓存,用于阶段三:返回旧数据+异步刷新
// 注意：rawCache 内部已实现线程安全，无需全局锁
func (c *Cache) GetRaw(domain string, qtype uint16) (*RawCacheEntry, bool) {
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

// SetRawWithVersion 设置原始缓存，带版本号（用于防止旧的后台补全覆盖新的缓存）
func (c *Cache) SetRawWithVersion(domain string, qtype uint16, ips []string, cnames []string, upstreamTTL uint32, queryVersion int64) {
	c.SetRawWithDNSSECAndVersion(domain, qtype, ips, cnames, upstreamTTL, false, queryVersion)
}

// SetRawWithDNSSEC 设置带 DNSSEC 标记的原始缓存
// 注意：rawCache 内部已实现线程安全，无需全局锁
func (c *Cache) SetRawWithDNSSEC(domain string, qtype uint16, ips []string, cnames []string, upstreamTTL uint32, authData bool) {
	key := cacheKey(domain, qtype)
	effTTL := c.calculateEffectiveTTL(upstreamTTL)
	entry := &RawCacheEntry{
		Records:           nil, // 向后兼容，暂时保持为 nil
		IPs:               ips,
		CNAMEs:            cnames,
		UpstreamTTL:       upstreamTTL,
		EffectiveTTL:      effTTL,
		AcquisitionTime:   timeNow(),
		AuthenticatedData: authData,
		QueryVersion:      timeNow().UnixNano(), // 使用当前时间作为版本号
	}
	c.rawCache.Set(key, entry)

	// 将过期数据添加到堆中（异步化，无全局锁）
	// 使用 EffectiveTTL 确保即使上游 TTL 很短，数据也在本地生存足够长时间
	expiryTime := timeNow().Unix() + int64(effTTL)
	c.addToExpiredHeap(key, expiryTime)
}

// SetRawWithDNSSECAndVersion 设置带 DNSSEC 标记和版本号的原始缓存
func (c *Cache) SetRawWithDNSSECAndVersion(domain string, qtype uint16, ips []string, cnames []string, upstreamTTL uint32, authData bool, queryVersion int64) {
	key := cacheKey(domain, qtype)
	effTTL := c.calculateEffectiveTTL(upstreamTTL)
	entry := &RawCacheEntry{
		Records:           nil, // 向后兼容，暂时保持为 nil
		IPs:               ips,
		CNAMEs:            cnames,
		UpstreamTTL:       upstreamTTL,
		EffectiveTTL:      effTTL,
		AcquisitionTime:   timeNow(),
		AuthenticatedData: authData,
		QueryVersion:      queryVersion,
	}
	c.rawCache.Set(key, entry)

	// 将过期数据添加到堆中（异步化，无全局锁）
	expiryTime := timeNow().Unix() + int64(effTTL)
	c.addToExpiredHeap(key, expiryTime)
}

// SetRawRecords 设置通用记录的原始缓存
func (c *Cache) SetRawRecords(domain string, qtype uint16, records []dns.RR, cnames []string, upstreamTTL uint32) {
	c.SetRawRecordsWithDNSSEC(domain, qtype, records, cnames, upstreamTTL, false)
}

// SetRawRecordsWithVersion 设置通用记录的原始缓存，带版本号
func (c *Cache) SetRawRecordsWithVersion(domain string, qtype uint16, records []dns.RR, cnames []string, upstreamTTL uint32, queryVersion int64) {
	c.SetRawRecordsWithDNSSECAndVersion(domain, qtype, records, cnames, upstreamTTL, false, queryVersion)
}

// SetRawRecordsWithDNSSEC 设置带 DNSSEC 标记的通用记录原始缓存
// 注意：IPs 字段会从 records 中自动派生（A/AAAA 记录的物化视图）
// 同时进行IP级别去重，确保IPs列表中没有重复
// 注意：rawCache 内部已实现线程安全，无需全局锁
func (c *Cache) SetRawRecordsWithDNSSEC(domain string, qtype uint16, records []dns.RR, cnames []string, upstreamTTL uint32, authData bool) {
	// 从 records 中提取 A/AAAA 记录的 IP 字符串（去重）
	ipSet := make(map[string]bool)
	var ips []string
	for _, r := range records {
		switch rec := r.(type) {
		case *dns.A:
			ipStr := rec.A.String()
			if !ipSet[ipStr] {
				ipSet[ipStr] = true
				ips = append(ips, ipStr)
			}
		case *dns.AAAA:
			ipStr := rec.AAAA.String()
			if !ipSet[ipStr] {
				ipSet[ipStr] = true
				ips = append(ips, ipStr)
			}
		}
	}

	key := cacheKey(domain, qtype)
	effTTL := c.calculateEffectiveTTL(upstreamTTL)
	entry := &RawCacheEntry{
		Records:           records,
		IPs:               ips, // 从 records 派生，已去重
		CNAMEs:            cnames,
		UpstreamTTL:       upstreamTTL,
		EffectiveTTL:      effTTL,
		AcquisitionTime:   timeNow(),
		AuthenticatedData: authData,
		QueryVersion:      timeNow().UnixNano(), // 使用当前时间作为版本号
	}
	c.rawCache.Set(key, entry)

	// 将过期数据添加到堆中（异步化，无全局锁）
	// 使用 EffectiveTTL 确保即使上游 TTL 很短，数据也在本地生存足够长时间
	expiryTime := timeNow().Unix() + int64(effTTL)
	c.addToExpiredHeap(key, expiryTime)
}

// SetRawRecordsWithDNSSECAndVersion 设置带 DNSSEC 标记和版本号的通用记录原始缓存
func (c *Cache) SetRawRecordsWithDNSSECAndVersion(domain string, qtype uint16, records []dns.RR, cnames []string, upstreamTTL uint32, authData bool, queryVersion int64) {
	// 从 records 中提取 A/AAAA 记录的 IP 字符串（去重）
	ipSet := make(map[string]bool)
	var ips []string
	for _, r := range records {
		switch rec := r.(type) {
		case *dns.A:
			ipStr := rec.A.String()
			if !ipSet[ipStr] {
				ipSet[ipStr] = true
				ips = append(ips, ipStr)
			}
		case *dns.AAAA:
			ipStr := rec.AAAA.String()
			if !ipSet[ipStr] {
				ipSet[ipStr] = true
				ips = append(ips, ipStr)
			}
		}
	}

	key := cacheKey(domain, qtype)
	effTTL := c.calculateEffectiveTTL(upstreamTTL)
	entry := &RawCacheEntry{
		Records:           records,
		IPs:               ips, // 从 records 派生，已去重
		CNAMEs:            cnames,
		UpstreamTTL:       upstreamTTL,
		EffectiveTTL:      effTTL,
		AcquisitionTime:   timeNow(),
		AuthenticatedData: authData,
		QueryVersion:      queryVersion,
	}
	c.rawCache.Set(key, entry)

	// 将过期数据添加到堆中（异步化，无全局锁）
	expiryTime := timeNow().Unix() + int64(effTTL)
	c.addToExpiredHeap(key, expiryTime)
}

// getRawCacheSnapshot 获取 rawCache 中所有值的快照（仅供内部使用）
func (c *Cache) getRawCacheSnapshot() []*RawCacheEntry {
	entries := make([]*RawCacheEntry, 0)
	allValues := c.rawCache.GetAllEntries()
	for _, val := range allValues {
		if entry, ok := val.(*RawCacheEntry); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// getRawCacheKeysSnapshot 获取 rawCache 中所有键的快照（仅供内部使用）
func (c *Cache) getRawCacheKeysSnapshot() []string {
	return c.rawCache.GetAllKeys()
}

// calculateEffectiveTTL 计算应用了本地策略后的有效 TTL
func (c *Cache) calculateEffectiveTTL(upstreamTTL uint32) uint32 {
	effTTL := upstreamTTL

	if c.config.MinTTLSeconds > 0 && effTTL < uint32(c.config.MinTTLSeconds) {
		effTTL = uint32(c.config.MinTTLSeconds)
	}

	if c.config.MaxTTLSeconds > 0 && effTTL > uint32(c.config.MaxTTLSeconds) {
		effTTL = uint32(c.config.MaxTTLSeconds)
	}

	return effTTL
}
