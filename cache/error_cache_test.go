package cache

import (
	"smartdnssort/config"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestErrorCacheStorage 测试错误缓存存储
func getDefaultCacheConfig() *config.CacheConfig {
	return &config.CacheConfig{
		MinTTLSeconds:        3600,
		MaxTTLSeconds:        84600,
		NegativeTTLSeconds:   300,
		ErrorCacheTTL:        30,
		FastResponseTTL:      15,
		UserReturnTTL:        600,
		MaxMemoryMB:          500, // Default value for tests
		KeepExpiredEntries:   false,
		EvictionThreshold:    0.9,
		EvictionBatchPercent: 0.1,
		ProtectPrefetchDomains: false,
	}
}

// TestErrorCacheStorage 测试错误缓存存储
func TestErrorCacheStorage(t *testing.T) {
	c := NewCache(getDefaultCacheConfig())
	domain := "error.example.com"
	qtype := dns.TypeA
	rcode := dns.RcodeServerFailure
	ttl := 30

	// 设置错误缓存
	c.SetError(domain, qtype, rcode, ttl)

	// 验证可以获取错误缓存
	entry, ok := c.GetError(domain, qtype)
	if !ok {
		t.Fatal("Expected error cache to exist")
	}

	if entry.Rcode != rcode {
		t.Errorf("Expected rcode %d, got %d", rcode, entry.Rcode)
	}

	if entry.TTL != ttl {
		t.Errorf("Expected TTL %d, got %d", ttl, entry.TTL)
	}
}

// TestErrorCacheExpiration 测试错误缓存过期
func TestErrorCacheExpiration(t *testing.T) {
	c := NewCache(getDefaultCacheConfig())
	domain := "expired.error.com"
	qtype := dns.TypeA
	rcode := dns.RcodeRefused
	ttl := 1 // 1秒TTL

	// 设置错误缓存
	c.SetError(domain, qtype, rcode, ttl)

	// 立即获取应该成功
	entry, ok := c.GetError(domain, qtype)
	if !ok {
		t.Fatal("Expected error cache to exist immediately after setting")
	}

	if entry.Rcode != rcode {
		t.Errorf("Expected rcode %d, got %d", rcode, entry.Rcode)
	}

	// 等待超过TTL
	time.Sleep(1100 * time.Millisecond)

	// 现在应该过期
	_, ok = c.GetError(domain, qtype)
	if ok {
		t.Error("Expected error cache to be expired")
	}
}

// TestErrorCacheCleanup 测试错误缓存清理
func TestErrorCacheCleanup(t *testing.T) {
	c := NewCache(getDefaultCacheConfig())
	domain := "cleanup.error.com"
	qtype := dns.TypeA
	rcode := dns.RcodeServerFailure
	ttl := 1 // 1秒TTL

	// 设置错误缓存
	c.SetError(domain, qtype, rcode, ttl)

	// 验证存在
	_, ok := c.GetError(domain, qtype)
	if !ok {
		t.Fatal("Expected error cache to exist")
	}

	// 等待过期
	time.Sleep(1100 * time.Millisecond)

	// 执行清理
	c.CleanExpired()

	// 验证已被清理(内部检查)
	c.mu.RLock()
	key := cacheKey(domain, qtype)
	_, exists := c.errorCache[key]
	c.mu.RUnlock()

	if exists {
		t.Error("Expected expired error cache to be cleaned up")
	}
}

// TestErrorCacheClear 测试清空错误缓存
func TestErrorCacheClear(t *testing.T) {
	c := NewCache(getDefaultCacheConfig())
	domain := "clear.error.com"
	qtype := dns.TypeA
	rcode := dns.RcodeServerFailure
	ttl := 300

	// 设置错误缓存
	c.SetError(domain, qtype, rcode, ttl)

	// 验证存在
	_, ok := c.GetError(domain, qtype)
	if !ok {
		t.Fatal("Expected error cache to exist")
	}

	// 清空所有缓存
	c.Clear()

	// 验证错误缓存已被清空
	_, ok = c.GetError(domain, qtype)
	if ok {
		t.Error("Expected error cache to be cleared")
	}
}

// TestErrorCacheMultipleDomains 测试多个域名的错误缓存
func TestErrorCacheMultipleDomains(t *testing.T) {
	c := NewCache(getDefaultCacheConfig())

	domains := []string{"error1.com", "error2.com", "error3.com"}
	rcodes := []int{dns.RcodeServerFailure, dns.RcodeRefused, dns.RcodeServerFailure}
	ttl := 30

	// 为多个域名设置错误缓存
	for i, domain := range domains {
		c.SetError(domain, dns.TypeA, rcodes[i], ttl)
	}

	// 验证所有域名的错误缓存都存在且正确
	for i, domain := range domains {
		entry, ok := c.GetError(domain, dns.TypeA)
		if !ok {
			t.Errorf("Expected error cache for %s to exist", domain)
			continue
		}

		if entry.Rcode != rcodes[i] {
			t.Errorf("Expected rcode %d for %s, got %d", rcodes[i], domain, entry.Rcode)
		}
	}
}

// TestErrorCacheIsExpiredMethod 测试ErrorCacheEntry.IsExpired方法
func TestErrorCacheIsExpiredMethod(t *testing.T) {
	// 创建一个已过期的条目
	expiredEntry := &ErrorCacheEntry{
		Rcode:    dns.RcodeServerFailure,
		CachedAt: time.Now().Add(-60 * time.Second), // 60秒前
		TTL:      30,                                // 30秒TTL
	}

	if !expiredEntry.IsExpired() {
		t.Error("Expected entry to be expired")
	}

	// 创建一个未过期的条目
	validEntry := &ErrorCacheEntry{
		Rcode:    dns.RcodeServerFailure,
		CachedAt: time.Now(),
		TTL:      300,
	}

	if validEntry.IsExpired() {
		t.Error("Expected entry to not be expired")
	}
}

