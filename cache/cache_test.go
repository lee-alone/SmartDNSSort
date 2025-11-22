package cache

import (
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
)

// TestSortedCache tests basic Get/Set operations for the sorted cache.
func TestSortedCache(t *testing.T) {
	c := NewCache()
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
	c := NewCache()
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

// TestRawCache tests basic Get/Set operations for the raw cache.
func TestRawCache(t *testing.T) {
	c := NewCache()
	domain := "raw.example.com"
	qtype := dns.TypeAAAA

	ips := []string{"2001:4860:4860::8888"}
	upstreamTTL := uint32(60)

	c.SetRaw(domain, qtype, ips, "", upstreamTTL)

	retrieved, ok := c.GetRaw(domain, qtype)
	assert.True(t, ok, "Expected to find raw cache entry")
	assert.NotNil(t, retrieved, "Retrieved entry should not be nil")
	assert.Equal(t, ips, retrieved.IPs, "IPs should match")
	assert.Equal(t, upstreamTTL, retrieved.UpstreamTTL, "UpstreamTTL should match")
}

// TestRawCacheExpiration tests the expiration logic for the raw cache.
func TestRawCacheExpiration(t *testing.T) {
	c := NewCache()
	domain := "expired-raw.example.com"
	qtype := dns.TypeA

	// 直接设置过期的缓存条目（不通过 SetRaw，因为它会覆盖 AcquisitionTime）
	c.mu.Lock()
	key := cacheKey(domain, qtype)
	c.rawCache[key] = &RawCacheEntry{
		IPs:             []string{"1.2.3.4"},
		UpstreamTTL:     60,
		AcquisitionTime: time.Now().Add(-100 * time.Second), // 100秒前获取，TTL 60秒，已过期
	}
	c.mu.Unlock()

	// GetRaw 应该返回过期的条目 (stale-while-revalidate)
	_, ok := c.GetRaw(domain, qtype)
	assert.True(t, ok, "Expected expired raw entry to be returned (stale-while-revalidate)")
}

// TestRawCacheCleanup tests that expired entries are eventually cleaned up after the grace period.
func TestRawCacheCleanup(t *testing.T) {
	c := NewCache()
	domain := "cleanup.example.com"
	qtype := dns.TypeA

	// 1. 添加一个刚过期的条目 (在 grace period 内)
	c.mu.Lock()
	c.rawCache[cacheKey(domain, qtype)] = &RawCacheEntry{
		IPs:             []string{"1.2.3.4"},
		UpstreamTTL:     60,
		AcquisitionTime: time.Now().Add(-100 * time.Second), // 过期 40s
	}
	c.mu.Unlock()

	// 执行清理
	c.CleanExpired()

	// 应该仍然存在
	_, ok := c.GetRaw(domain, qtype)
	assert.True(t, ok, "Entry within grace period should NOT be cleaned")

	// 2. 修改为超过 grace period (1小时 + TTL)
	c.mu.Lock()
	c.rawCache[cacheKey(domain, qtype)] = &RawCacheEntry{
		IPs:             []string{"1.2.3.4"},
		UpstreamTTL:     60,
		AcquisitionTime: time.Now().Add(-4000 * time.Second), // > 3600 + 60
	}
	c.mu.Unlock()

	// 执行清理
	c.CleanExpired()

	// 应该被删除
	_, ok = c.GetRaw(domain, qtype)
	assert.False(t, ok, "Entry exceeding grace period SHOULD be cleaned")
}

// TestCleanExpired tests the cleaning of expired entries.
func TestCleanExpired(t *testing.T) {
	c := NewCache()
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
