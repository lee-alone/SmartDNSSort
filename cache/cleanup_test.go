package cache

import (
	"testing"
	"time"

	"smartdnssort/config"
)

// TestCleanupStrategy 测试混合动力清理策略
func TestCleanupStrategy(t *testing.T) {
	cfg := &config.CacheConfig{
		MaxTTLSeconds: 3600,
	}
	c := NewCache(cfg)
	defer c.Close()

	// 添加一些缓存数据
	domain1 := "example.com"
	domain2 := "test.com"

	// 添加新鲜数据（TTL = 300秒）
	c.SetRaw(domain1, 1, []string{"1.1.1.1"}, nil, 300)

	// 添加即将过期的数据（TTL = 100秒）
	c.SetRaw(domain2, 1, []string{"2.2.2.2"}, nil, 100)

	// 验证数据已添加
	if entry, ok := c.GetRaw(domain1, 1); !ok || entry == nil {
		t.Error("Failed to get fresh data")
	}

	if entry, ok := c.GetRaw(domain2, 1); !ok || entry == nil {
		t.Error("Failed to get stale data")
	}

	// 验证堆已更新（给异步 worker 一点时间）
	time.Sleep(100 * time.Millisecond)
	if len(c.expiredHeap) != 2 {
		t.Errorf("Expected 2 entries in heap, got %d", len(c.expiredHeap))
	}

	// 测试清理逻辑
	c.CleanExpired()

	// 由于数据还没过期，不应该被删除
	if entry, ok := c.GetRaw(domain1, 1); !ok || entry == nil {
		t.Error("Fresh data should not be deleted")
	}

	t.Logf("Cleanup test passed. Heap size: %d", len(c.expiredHeap))
}

// TestHeapParsing 测试堆条目的解析
func TestHeapParsing(t *testing.T) {
	key := "example.com#1"
	expiryTime := int64(1234567890)

	entry := expireEntry{key: key, expiry: expiryTime}

	if entry.key != key {
		t.Errorf("Expected key %s, got %s", key, entry.key)
	}

	if entry.expiry != expiryTime {
		t.Errorf("Expected expiry %d, got %d", expiryTime, entry.expiry)
	}
}

// TestStaleDataHandling 测试 Stale 数据的处理
func TestStaleDataHandling(t *testing.T) {
	cfg := &config.CacheConfig{
		MaxTTLSeconds: 3600,
	}
	c := NewCache(cfg)
	defer c.Close()

	domain := "example.com"

	// 添加 TTL 很短的数据
	c.SetRaw(domain, 1, []string{"1.1.1.1"}, nil, 1)

	// 等待数据过期
	time.Sleep(2 * time.Second)

	// 数据应该仍然存在（LRU 还没删除）
	if entry, ok := c.GetRaw(domain, 1); !ok || entry == nil {
		t.Error("Stale data should still exist in cache")
	}

	// 但 IsExpired 应该返回 true
	if entry, ok := c.GetRaw(domain, 1); ok && entry != nil {
		if !entry.IsExpired() {
			t.Error("Stale data should be marked as expired")
		}
	}
}
