package cache

import (
	"fmt" // Added for fmt.Sprintf in createTestDNSMsg
	"testing"
	"time"

	"smartdnssort/config" // Added for config.CacheConfig in getDefaultCacheConfig

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
)

// TestSortedCache tests basic Get/Set operations for the sorted cache.
func TestSortedCache(t *testing.T) {
	c := NewCache(getDefaultCacheConfig())
	domain := "sorted.example.com"
	qtype := dns.TypeA

	entry := &SortedCacheEntry{
		IPs:       []string{"1.1.1.1", "8.8.8.8"},
		RTTs:      []int{10, 20},
		Timestamp: time.Now(),
		TTL:       300,
		IsValid:   true,
	}

	c.SetSorted(domain, qtype, entry)

	retrieved, ok := c.GetSorted(domain, qtype)
	assert.True(t, ok, "Expected to find sorted cache entry")
	assert.NotNil(t, retrieved, "Retrieved entry should not be nil")
	assert.Equal(t, entry.IPs, retrieved.IPs, "IPs should match")
	assert.Equal(t, entry.RTTs, retrieved.RTTs, "RTTs should match")
}

// TestSortedCacheExpiration tests the expiration logic for the sorted cache.
func TestSortedCacheExpiration(t *testing.T) {
	c := NewCache(getDefaultCacheConfig())
	domain := "expired-sorted.example.com"
	qtype := dns.TypeA

	entry := &SortedCacheEntry{
		IPs:       []string{"1.1.1.1"},
		RTTs:      []int{50},
		Timestamp: time.Now().Add(-400 * time.Second), // Expired
		TTL:       300,
		IsValid:   true,
	}

	c.SetSorted(domain, qtype, entry)

	_, ok := c.GetSorted(domain, qtype)
	assert.False(t, ok, "Expected expired sorted entry to be invalid")
}

// getDefaultCacheConfig provides a default cache configuration for testing.
func getDefaultCacheConfig() *config.CacheConfig {
	return &config.CacheConfig{
		FastResponseTTL: 15,
		UserReturnTTL:   600,
		MinTTLSeconds:   0,
		MaxTTLSeconds:   0,
		NegativeTTLSeconds: 300,
		ErrorCacheTTL:      30,
		MaxMemoryMB:        128,
		MsgCacheSizeMB:     1, // Small size for testing
		DNSSECMsgCacheTTLSeconds: 300, // Default to 5 minutes
		EvictionThreshold:  0.9,
		EvictionBatchPercent: 0.1,
		ProtectPrefetchDomains: false,
		SaveToDiskIntervalMinutes: 60,
	}
}

// createTestDNSMsg creates a dns.Msg with specified records and TTL.
func createTestDNSMsg(domain string, qtype uint16, ip string, ttl uint32, includeRRSIG bool, includeDNSKEY bool, includeDS bool) *dns.Msg {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), qtype)
	msg.Authoritative = true
	msg.RecursionAvailable = true

	if ip != "" {
		switch qtype {
		case dns.TypeA:
			rr, _ := dns.NewRR(fmt.Sprintf("%s A %s", dns.Fqdn(domain), ip))
			rr.Header().Ttl = ttl
			msg.Answer = append(msg.Answer, rr)
		case dns.TypeAAAA:
			rr, _ := dns.NewRR(fmt.Sprintf("%s AAAA %s", dns.Fqdn(domain), ip))
			rr.Header().Ttl = ttl
			msg.Answer = append(msg.Answer, rr)
		}
	}

	if includeRRSIG {
		rrsig, _ := dns.NewRR(fmt.Sprintf("%s RRSIG A 8 2 300 20250101000000 20240101000000 12345 example.com. NSEC", dns.Fqdn(domain)))
		rrsig.Header().Ttl = 30 // Short TTL for RRSIG for testing purposes
		msg.Answer = append(msg.Answer, rrsig)
	}

	if includeDNSKEY {
		dnskey, _ := dns.NewRR(fmt.Sprintf("%s DNSKEY 256 3 8 AwEA...dummykey...", dns.Fqdn(domain)))
		dnskey.Header().Ttl = ttl
		msg.Extra = append(msg.Extra, dnskey)
	}

	if includeDS {
		ds, _ := dns.NewRR(fmt.Sprintf("%s DS 12345 8 2 1234567890ABCDEF...", dns.Fqdn(domain)))
		ds.Header().Ttl = ttl
		msg.Extra = append(msg.Extra, ds)
	}

	return msg
}

// TestDNSSECCacheBasic tests basic Get/Set operations for the DNSSEC message cache.
func TestDNSSECCacheBasic(t *testing.T) {
	cfg := getDefaultCacheConfig()
	c := NewCache(cfg)
	domain := "dnssec.example.com"
	qtype := dns.TypeA
	ttl := uint32(120) // Original TTL for records

	testMsg := createTestDNSMsg(domain, qtype, "1.2.3.4", ttl, true, false, false)
	c.SetDNSSECMsg(domain, qtype, testMsg)

	retrievedEntry, ok := c.GetDNSSECMsg(domain, qtype)
	assert.True(t, ok, "Expected to find DNSSEC cache entry")
	assert.NotNil(t, retrievedEntry, "Retrieved entry should not be nil")
	assert.Equal(t, testMsg.Answer[0].(*dns.A).A.String(), retrievedEntry.Message.Answer[0].(*dns.A).A.String(), "IP should match")

	// The RRSIG TTL was 30, so getMinTTL should be 30.
	// Configured DNSSECMsgCacheTTLSeconds is 300. So effective TTL should be 30.
	assert.Equal(t, uint32(30), retrievedEntry.TTL, "Entry TTL should be the minimum of records and config")
	assert.True(t, retrievedEntry.TTL <= ttl, "Entry TTL should be less than or equal to original message TTL (which is 120)")
	assert.True(t, retrievedEntry.TTL <= uint32(cfg.DNSSECMsgCacheTTLSeconds), "Entry TTL should be capped by config (300)")

	// Ensure the message stored is a copy and doesn't directly point to the original
	assert.False(t, retrievedEntry.Message == testMsg, "Stored message should be a copy, not the original reference")
}

// TestDNSSECCacheTTLEffective tests the effective TTL calculation for DNSSEC entries.
func TestDNSSECCacheTTLEffective(t *testing.T) {
	cfg := getDefaultCacheConfig()
	c := NewCache(cfg)
	domain := "ttl.example.com"
	qtype := dns.TypeA

	// Case 1: All records have high TTL, config caps it
	cfg.DNSSECMsgCacheTTLSeconds = 60 // Configured TTL
	msgWithHighTTL := createTestDNSMsg(domain, qtype, "1.1.1.1", 300, true, false, false) // RRSIG has 30s, A has 300s
	c.SetDNSSECMsg(domain, qtype, msgWithHighTTL)
	retrievedEntry, ok := c.GetDNSSECMsg(domain, qtype)
	assert.True(t, ok)
	// Min TTL of records is 30 (from RRSIG). Configured is 60. So effective is 30.
	assert.Equal(t, uint32(30), retrievedEntry.TTL, "Effective TTL should be min(minMsgTTL, configTTL)")

	// Case 2: All records have high TTL, but without RRSIG to change minMsgTTL
	// Need to reset cache config or create new cache for clean test
	cfg2 := getDefaultCacheConfig()
	cfg2.DNSSECMsgCacheTTLSeconds = 60 // Configured TTL
	c2 := NewCache(cfg2)
	msgWithHighTTLNoRRSIG := createTestDNSMsg(domain, qtype, "1.1.1.1", 300, false, false, false)
	c2.SetDNSSECMsg(domain, qtype, msgWithHighTTLNoRRSIG)
	retrievedEntry2, ok2 := c2.GetDNSSECMsg(domain, qtype)
	assert.True(t, ok2)
	// Min TTL of records is 300 (from A record). Configured is 60. So effective is 60.
	assert.Equal(t, uint32(60), retrievedEntry2.TTL, "Effective TTL should be min(minMsgTTL, configTTL)")

	// Case 3: Min record TTL is lower than config TTL
	cfg3 := getDefaultCacheConfig()
	cfg3.DNSSECMsgCacheTTLSeconds = 300 // Configured TTL
	c3 := NewCache(cfg3)
	msgWithLowRRSIGTTL := createTestDNSMsg(domain, qtype, "1.1.1.1", 300, true, false, false) // RRSIG has 30s
	c3.SetDNSSECMsg(domain, qtype, msgWithLowRRSIGTTL)
	retrievedEntry3, ok3 := c3.GetDNSSECMsg(domain, qtype)
	assert.True(t, ok3)
	// Min TTL of records is 30 (from RRSIG). Configured is 300. So effective is 30.
	assert.Equal(t, uint32(30), retrievedEntry3.TTL, "Effective TTL should be min(minMsgTTL, configTTL)")
}

// TestDNSSECCacheFilterRecords tests that DNSKEY and DS records are filtered out.
func TestDNSSECCacheFilterRecords(t *testing.T) {
	cfg := getDefaultCacheConfig()
	c := NewCache(cfg)
	domain := "filter.example.com"
	qtype := dns.TypeA

	// Create a message with A, RRSIG, DNSKEY, and DS records
	testMsg := createTestDNSMsg(domain, qtype, "1.2.3.4", 300, true, true, true)
	c.SetDNSSECMsg(domain, qtype, testMsg)

	retrievedEntry, ok := c.GetDNSSECMsg(domain, qtype)
	assert.True(t, ok, "Expected to find DNSSEC cache entry")
	assert.NotNil(t, retrievedEntry, "Retrieved entry should not be nil")

	// Assert DNSKEY and DS are NOT in Answer, Ns, or Extra sections
	for _, rr := range retrievedEntry.Message.Answer {
		assert.NotEqual(t, dns.TypeDNSKEY, rr.Header().Rrtype, "DNSKEY should be filtered from Answer")
		assert.NotEqual(t, dns.TypeDS, rr.Header().Rrtype, "DS should be filtered from Answer")
	}
	for _, rr := range retrievedEntry.Message.Ns {
		assert.NotEqual(t, dns.TypeDNSKEY, rr.Header().Rrtype, "DNSKEY should be filtered from Ns")
		assert.NotEqual(t, dns.TypeDS, rr.Header().Rrtype, "DS should be filtered from Ns")
	}
	for _, rr := range retrievedEntry.Message.Extra {
		assert.NotEqual(t, dns.TypeDNSKEY, rr.Header().Rrtype, "DNSKEY should be filtered from Extra")
		assert.NotEqual(t, dns.TypeDS, rr.Header().Rrtype, "DS should be filtered from Extra")
	}

	// Assert that A and RRSIG are still present
	aCount := 0
	rrsigCount := 0
	for _, rr := range retrievedEntry.Message.Answer {
		if rr.Header().Rrtype == dns.TypeA {
			aCount++
		}
		if rr.Header().Rrtype == dns.TypeRRSIG {
			rrsigCount++
		}
	}
	assert.Greater(t, aCount, 0, "A record should still be present")
	assert.Greater(t, rrsigCount, 0, "RRSIG record should still be present")
}

// TestDNSSECCacheExpiration tests the expiration logic for DNSSEC cache entries.
func TestDNSSECCacheExpiration(t *testing.T) {
	cfg := getDefaultCacheConfig()
	cfg.DNSSECMsgCacheTTLSeconds = 1 // Very short TTL for testing expiration
	c := NewCache(cfg)
	domain := "expire.dnssec.example.com"
	qtype := dns.TypeA

	testMsg := createTestDNSMsg(domain, qtype, "1.2.3.4", 10, true, false, false) // RRSIG has 30s, A has 10s, but config caps to 1s
	c.SetDNSSECMsg(domain, qtype, testMsg)

	// Immediately try to retrieve
	_, ok := c.GetDNSSECMsg(domain, qtype)
	assert.True(t, ok, "Expected to find DNSSEC cache entry immediately after setting")

	// Wait for the TTL to expire
	time.Sleep(2 * time.Second) // Sleep a bit longer than 1 second

	_, ok = c.GetDNSSECMsg(domain, qtype)
	assert.False(t, ok, "Expected DNSSEC cache entry to be expired after its TTL")
}

// TestRawCache tests basic Get/Set operations for the raw cache.
func TestRawCache(t *testing.T) {
	c := NewCache(getDefaultCacheConfig())
	domain := "raw.example.com"
	qtype := dns.TypeAAAA

	ips := []string{"2001:4860:4860::8888"}
	upstreamTTL := uint32(60)

	c.SetRaw(domain, qtype, ips, nil, upstreamTTL)

	retrieved, ok := c.GetRaw(domain, qtype)
	assert.True(t, ok, "Expected to find raw cache entry")
	assert.NotNil(t, retrieved, "Retrieved entry should not be nil")
	assert.Equal(t, ips, retrieved.IPs, "IPs should match")
	assert.Equal(t, upstreamTTL, retrieved.UpstreamTTL, "UpstreamTTL should match")
}

// TestRawCacheExpiration tests the expiration logic for the raw cache.
func TestRawCacheExpiration(t *testing.T) {
	c := NewCache(getDefaultCacheConfig())
	domain := "expired-raw.example.com"
	qtype := dns.TypeA

	// 直接设置过期的缓存条目（不通过 SetRaw，因为它会覆盖 AcquisitionTime）
	c.mu.Lock()
	key := cacheKey(domain, qtype)
	entry := &RawCacheEntry{
		IPs:             []string{"1.2.3.4"},
		UpstreamTTL:     60,
		AcquisitionTime: time.Now().Add(-100 * time.Second), // 100秒前获取，TTL 60秒，已过期
	}
	c.rawCache.Set(key, entry)
	c.mu.Unlock()

	// GetRaw 应该返回过期的条目 (stale-while-revalidate)
	_, ok := c.GetRaw(domain, qtype)
	assert.True(t, ok, "Expected expired raw entry to be returned (stale-while-revalidate)")
}

// TestCleanExpired tests the cleaning of expired entries.
func TestCleanExpired(t *testing.T) {
	c := NewCache(getDefaultCacheConfig())
	expiredDomain := "expired.com"
	validDomain := "valid.com"
	qtype := dns.TypeA

	// Add an expired entry
	c.SetSorted(expiredDomain, qtype, &SortedCacheEntry{
		IPs:       []string{"1.1.1.1"},
		Timestamp: time.Now().Add(-200 * time.Second),
		TTL:       100,
		IsValid:   true,
	})

	// Add a valid entry
	c.SetSorted(validDomain, qtype, &SortedCacheEntry{
		IPs:       []string{"8.8.8.8"},
		Timestamp: time.Now(),
		TTL:       300,
		IsValid:   true,
	})

	c.CleanExpired()

	_, ok := c.GetSorted(expiredDomain, qtype)
	assert.False(t, ok, "Expired entry should have been cleaned")

	_, ok = c.GetSorted(validDomain, qtype)
	assert.True(t, ok, "Valid entry should not have been cleaned")
}
