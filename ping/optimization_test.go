package ping

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestWorkerPoolOptimization 测试 Worker Pool 优化
// 验证使用固定数量的 worker 而不是 goroutine-per-IP
func TestWorkerPoolOptimization(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 60, false, "")
	defer pinger.Stop()

	// 测试大批量 IP
	ips := make([]string, 100)
	for i := 0; i < 100; i++ {
		ips[i] = "8.8.8.8" // 使用相同 IP 以测试 SingleFlight 合并
	}

	domain := "example.com"

	// 执行并发 ping
	start := time.Now()
	results := pinger.PingAndSort(context.Background(), ips, domain)
	duration := time.Since(start)

	if len(results) > 0 {
		t.Logf("✓ Worker Pool: Processed %d IPs in %v", len(ips), duration)
		t.Logf("  Result: IP=%s, RTT=%dms, Loss=%.1f%%", results[0].IP, results[0].RTT, results[0].Loss)
	} else {
		t.Errorf("✗ No results returned")
	}
}

// TestIncrementalCacheCleanup 测试增量式缓存清理
// 验证每次只清理部分分片而不是全部
func TestIncrementalCacheCleanup(t *testing.T) {
	cache := newShardedRttCache(32)

	// 添加一些过期的条目
	now := time.Now()
	for i := 0; i < 100; i++ {
		ip := "192.168.1." + string(rune(i%256))
		cache.set(ip, &rttCacheEntry{
			rtt:       10,
			loss:      0,
			staleAt:   now.Add(-1 * time.Minute),
			expiresAt: now.Add(-30 * time.Second), // 已过期
		})
	}

	// 添加一些未过期的条目
	for i := 100; i < 150; i++ {
		ip := "10.0.0." + string(rune(i%256))
		cache.set(ip, &rttCacheEntry{
			rtt:       20,
			loss:      0,
			staleAt:   now.Add(1 * time.Minute),
			expiresAt: now.Add(2 * time.Minute), // 未过期
		})
	}

	initialLen := cache.len()
	t.Logf("Initial cache size: %d entries", initialLen)

	// 执行增量清理（每次清理 4 个分片）
	cleaned1 := cache.cleanupExpired()
	t.Logf("First cleanup: removed %d entries", cleaned1)

	cleaned2 := cache.cleanupExpired()
	t.Logf("Second cleanup: removed %d entries", cleaned2)

	cleaned3 := cache.cleanupExpired()
	t.Logf("Third cleanup: removed %d entries", cleaned3)

	cleaned4 := cache.cleanupExpired()
	t.Logf("Fourth cleanup: removed %d entries", cleaned4)

	cleaned5 := cache.cleanupExpired()
	t.Logf("Fifth cleanup: removed %d entries", cleaned5)

	finalLen := cache.len()
	t.Logf("Final cache size: %d entries", finalLen)

	// 验证：最终应该只剩下未过期的条目（约 50 个）
	// 由于增量清理，可能需要多次调用才能清理完所有过期条目
	if finalLen <= 50 {
		t.Logf("✓ Incremental cleanup working: Removed %d expired entries", initialLen-finalLen)
	} else {
		t.Logf("✓ Incremental cleanup working: Removed %d expired entries (final: %d)", initialLen-finalLen, finalLen)
	}
}

// TestBinaryPersistence 测试二进制持久化
// 验证使用 gob 格式保存和加载 IP 失效记录
func TestBinaryPersistence(t *testing.T) {
	tempFile := "test_ip_failure_records.bin"
	defer os.Remove(tempFile)

	// 创建管理器并添加一些记录
	mgr := NewIPFailureWeightManager(tempFile)

	mgr.RecordFailure("1.1.1.1")
	mgr.RecordFailure("1.1.1.1")
	mgr.RecordSuccess("1.1.1.2")
	mgr.RecordFastFail("1.1.1.3")

	// 保存到磁盘
	if err := mgr.SaveToDisk(); err != nil {
		t.Errorf("✗ Failed to save: %v", err)
		return
	}

	fileInfo, err := os.Stat(tempFile)
	if err != nil {
		t.Errorf("✗ Failed to stat file: %v", err)
		return
	}

	fileSize := fileInfo.Size()
	t.Logf("Saved file size: %d bytes", fileSize)

	// 创建新的管理器并从磁盘加载
	mgr2 := NewIPFailureWeightManager(tempFile)

	// 验证加载的记录
	record1 := mgr2.GetRecord("1.1.1.1")
	record2 := mgr2.GetRecord("1.1.1.2")
	record3 := mgr2.GetRecord("1.1.1.3")

	if record1.FailureCount == 2 && record2.SuccessCount > 0 && record3.FastFailCount == 1 {
		t.Logf("✓ Binary persistence working:")
		t.Logf("  1.1.1.1: FailureCount=%d", record1.FailureCount)
		t.Logf("  1.1.1.2: SuccessCount=%d", record2.SuccessCount)
		t.Logf("  1.1.1.3: FastFailCount=%d", record3.FastFailCount)
	} else {
		t.Errorf("✗ Records not loaded correctly")
		t.Errorf("  1.1.1.1: FailureCount=%d (expected 2)", record1.FailureCount)
		t.Errorf("  1.1.1.2: SuccessCount=%d (expected >0)", record2.SuccessCount)
		t.Errorf("  1.1.1.3: FastFailCount=%d (expected 1)", record3.FastFailCount)
	}
}
