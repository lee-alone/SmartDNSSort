package cache

import (
	"fmt"
	"sync"
	"testing"
)

// BenchmarkLRUCacheGet 测试改进前后的 LRU Get 性能
func BenchmarkLRUCacheGet(b *testing.B) {
	lru := NewLRUCache(10000)
	defer lru.Close()

	// 预填充缓存
	for i := 0; i < 1000; i++ {
		lru.Set(fmt.Sprintf("key-%d", i), i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			lru.Get(fmt.Sprintf("key-%d", i%1000))
			i++
		}
	})
}

// BenchmarkShardedCacheGet 测试分片缓存的 Get 性能
func BenchmarkShardedCacheGet(b *testing.B) {
	sc := NewShardedCache(10000, 64)

	// 预填充缓存
	for i := 0; i < 1000; i++ {
		sc.Set(fmt.Sprintf("key-%d", i), i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sc.Get(fmt.Sprintf("key-%d", i%1000))
			i++
		}
	})
}

// BenchmarkLRUCacheSet 测试 LRU Set 性能
func BenchmarkLRUCacheSet(b *testing.B) {
	lru := NewLRUCache(10000)
	defer lru.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			lru.Set(fmt.Sprintf("key-%d", i), i)
			i++
		}
	})
}

// BenchmarkShardedCacheSet 测试分片缓存的 Set 性能
func BenchmarkShardedCacheSet(b *testing.B) {
	sc := NewShardedCache(10000, 64)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sc.Set(fmt.Sprintf("key-%d", i), i)
			i++
		}
	})
}

// BenchmarkMixedWorkload 测试混合工作负载（80% 读，20% 写）
func BenchmarkMixedWorkloadLRU(b *testing.B) {
	lru := NewLRUCache(10000)
	defer lru.Close()

	// 预填充
	for i := 0; i < 1000; i++ {
		lru.Set(fmt.Sprintf("key-%d", i), i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%5 < 4 { // 80% 读
				lru.Get(fmt.Sprintf("key-%d", i%1000))
			} else { // 20% 写
				lru.Set(fmt.Sprintf("key-%d", i%1000), i)
			}
			i++
		}
	})
}

// BenchmarkMixedWorkloadSharded 测试分片缓存的混合工作负载
func BenchmarkMixedWorkloadSharded(b *testing.B) {
	sc := NewShardedCache(10000, 64)

	// 预填充
	for i := 0; i < 1000; i++ {
		sc.Set(fmt.Sprintf("key-%d", i), i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%5 < 4 { // 80% 读
				sc.Get(fmt.Sprintf("key-%d", i%1000))
			} else { // 20% 写
				sc.Set(fmt.Sprintf("key-%d", i%1000), i)
			}
			i++
		}
	})
}

// TestConcurrentAccess 测试并发访问的正确性
func TestConcurrentAccess(t *testing.T) {
	tests := []struct {
		name string
		fn   func(t *testing.T)
	}{
		{
			name: "LRU Concurrent",
			fn: func(t *testing.T) {
				lru := NewLRUCache(1000)
				defer lru.Close()

				var wg sync.WaitGroup
				errors := make(chan error, 100)

				// 10 个 goroutine 并发写入
				for i := 0; i < 10; i++ {
					wg.Add(1)
					go func(id int) {
						defer wg.Done()
						for j := 0; j < 100; j++ {
							lru.Set(fmt.Sprintf("key-%d-%d", id, j), j)
						}
					}(i)
				}

				// 10 个 goroutine 并发读取
				for i := 0; i < 10; i++ {
					wg.Add(1)
					go func(id int) {
						defer wg.Done()
						for j := 0; j < 100; j++ {
							_, _ = lru.Get(fmt.Sprintf("key-%d-%d", id, j))
						}
					}(i)
				}

				wg.Wait()
				close(errors)

				for err := range errors {
					if err != nil {
						t.Errorf("Concurrent access error: %v", err)
					}
				}
			},
		},
		{
			name: "Sharded Concurrent",
			fn: func(t *testing.T) {
				sc := NewShardedCache(1000, 32)

				var wg sync.WaitGroup
				errors := make(chan error, 100)

				// 10 个 goroutine 并发写入
				for i := 0; i < 10; i++ {
					wg.Add(1)
					go func(id int) {
						defer wg.Done()
						for j := 0; j < 100; j++ {
							sc.Set(fmt.Sprintf("key-%d-%d", id, j), j)
						}
					}(i)
				}

				// 10 个 goroutine 并发读取
				for i := 0; i < 10; i++ {
					wg.Add(1)
					go func(id int) {
						defer wg.Done()
						for j := 0; j < 100; j++ {
							_, _ = sc.Get(fmt.Sprintf("key-%d-%d", id, j))
						}
					}(i)
				}

				wg.Wait()
				close(errors)

				for err := range errors {
					if err != nil {
						t.Errorf("Concurrent access error: %v", err)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.fn)
	}
}

// TestShardedCacheCorrectness 测试分片缓存的正确性
func TestShardedCacheCorrectness(t *testing.T) {
	sc := NewShardedCache(100, 4)

	// 测试基本操作
	sc.Set("key1", "value1")
	val, ok := sc.Get("key1")
	if !ok || val != "value1" {
		t.Errorf("Expected value1, got %v", val)
	}

	// 测试更新
	sc.Set("key1", "value2")
	val, ok = sc.Get("key1")
	if !ok || val != "value2" {
		t.Errorf("Expected value2, got %v", val)
	}

	// 测试删除
	sc.Delete("key1")
	_, ok = sc.Get("key1")
	if ok {
		t.Errorf("Expected key1 to be deleted")
	}

	// 测试容量限制
	for i := 0; i < 150; i++ {
		sc.Set(fmt.Sprintf("key-%d", i), i)
	}
	if sc.Len() > 100 {
		t.Errorf("Expected cache size <= 100, got %d", sc.Len())
	}
}

// TestLRUCacheCorrectness 测试改进的 LRU 缓存正确性
func TestLRUCacheCorrectness(t *testing.T) {
	lru := NewLRUCache(100)
	defer lru.Close()

	// 测试基本操作
	lru.Set("key1", "value1")
	val, ok := lru.Get("key1")
	if !ok || val != "value1" {
		t.Errorf("Expected value1, got %v", val)
	}

	// 测试更新
	lru.Set("key1", "value2")
	val, ok = lru.Get("key1")
	if !ok || val != "value2" {
		t.Errorf("Expected value2, got %v", val)
	}

	// 测试删除
	lru.Delete("key1")
	_, ok = lru.Get("key1")
	if ok {
		t.Errorf("Expected key1 to be deleted")
	}

	// 测试容量限制
	for i := 0; i < 150; i++ {
		lru.Set(fmt.Sprintf("key-%d", i), i)
	}
	if lru.Len() > 100 {
		t.Errorf("Expected cache size <= 100, got %d", lru.Len())
	}
}
