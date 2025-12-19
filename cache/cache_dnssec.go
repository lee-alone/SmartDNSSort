package cache

import (
	"github.com/miekg/dns"
)

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

// getMinTTL 获取 DNS 消息中所有记录的最小 TTL
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
