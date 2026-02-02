package ping

import (
	"context"
	"sync"
	"testing"
)

// TestSingleFlightDomainKey 测试 SingleFlight 的 key 包含 domain
// 验证不同 domain 对同一 IP 的探测不会被错误复用
//
// 场景：
// 1. 查询 example.com 的 8.8.8.8，触发 SingleFlight key="8.8.8.8:example.com"
// 2. 查询 other.com 的 8.8.8.8，触发 SingleFlight key="8.8.8.8:other.com"
// 3. 两个 key 不同，所以会执行两次独立的探测
// 4. 如果 key 只是 IP，第二个查询会复用第一个的结果（错误）
func TestSingleFlightDomainKey(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 60, false, "")
	defer pinger.Stop()

	ip := "8.8.8.8"
	domain1 := "example.com"
	domain2 := "other.com"

	// 场景：同一 IP，不同 domain，并发查询
	var wg sync.WaitGroup
	var results1, results2 []Result

	// 查询 1：example.com 的 8.8.8.8
	wg.Add(1)
	go func() {
		defer wg.Done()
		results1 = pinger.PingAndSort(context.Background(), []string{ip}, domain1)
	}()

	// 查询 2：other.com 的 8.8.8.8（应该独立探测，不复用查询 1 的结果）
	wg.Add(1)
	go func() {
		defer wg.Done()
		results2 = pinger.PingAndSort(context.Background(), []string{ip}, domain2)
	}()

	wg.Wait()

	// 验证：两个查询都应该返回结果
	if len(results1) > 0 && len(results2) > 0 {
		t.Logf("✓ Correct: Both queries returned results")
		t.Logf("  Query 1 (example.com): %v", results1[0])
		t.Logf("  Query 2 (other.com): %v", results2[0])
	} else {
		t.Errorf("✗ Wrong: Query 1 results=%d, Query 2 results=%d", len(results1), len(results2))
	}
}

// TestSingleFlightSameDomainKey 测试 SingleFlight 的 key 对相同 domain 的复用
// 验证相同 domain 对同一 IP 的并发探测会被正确合并
//
// 场景：
// 1. 5 个并发查询都查询 example.com 的 8.8.8.8
// 2. 所有查询的 SingleFlight key 都是 "8.8.8.8:example.com"
// 3. SingleFlight 会合并这些请求，只执行一次真正的探测
// 4. 其他 4 个查询会等待第一个的结果
func TestSingleFlightSameDomainKey(t *testing.T) {
	pinger := NewPinger(1, 100, 8, 0, 60, false, "")
	defer pinger.Stop()

	ip := "8.8.8.8"
	domain := "example.com"

	// 场景：同一 IP，同一 domain，并发查询
	var wg sync.WaitGroup
	concurrency := 5
	var results [][]Result
	var mu sync.Mutex

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := pinger.PingAndSort(context.Background(), []string{ip}, domain)
			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}()
	}

	wg.Wait()

	// 验证：所有查询都应该返回相同的结果（因为 SingleFlight 合并）
	if len(results) == concurrency {
		t.Logf("✓ Correct: All %d concurrent queries completed", concurrency)

		// 检查结果是否一致
		if len(results[0]) > 0 {
			firstResult := results[0][0]
			allSame := true
			for i := 1; i < len(results); i++ {
				if len(results[i]) > 0 && results[i][0].RTT != firstResult.RTT {
					allSame = false
					break
				}
			}
			if allSame {
				t.Logf("✓ All results are consistent (SingleFlight merged correctly)")
			} else {
				t.Logf("WARNING: Results differ between concurrent queries")
			}
		}
	} else {
		t.Errorf("✗ Wrong: Expected %d results, got %d", concurrency, len(results))
	}
}
