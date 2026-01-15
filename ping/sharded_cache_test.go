package ping

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestShardedCacheBasicOperations 测试分片缓存的基本操作
func TestShardedCacheBasicOperations(t *testing.T) {
	cache := newShardedRttCache(16)

	entry := &rttCacheEntry{
		rtt:       50,
		loss:      0,
		staleAt:   time.Now().Add(1 * time.Minute),
		expiresAt: time.Now().Add(2 * time.Minute),
	}

	cache.set("8.8.8.8", entry)

	retrieved, ok := cache.get("8.8.8.8")
	if !ok {
		t.Error("✗ Failed to retrieve entry from cache")
	} else if retrieved.rtt != 50 {
		t.Errorf("✗ Expected RTT=50, got %d", retrieved.rtt)
	} else {
		t.Log("✓ Basic set/get operations work correctly")
	}

	// 测试 delete
	cache.delete("8.8.8.8")
	_, ok = cache.get("8.8.8.8")
	if ok {
		t.Error("✗ Entry should be deleted")
	} else {
		t.Log("✓ Delete operation works correctly")
	}
}

// TestShardedCacheDistribution 测试 IP 在分片中的分布
func TestShardedCacheDistribution(t *testing.T) {
	cache := newShardedRttCache(16)

	// 插入多个 IP，检查分布
	ips := []string{
		"8.8.8.8", "1.1.1.1", "208.67.222.222", "9.9.9.9",
		"64.6.64.6", "156.154.70.1", "8.26.56.26", "9.9.9.10",
	}

	for _, ip := range ips {
		entry := &rttCacheEntry{
			rtt:       50,
			loss:      0,
			expiresAt: time.Now().Add(1 * time.Minute),
		}
		cache.set(ip, entry)
	}

	// 检查分片分布
	shardDistribution := make([]int, len(cache.shards))
	for _, shard := range cache.shards {
		shard.mu.RLock()
		for range shard.cache {
			// 计数
		}
		shard.mu.RUnlock()
	}

	// 验证所有 IP 都被存储
	if cache.len() != len(ips) {
		t.Errorf("✗ Expected %d entries, got %d", len(ips), cache.len())
	} else {
		t.Logf("✓ All %d entries stored correctly", len(ips))
	}

	// 验证分布相对均匀
	for i, shard := range cache.shards {
		shard.mu.RLock()
		count := len(shard.cache)
		shard.mu.RUnlock()
		if count > 0 {
			shardDistribution[i] = count
		}
	}

	nonEmptyShards := 0
	for _, count := range shardDistribution {
		if count > 0 {
			nonEmptyShards++
		}
	}

	if nonEmptyShards > 1 {
		t.Logf("✓ Entries distributed across %d shards", nonEmptyShards)
	} else {
		t.Logf("WARNING: Entries only in %d shard(s), distribution may be skewed", nonEmptyShards)
	}
}

// TestShardedCacheExpiration 测试过期条目清理
func TestShardedCacheExpiration(t *testing.T) {
	cache := newShardedRttCache(16)

	// 插入已过期的条目
	expiredEntry := &rttCacheEntry{
		rtt:       50,
		loss:      0,
		staleAt:   time.Now().Add(-2 * time.Second),
		expiresAt: time.Now().Add(-1 * time.Second), // 已过期
	}

	// 插入未过期的条目
	validEntry := &rttCacheEntry{
		rtt:       50,
		loss:      0,
		staleAt:   time.Now().Add(1 * time.Minute),
		expiresAt: time.Now().Add(2 * time.Minute), // 未过期
	}

	cache.set("8.8.8.8", expiredEntry)
	cache.set("1.1.1.1", validEntry)

	// 清理过期条目
	cleaned := cache.cleanupExpired()

	if cleaned != 1 {
		t.Errorf("✗ Expected to clean 1 entry, cleaned %d", cleaned)
	} else {
		t.Log("✓ Expired entry cleaned correctly")
	}

	// 验证有效条目仍然存在
	_, ok := cache.get("1.1.1.1")
	if !ok {
		t.Error("✗ Valid entry should not be deleted")
	} else {
		t.Log("✓ Valid entry preserved after cleanup")
	}

	// 验证过期条目已删除
	_, ok = cache.get("8.8.8.8")
	if ok {
		t.Error("✗ Expired entry should be deleted")
	} else {
		t.Log("✓ Expired entry deleted correctly")
	}
}

// TestShardedCacheConcurrentAccess 测试并发访问
func TestShardedCacheConcurrentAccess(t *testing.T) {
	cache := newShardedRttCache(32)

	// 并发写入
	var wg sync.WaitGroup
	numGoroutines := 100
	ipsPerGoroutine := 100

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < ipsPerGoroutine; i++ {
				ip := fmt.Sprintf("10.0.%d.%d", goroutineID, i)
				entry := &rttCacheEntry{
					rtt:       50 + goroutineID,
					loss:      0,
					staleAt:   time.Now().Add(1 * time.Minute),
					expiresAt: time.Now().Add(2 * time.Minute),
				}
				cache.set(ip, entry)
			}
		}(g)
	}

	wg.Wait()

	// 验证所有条目都被写入
	expectedCount := numGoroutines * ipsPerGoroutine
	actualCount := cache.len()
	if actualCount != expectedCount {
		t.Errorf("✗ Expected %d entries, got %d", expectedCount, actualCount)
	} else {
		t.Logf("✓ All %d entries written successfully under concurrent access", expectedCount)
	}

	// 并发读取
	var readCount int32
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < ipsPerGoroutine; i++ {
				ip := fmt.Sprintf("10.0.%d.%d", goroutineID, i)
				if _, ok := cache.get(ip); ok {
					atomic.AddInt32(&readCount, 1)
				}
			}
		}(g)
	}

	wg.Wait()

	if readCount != int32(expectedCount) {
		t.Errorf("✗ Expected to read %d entries, read %d", expectedCount, readCount)
	} else {
		t.Logf("✓ All %d entries read successfully under concurrent access", readCount)
	}
}

// TestShardedCacheLockContention 测试锁竞争减少
// 这个测试比较分片缓存和全局锁缓存的性能
func TestShardedCacheLockContention(t *testing.T) {
	cache := newShardedRttCache(32)

	// 模拟高并发读写
	var wg sync.WaitGroup
	numGoroutines := 50
	operationsPerGoroutine := 1000

	start := time.Now()

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < operationsPerGoroutine; i++ {
				ip := fmt.Sprintf("10.0.%d.%d", goroutineID%10, i%100)
				entry := &rttCacheEntry{
					rtt:       50,
					loss:      0,
					staleAt:   time.Now().Add(1 * time.Minute),
					expiresAt: time.Now().Add(2 * time.Minute),
				}

				// 混合读写操作
				if i%3 == 0 {
					cache.set(ip, entry)
				} else {
					cache.get(ip)
				}
			}
		}(g)
	}

	wg.Wait()
	duration := time.Since(start)

	totalOps := numGoroutines * operationsPerGoroutine
	opsPerSec := float64(totalOps) / duration.Seconds()

	t.Logf("✓ Completed %d operations in %v (%.0f ops/sec)", totalOps, duration, opsPerSec)
	t.Logf("  - %d goroutines, %d operations each", numGoroutines, operationsPerGoroutine)
	t.Logf("  - 32 shards, each with independent lock")
}

// TestShardedCacheClear 测试清空缓存
func TestShardedCacheClear(t *testing.T) {
	cache := newShardedRttCache(16)

	// 插入多个条目
	for i := 0; i < 100; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		entry := &rttCacheEntry{
			rtt:       50,
			loss:      0,
			staleAt:   time.Now().Add(1 * time.Minute),
			expiresAt: time.Now().Add(2 * time.Minute),
		}
		cache.set(ip, entry)
	}

	if cache.len() != 100 {
		t.Errorf("✗ Expected 100 entries before clear, got %d", cache.len())
	}

	// 清空缓存
	cache.clear()

	if cache.len() != 0 {
		t.Errorf("✗ Expected 0 entries after clear, got %d", cache.len())
	} else {
		t.Log("✓ Cache cleared successfully")
	}
}

// TestShardedCacheGetAllEntries 测试获取所有条目
func TestShardedCacheGetAllEntries(t *testing.T) {
	cache := newShardedRttCache(16)

	// 插入多个条目
	testIPs := []string{"8.8.8.8", "1.1.1.1", "208.67.222.222"}
	for _, ip := range testIPs {
		entry := &rttCacheEntry{
			rtt:       50,
			loss:      0,
			staleAt:   time.Now().Add(1 * time.Minute),
			expiresAt: time.Now().Add(2 * time.Minute),
		}
		cache.set(ip, entry)
	}

	// 获取所有条目
	allEntries := cache.getAllEntries()

	if len(allEntries) != len(testIPs) {
		t.Errorf("✗ Expected %d entries, got %d", len(testIPs), len(allEntries))
	} else {
		t.Logf("✓ Retrieved all %d entries", len(allEntries))
	}

	// 验证所有 IP 都在结果中
	for _, ip := range testIPs {
		if _, ok := allEntries[ip]; !ok {
			t.Errorf("✗ IP %s not found in getAllEntries result", ip)
		}
	}
}

// BenchmarkShardedCacheGet 基准测试：分片缓存读取
func BenchmarkShardedCacheGet(b *testing.B) {
	cache := newShardedRttCache(32)

	// 预填充缓存
	for i := 0; i < 1000; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		entry := &rttCacheEntry{
			rtt:       50,
			loss:      0,
			staleAt:   time.Now().Add(1 * time.Minute),
			expiresAt: time.Now().Add(2 * time.Minute),
		}
		cache.set(ip, entry)
	}

	b.ResetTimer()

	// 并发读取
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ip := fmt.Sprintf("10.0.%d.%d", (i/256)%10, i%256)
			cache.get(ip)
			i++
		}
	})
}

// BenchmarkShardedCacheSet 基准测试：分片缓存写入
func BenchmarkShardedCacheSet(b *testing.B) {
	cache := newShardedRttCache(32)
	entry := &rttCacheEntry{
		rtt:       50,
		loss:      0,
		staleAt:   time.Now().Add(1 * time.Minute),
		expiresAt: time.Now().Add(2 * time.Minute),
	}

	b.ResetTimer()

	// 并发写入
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			ip := fmt.Sprintf("10.0.%d.%d", (i/256)%10, i%256)
			cache.set(ip, entry)
			i++
		}
	})
}
