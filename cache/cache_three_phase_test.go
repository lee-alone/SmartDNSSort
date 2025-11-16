package cache

import (
	"testing"
	"time"
)

// TestThreePhaseCache 测试三阶段缓存逻辑
// 阶段一：首次查询（无缓存）
// 阶段二：排序完成后缓存命中
// 阶段三：缓存过期后再次访问
func TestThreePhaseCache(t *testing.T) {
	cache := NewCache()
	domain := "example.com"
	qtype := uint16(1) // DNS TypeA

	// ========== 阶段一：首次查询（无缓存）==========
	t.Run("Phase1-FirstQuery", func(t *testing.T) {
		// 验证首次查询时，缓存不存在
		if _, ok := cache.GetSorted(domain, qtype); ok {
			t.Error("预期首次查询时排序缓存不存在")
		}

		if _, ok := cache.GetRaw(domain, qtype); ok {
			t.Error("预期首次查询时原始缓存不存在")
		}

		// 缓存未命中计数
		_, misses := cache.GetStats()
		if misses != 0 {
			t.Errorf("预期缓存未命中计数为 0，实际为 %d", misses)
		}

		// 设置原始缓存（模拟上游 DNS 响应）
		ips := []string{"1.1.1.1", "8.8.8.8"}
		ttl := uint32(300) // 5 分钟
		cache.SetRaw(domain, qtype, ips, ttl)

		// 验证原始缓存已设置
		if raw, ok := cache.GetRaw(domain, qtype); !ok {
			t.Error("原始缓存应该存在")
		} else {
			if len(raw.IPs) != 2 || raw.IPs[0] != "1.1.1.1" {
				t.Errorf("原始缓存 IP 列表不匹配")
			}
			if raw.TTL != ttl {
				t.Errorf("原始缓存 TTL 不匹配，预期 %d，实际 %d", ttl, raw.TTL)
			}
		}
	})

	// ========== 阶段二：排序完成后缓存命中 ==========
	t.Run("Phase2-SortedCacheHit", func(t *testing.T) {
		// 设置排序缓存（模拟排序完成后的结果）
		sortedEntry := &SortedCacheEntry{
			IPs:       []string{"8.8.8.8", "1.1.1.1"}, // 排序后的 IP
			RTTs:      []int{50, 100},                 // 对应的 RTT
			Timestamp: time.Now(),
			TTL:       600, // 10 分钟
			IsValid:   true,
		}
		cache.SetSorted(domain, qtype, sortedEntry)

		// 验证排序缓存命中
		if sorted, ok := cache.GetSorted(domain, qtype); !ok {
			t.Error("排序缓存应该存在")
		} else {
			if len(sorted.IPs) != 2 || sorted.IPs[0] != "8.8.8.8" {
				t.Errorf("排序缓存 IP 列表不匹配")
			}
			if len(sorted.RTTs) != 2 || sorted.RTTs[0] != 50 {
				t.Errorf("排序缓存 RTT 不匹配")
			}
			if !sorted.IsValid {
				t.Error("排序缓存应该有效")
			}
		}

		// 验证通过 Get 方法也能获取排序缓存
		if entry, ok := cache.Get(domain, qtype); !ok {
			t.Error("Get 方法应该返回排序缓存")
		} else {
			if entry.IPs[0] != "8.8.8.8" {
				t.Errorf("Get 方法应该返回排序后的 IP")
			}
		}
	})

	// ========== 阶段三：缓存过期后再次访问 ==========
	t.Run("Phase3-ExpiredCacheRefresh", func(t *testing.T) {
		// 设置一个已过期的排序缓存
		expiredEntry := &SortedCacheEntry{
			IPs:       []string{"8.8.8.8", "1.1.1.1"},
			RTTs:      []int{50, 100},
			Timestamp: time.Now().Add(-time.Hour), // 1 小时前
			TTL:       60,                         // 1 分钟 TTL，已过期
			IsValid:   true,
		}
		cache.SetSorted(domain, qtype, expiredEntry)

		// 验证排序缓存已过期
		if _, ok := cache.GetSorted(domain, qtype); ok {
			t.Error("过期的排序缓存应该返回 false")
		}

		// 验证原始缓存仍然有效（原始缓存的 TTL 更长：300秒）
		if raw, ok := cache.GetRaw(domain, qtype); !ok {
			t.Error("原始缓存应该仍然有效（未过期）")
		} else {
			t.Logf("原始缓存仍然有效，IPs: %v, TTL: %d", raw.IPs, raw.TTL)
		}
	})
}

// TestSortingState 测试排序状态管理
func TestSortingState(t *testing.T) {
	cache := NewCache()
	domain := "test.com"
	qtype := uint16(1)

	// 获取或创建排序状态
	state1, isNew1 := cache.GetOrStartSort(domain, qtype)
	if !isNew1 {
		t.Error("首次调用应该返回 isNew=true")
	}
	if state1 == nil {
		t.Error("排序状态不应该为 nil")
	}

	// 再次调用应该返回同一个状态，isNew=false
	state2, isNew2 := cache.GetOrStartSort(domain, qtype)
	if isNew2 {
		t.Error("第二次调用应该返回 isNew=false")
	}
	if state1 != state2 {
		t.Error("应该返回同一个排序状态对象")
	}

	// 完成排序
	result := &SortedCacheEntry{
		IPs:       []string{"1.1.1.1"},
		RTTs:      []int{50},
		Timestamp: time.Now(),
		TTL:       300,
		IsValid:   true,
	}
	cache.FinishSort(domain, qtype, result, nil)

	// 验证排序完成信号
	select {
	case <-state1.Done:
		t.Logf("排序完成信号已发送")
	case <-time.After(100 * time.Millisecond):
		t.Error("应该收到排序完成信号")
	}

	// 清理排序状态
	cache.ClearSort(domain, qtype)
}

// TestConcurrentCacheAccess 测试并发缓存访问
func TestConcurrentCacheAccess(t *testing.T) {
	cache := NewCache()
	domain := "concurrent.com"
	qtype := uint16(1)

	// 并发读写缓存
	done := make(chan bool, 10)

	// 并发写入
	for i := 0; i < 5; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				entry := &SortedCacheEntry{
					IPs:       []string{"1.1.1.1", "8.8.8.8"},
					RTTs:      []int{50, 100},
					Timestamp: time.Now(),
					TTL:       300,
					IsValid:   true,
				}
				cache.SetSorted(domain, qtype, entry)
			}
			done <- true
		}(i)
	}

	// 并发读取
	for i := 0; i < 5; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				if _, ok := cache.GetSorted(domain, qtype); !ok {
					// 缓存可能还未设置，不算错误
				}
			}
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	t.Log("并发访问测试通过")
}

// TestCacheExpiry 测试缓存过期检测
func TestCacheExpiry(t *testing.T) {
	cache := NewCache()
	domain := "expire.com"
	qtype := uint16(1)

	// 设置一个很短 TTL 的缓存
	entry := &SortedCacheEntry{
		IPs:       []string{"1.1.1.1"},
		RTTs:      []int{50},
		Timestamp: time.Now().Add(-100 * time.Millisecond), // 100ms 前
		TTL:       1,                                       // 1 秒 TTL
		IsValid:   true,
	}
	cache.SetSorted(domain, qtype, entry)

	// 立即读取应该成功
	if _, ok := cache.GetSorted(domain, qtype); !ok {
		t.Error("新设置的缓存应该有效")
	}

	// 等待缓存过期
	time.Sleep(1100 * time.Millisecond)

	// 过期后应该返回 false
	if _, ok := cache.GetSorted(domain, qtype); ok {
		t.Error("过期的缓存应该返回 false")
	}
}

// TestCleanExpired 测试清理过期缓存
func TestCleanExpired(t *testing.T) {
	cache := NewCache()

	// 添加一些缓存
	for i := 0; i < 5; i++ {
		domain := "test" + string(rune(i)) + ".com"
		qtype := uint16(1)

		// 混合过期和未过期的缓存
		var ttl int
		if i%2 == 0 {
			ttl = 1 // 短 TTL，会过期
		} else {
			ttl = 3600 // 长 TTL，不会过期
		}

		entry := &SortedCacheEntry{
			IPs:       []string{"1.1.1.1"},
			RTTs:      []int{50},
			Timestamp: time.Now(),
			TTL:       ttl,
			IsValid:   true,
		}
		cache.SetSorted(domain, qtype, entry)
	}

	// 等待过期缓存到期
	time.Sleep(1100 * time.Millisecond)

	// 清理过期缓存
	cache.CleanExpired()

	// 验证过期缓存已删除
	for i := 0; i < 5; i++ {
		domain := "test" + string(rune(i)) + ".com"
		qtype := uint16(1)

		_, exists := cache.GetSorted(domain, qtype)
		if i%2 == 0 {
			if exists {
				t.Errorf("过期缓存应该被删除: %s", domain)
			}
		} else {
			if !exists {
				t.Errorf("未过期缓存应该保留: %s", domain)
			}
		}
	}

	t.Log("清理过期缓存测试通过")
}

// TestRawCacheLayer 测试原始缓存层
func TestRawCacheLayer(t *testing.T) {
	cache := NewCache()
	domain := "raw.com"
	qtype := uint16(1)

	// 设置原始缓存
	ips := []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}
	ttl := uint32(600)
	cache.SetRaw(domain, qtype, ips, ttl)

	// 验证原始缓存
	if raw, ok := cache.GetRaw(domain, qtype); !ok {
		t.Error("原始缓存应该存在")
	} else {
		if len(raw.IPs) != 3 {
			t.Errorf("原始缓存 IP 数量不匹配，预期 3，实际 %d", len(raw.IPs))
		}
		for i, ip := range raw.IPs {
			if ip != ips[i] {
				t.Errorf("原始缓存 IP 不匹配，位置 %d", i)
			}
		}
	}

	// 设置排序缓存后，Get 应该返回排序缓存而不是原始缓存
	sortedEntry := &SortedCacheEntry{
		IPs:       []string{"8.8.8.8", "1.1.1.1", "9.9.9.9"},
		RTTs:      []int{50, 100, 75},
		Timestamp: time.Now(),
		TTL:       600,
		IsValid:   true,
	}
	cache.SetSorted(domain, qtype, sortedEntry)

	if entry, ok := cache.Get(domain, qtype); !ok {
		t.Error("Get 应该返回排序缓存")
	} else {
		if entry.IPs[0] != "8.8.8.8" {
			t.Error("Get 应该返回排序后的 IP")
		}
	}
}
