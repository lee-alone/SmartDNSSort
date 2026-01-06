package resolver

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestNewCache(t *testing.T) {
	// 测试创建新缓存
	cache := NewCache(100, true)

	if cache.maxSize != 100 {
		t.Errorf("expected max size 100, got %d", cache.maxSize)
	}
	if !cache.expiry {
		t.Error("expected expiry to be true")
	}
	if cache.Size() != 0 {
		t.Error("expected empty cache")
	}
}

func TestCacheSetAndGet(t *testing.T) {
	// 测试设置和获取缓存
	cache := NewCache(100, true)

	// 创建测试记录
	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{8, 8, 8, 8},
	}

	key := CacheKey("example.com.", dns.TypeA)
	cache.Set(key, []dns.RR{rr}, 5*time.Minute)

	// 获取缓存
	rrs, found := cache.Get(key)
	if !found {
		t.Error("expected to find cached record")
	}
	if len(rrs) != 1 {
		t.Errorf("expected 1 record, got %d", len(rrs))
	}
}

func TestCacheExpiry(t *testing.T) {
	// 测试缓存过期
	cache := NewCache(100, true)

	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{8, 8, 8, 8},
	}

	key := CacheKey("example.com.", dns.TypeA)
	cache.Set(key, []dns.RR{rr}, 1*time.Millisecond)

	// 立即获取应该成功
	_, found := cache.Get(key)
	if !found {
		t.Error("expected to find cached record immediately")
	}

	// 等待过期
	time.Sleep(10 * time.Millisecond)

	// 再次获取应该失败
	_, found = cache.Get(key)
	if found {
		t.Error("expected cached record to be expired")
	}
}

func TestCacheLRUEviction(t *testing.T) {
	// 测试 LRU 淘汰
	cache := NewCache(2, false)

	rr1 := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example1.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{8, 8, 8, 8},
	}

	rr2 := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example2.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{1, 1, 1, 1},
	}

	rr3 := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example3.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{2, 2, 2, 2},
	}

	key1 := CacheKey("example1.com.", dns.TypeA)
	key2 := CacheKey("example2.com.", dns.TypeA)
	key3 := CacheKey("example3.com.", dns.TypeA)

	// 添加第一个条目
	cache.Set(key1, []dns.RR{rr1}, 5*time.Minute)
	if cache.Size() != 1 {
		t.Errorf("expected cache size 1, got %d", cache.Size())
	}

	// 添加第二个条目
	cache.Set(key2, []dns.RR{rr2}, 5*time.Minute)
	if cache.Size() != 2 {
		t.Errorf("expected cache size 2, got %d", cache.Size())
	}

	// 添加第三个条目，应该淘汰第一个
	cache.Set(key3, []dns.RR{rr3}, 5*time.Minute)
	if cache.Size() != 2 {
		t.Errorf("expected cache size 2, got %d", cache.Size())
	}

	// 验证第一个条目已被淘汰
	_, found := cache.Get(key1)
	if found {
		t.Error("expected first entry to be evicted")
	}

	// 验证第二个和第三个条目仍然存在
	_, found = cache.Get(key2)
	if !found {
		t.Error("expected second entry to exist")
	}

	_, found = cache.Get(key3)
	if !found {
		t.Error("expected third entry to exist")
	}
}

func TestCacheDelete(t *testing.T) {
	// 测试删除缓存条目
	cache := NewCache(100, true)

	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{8, 8, 8, 8},
	}

	key := CacheKey("example.com.", dns.TypeA)
	cache.Set(key, []dns.RR{rr}, 5*time.Minute)

	if cache.Size() != 1 {
		t.Error("expected cache size 1")
	}

	cache.Delete(key)

	if cache.Size() != 0 {
		t.Error("expected cache size 0 after delete")
	}

	_, found := cache.Get(key)
	if found {
		t.Error("expected entry to be deleted")
	}
}

func TestCacheClear(t *testing.T) {
	// 测试清空缓存
	cache := NewCache(100, true)

	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{8, 8, 8, 8},
	}

	key := CacheKey("example.com.", dns.TypeA)
	cache.Set(key, []dns.RR{rr}, 5*time.Minute)

	if cache.Size() != 1 {
		t.Error("expected cache size 1")
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Error("expected cache size 0 after clear")
	}
}

func TestCacheCleanupExpired(t *testing.T) {
	// 测试清理过期条目
	cache := NewCache(100, true)

	rr1 := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example1.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{8, 8, 8, 8},
	}

	rr2 := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example2.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{1, 1, 1, 1},
	}

	key1 := CacheKey("example1.com.", dns.TypeA)
	key2 := CacheKey("example2.com.", dns.TypeA)

	// 添加一个短期条目和一个长期条目
	cache.Set(key1, []dns.RR{rr1}, 1*time.Millisecond)
	cache.Set(key2, []dns.RR{rr2}, 5*time.Minute)

	if cache.Size() != 2 {
		t.Error("expected cache size 2")
	}

	// 等待第一个条目过期
	time.Sleep(10 * time.Millisecond)

	// 清理过期条目
	cache.CleanupExpired()

	if cache.Size() != 1 {
		t.Errorf("expected cache size 1 after cleanup, got %d", cache.Size())
	}

	// 验证第一个条目已被清理
	_, found := cache.Get(key1)
	if found {
		t.Error("expected first entry to be cleaned up")
	}

	// 验证第二个条目仍然存在
	_, found = cache.Get(key2)
	if !found {
		t.Error("expected second entry to exist")
	}
}

func TestCacheGetStats(t *testing.T) {
	// 测试获取缓存统计信息
	cache := NewCache(100, true)

	rr := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{8, 8, 8, 8},
	}

	key := CacheKey("example.com.", dns.TypeA)
	cache.Set(key, []dns.RR{rr}, 5*time.Minute)

	stats := cache.GetStats()

	if stats["size"] != 1 {
		t.Errorf("expected size 1, got %v", stats["size"])
	}
	if stats["max_size"] != 100 {
		t.Errorf("expected max_size 100, got %v", stats["max_size"])
	}
	if stats["expiry"] != true {
		t.Errorf("expected expiry true, got %v", stats["expiry"])
	}
}

func TestCacheKey(t *testing.T) {
	// 测试缓存键生成
	key := CacheKey("example.com.", dns.TypeA)

	if key != "example.com.:A" {
		t.Errorf("expected key 'example.com.:A', got '%s'", key)
	}

	key = CacheKey("example.com.", dns.TypeMX)
	if key != "example.com.:MX" {
		t.Errorf("expected key 'example.com.:MX', got '%s'", key)
	}
}

func TestCacheUpdateExisting(t *testing.T) {
	// 测试更新现有条目
	cache := NewCache(100, true)

	rr1 := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{8, 8, 8, 8},
	}

	rr2 := &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{1, 1, 1, 1},
	}

	key := CacheKey("example.com.", dns.TypeA)

	// 设置初始值
	cache.Set(key, []dns.RR{rr1}, 5*time.Minute)
	if cache.Size() != 1 {
		t.Error("expected cache size 1")
	}

	// 更新值
	cache.Set(key, []dns.RR{rr2}, 5*time.Minute)
	if cache.Size() != 1 {
		t.Error("expected cache size 1 after update")
	}

	// 验证值已更新
	rrs, found := cache.Get(key)
	if !found {
		t.Error("expected to find cached record")
	}
	if len(rrs) != 1 {
		t.Errorf("expected 1 record, got %d", len(rrs))
	}
}
