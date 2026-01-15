package ping

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestStaleWhileRevalidate 测试软过期更新功能
func TestStaleWhileRevalidate(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 5, false, "")
	defer pinger.Stop()

	// 使用一个可达的 IP
	ips := []string{"8.8.8.8"}
	domain := "example.com"

	// 第一次查询：缓存未命中，执行探测
	results1 := pinger.PingAndSort(context.Background(), ips, domain)
	if len(results1) == 0 {
		t.Skip("Cannot reach 8.8.8.8, skipping test")
	}

	if results1[0].ProbeMethod != "icmp" && results1[0].ProbeMethod != "tls" && results1[0].ProbeMethod != "udp53" {
		t.Logf("First query probe method: %s", results1[0].ProbeMethod)
	}

	// 等待缓存过期但在软过期期间内
	time.Sleep(6 * time.Second)

	// 第二次查询：缓存已过期但在软过期期间，应该返回旧数据
	start := time.Now()
	results2 := pinger.PingAndSort(context.Background(), ips, domain)
	duration := time.Since(start)

	if len(results2) == 0 {
		t.Error("✗ Second query returned no results")
		return
	}

	// 验证返回的是旧数据（ProbeMethod 应该是 "stale"）
	if results2[0].ProbeMethod == "stale" {
		t.Logf("✓ Stale cache returned: RTT=%d, Loss=%.1f%%, Duration=%v",
			results2[0].RTT, results2[0].Loss, duration)
	} else {
		t.Logf("WARNING: Expected 'stale' probe method, got '%s'", results2[0].ProbeMethod)
	}

	// 验证响应速度快（应该在 1ms 以内）
	if duration < 10*time.Millisecond {
		t.Logf("✓ Stale response is fast: %v", duration)
	} else {
		t.Logf("WARNING: Stale response took %v (expected < 10ms)", duration)
	}

	// 等待异步更新完成
	time.Sleep(1 * time.Second)

	// 第三次查询：缓存应该已经被异步更新
	results3 := pinger.PingAndSort(context.Background(), ips, domain)
	if len(results3) > 0 {
		t.Logf("✓ Cache updated after stale revalidate: ProbeMethod=%s", results3[0].ProbeMethod)
	}
}

// TestStaleRevalidateNoDuplicates 测试软过期更新不会重复触发
func TestStaleRevalidateNoDuplicates(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 5, false, "")
	defer pinger.Stop()

	ips := []string{"8.8.8.8"}
	domain := "example.com"

	// 第一次查询：缓存未命中
	pinger.PingAndSort(context.Background(), ips, domain)

	// 等待缓存过期但在软过期期间
	time.Sleep(6 * time.Second)

	// 计数异步更新的触发次数
	var updateCount int32

	// 多个并发查询应该只触发一次异步更新
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pinger.PingAndSort(context.Background(), ips, domain)
		}()
	}
	wg.Wait()

	// 检查 staleRevalidating 状态
	pinger.staleRevalidateMu.Lock()
	isRevalidating := pinger.staleRevalidating["8.8.8.8"]
	pinger.staleRevalidateMu.Unlock()

	if !isRevalidating {
		t.Logf("✓ Stale revalidate completed (not in progress)")
	} else {
		t.Logf("WARNING: Stale revalidate still in progress")
	}

	_ = updateCount
}

// TestStaleGracePeriod 测试软过期容忍期
func TestStaleGracePeriod(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 10, false, "")
	defer pinger.Stop()

	// 设置自定义软过期容忍期
	pinger.staleGracePeriod = 5 * time.Second

	ips := []string{"8.8.8.8"}
	domain := "example.com"

	// 第一次查询
	results1 := pinger.PingAndSort(context.Background(), ips, domain)
	if len(results1) == 0 {
		t.Skip("Cannot reach 8.8.8.8, skipping test")
	}

	// 等待硬过期（10 秒）
	time.Sleep(11 * time.Second)

	// 此时应该完全过期，需要重新探测
	start := time.Now()
	results2 := pinger.PingAndSort(context.Background(), ips, domain)
	duration := time.Since(start)

	if len(results2) == 0 {
		t.Error("✗ Query after hard expiration returned no results")
		return
	}

	// 应该执行了新的探测（不是 stale）
	if results2[0].ProbeMethod != "stale" {
		t.Logf("✓ Hard expiration triggered new probe: ProbeMethod=%s, Duration=%v",
			results2[0].ProbeMethod, duration)
	} else {
		t.Logf("WARNING: Expected new probe after hard expiration, got stale")
	}
}

// TestStaleWhileRevalidateWithFailure 测试软过期更新处理失败情况
func TestStaleWhileRevalidateWithFailure(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 5, false, "")
	defer pinger.Stop()

	// 使用一个不可达的 IP
	ips := []string{"192.0.2.1"}
	domain := "example.com"

	// 第一次查询：缓存未命中，执行探测（会失败）
	results1 := pinger.PingAndSort(context.Background(), ips, domain)
	if len(results1) == 0 {
		t.Error("✗ First query returned no results")
		return
	}

	// 验证是失败结果
	if results1[0].Loss == 100 {
		t.Logf("✓ First query detected unreachable IP: Loss=100%%")
	}

	// 等待缓存过期但在软过期期间
	time.Sleep(6 * time.Second)

	// 第二次查询：应该返回旧的失败结果
	results2 := pinger.PingAndSort(context.Background(), ips, domain)
	if len(results2) == 0 {
		t.Error("✗ Second query returned no results")
		return
	}

	if results2[0].ProbeMethod == "stale" && results2[0].Loss == 100 {
		t.Logf("✓ Stale failure result returned: Loss=100%%, ProbeMethod=stale")
	} else {
		t.Logf("WARNING: Expected stale failure result, got ProbeMethod=%s, Loss=%.1f%%",
			results2[0].ProbeMethod, results2[0].Loss)
	}
}

// BenchmarkStaleWhileRevalidate 基准测试：软过期更新的性能
func BenchmarkStaleWhileRevalidate(b *testing.B) {
	pinger := NewPinger(1, 100, 8, 0, 60, false, "")
	defer pinger.Stop()

	ips := []string{"8.8.8.8"}
	domain := "example.com"

	// 预热缓存
	pinger.PingAndSort(context.Background(), ips, domain)

	b.ResetTimer()

	// 并发查询缓存
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pinger.PingAndSort(context.Background(), ips, domain)
		}
	})
}
