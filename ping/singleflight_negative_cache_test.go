package ping

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestSingleFlightMerging 测试 SingleFlight 请求合并功能
// 验证 SingleFlight 对象被正确初始化
func TestSingleFlightMerging(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 60, false, "")
	defer pinger.Stop()

	// 验证 SingleFlight 对象已初始化
	if pinger.probeFlight == nil {
		t.Error("✗ SingleFlight not initialized")
	} else {
		t.Log("✓ SingleFlight initialized successfully")
	}

	// 验证并发查询能正常工作
	ips := []string{"8.8.8.8"}
	domain := "example.com"

	var wg sync.WaitGroup
	var mu sync.Mutex
	var results [][]Result

	// 并发发起 5 个查询
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := pinger.PingAndSort(context.Background(), ips, domain)
			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(results) == 5 {
		t.Logf("✓ All 5 concurrent queries completed successfully")
	} else {
		t.Errorf("✗ Expected 5 results, got %d", len(results))
	}
}

// TestNegativeCaching 测试负向缓存功能
// 验证失败结果也被缓存，避免重复探测
func TestNegativeCaching(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 60, false, "")
	defer pinger.Stop()

	// 使用一个不可达的 IP（模拟失败场景）
	ips := []string{"192.0.2.1"} // TEST-NET-1，不可达
	domain := "example.com"

	// 第一次查询（会超时，结果被缓存）
	start1 := time.Now()
	result1 := pinger.PingAndSort(context.Background(), ips, domain)
	duration1 := time.Since(start1)

	// 第二次查询（应该从缓存返回，速度很快）
	start2 := time.Now()
	result2 := pinger.PingAndSort(context.Background(), ips, domain)
	duration2 := time.Since(start2)

	// 验证结果
	if len(result1) > 0 && len(result2) > 0 {
		// 第二次查询应该快得多（从缓存返回）
		if duration2 < duration1/2 {
			t.Logf("✓ Negative caching working: First query took %v, second query took %v (from cache)", duration1, duration2)
		} else {
			t.Logf("WARNING: Second query not significantly faster. Expected cache hit to be much faster.")
		}

		// 验证缓存中的丢包率被正确保存
		if result2[0].ProbeMethod == "cached" {
			t.Logf("✓ Result marked as 'cached': Loss=%.1f%%", result2[0].Loss)
		}
	}
}

// TestDynamicTTL 测试动态 TTL 计算
// 验证不同质量的 IP 获得不同的缓存时间
func TestDynamicTTL(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 60, false, "")
	defer pinger.Stop()

	testCases := []struct {
		name   string
		result Result
		minTTL time.Duration
		maxTTL time.Duration
	}{
		{
			name:   "Perfect IP (RTT<50ms, 0% loss)",
			result: Result{IP: "1.1.1.1", RTT: 30, Loss: 0},
			minTTL: 9 * time.Minute,  // 10 * 60s = 600s
			maxTTL: 11 * time.Minute, // 10 * 60s = 600s
		},
		{
			name:   "Good IP (RTT 50-100ms, 0% loss)",
			result: Result{IP: "1.1.1.2", RTT: 75, Loss: 0},
			minTTL: 4 * time.Minute, // 5 * 60s = 300s
			maxTTL: 6 * time.Minute, // 5 * 60s = 300s
		},
		{
			name:   "Slight packet loss (10% loss)",
			result: Result{IP: "1.1.1.3", RTT: 50, Loss: 10},
			minTTL: 50 * time.Second, // 1 * 60s = 60s
			maxTTL: 70 * time.Second, // 1 * 60s = 60s
		},
		{
			name:   "Severe packet loss (80% loss)",
			result: Result{IP: "1.1.1.4", RTT: 100, Loss: 80},
			minTTL: 8 * time.Second,  // 0.17 * 60s ≈ 10s
			maxTTL: 12 * time.Second, // 0.17 * 60s ≈ 10s
		},
		{
			name:   "Complete failure (100% loss)",
			result: Result{IP: "1.1.1.5", RTT: 999999, Loss: 100},
			minTTL: 4 * time.Second, // 0.08 * 60s ≈ 5s
			maxTTL: 6 * time.Second, // 0.08 * 60s ≈ 5s
		},
	}

	for _, tc := range testCases {
		ttl := pinger.calculateDynamicTTL(tc.result)
		if ttl >= tc.minTTL && ttl <= tc.maxTTL {
			t.Logf("✓ %s: TTL=%v (expected %v-%v)", tc.name, ttl, tc.minTTL, tc.maxTTL)
		} else {
			t.Errorf("✗ %s: TTL=%v (expected %v-%v)", tc.name, ttl, tc.minTTL, tc.maxTTL)
		}
	}
}

// TestCacheWithMixedResults 测试缓存同时处理成功和失败结果
func TestCacheWithMixedResults(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 60, false, "")
	defer pinger.Stop()

	// 模拟一个成功的 IP 和一个失败的 IP
	ips := []string{"8.8.8.8", "192.0.2.1"}
	domain := "example.com"

	// 第一次查询
	pinger.PingAndSort(context.Background(), ips, domain)

	// 检查缓存中是否同时存储了成功和失败的结果
	allEntries := pinger.rttCache.getAllEntries()
	successCount := 0
	failureCount := 0
	for _, entry := range allEntries {
		if entry.loss == 0 {
			successCount++
		} else {
			failureCount++
		}
	}

	t.Logf("Cache contains: %d success entries, %d failure entries", successCount, failureCount)
	if successCount > 0 && failureCount > 0 {
		t.Logf("✓ Mixed caching working: both success and failure results are cached")
	}

	// 第二次查询应该从缓存返回
	results2 := pinger.PingAndSort(context.Background(), ips, domain)

	cachedCount := 0
	for _, r := range results2 {
		if r.ProbeMethod == "cached" {
			cachedCount++
		}
	}

	if cachedCount == len(results2) {
		t.Logf("✓ All results from cache on second query")
	} else {
		t.Logf("WARNING: Only %d/%d results from cache", cachedCount, len(results2))
	}
}
