package resolver

import (
	"testing"
	"time"
)

func TestNewStats(t *testing.T) {
	// 测试创建新的统计模块
	stats := NewStats()

	if stats.GetTotalQueries() != 0 {
		t.Error("expected total queries to be 0")
	}
	if stats.GetSuccessQueries() != 0 {
		t.Error("expected success queries to be 0")
	}
	if stats.GetFailedQueries() != 0 {
		t.Error("expected failed queries to be 0")
	}
}

func TestRecordQuery_Success(t *testing.T) {
	// 测试记录成功查询
	stats := NewStats()

	stats.RecordQuery(100*time.Millisecond, true)

	if stats.GetTotalQueries() != 1 {
		t.Errorf("expected total queries 1, got %d", stats.GetTotalQueries())
	}
	if stats.GetSuccessQueries() != 1 {
		t.Errorf("expected success queries 1, got %d", stats.GetSuccessQueries())
	}
	if stats.GetFailedQueries() != 0 {
		t.Errorf("expected failed queries 0, got %d", stats.GetFailedQueries())
	}
}

func TestRecordQuery_Failed(t *testing.T) {
	// 测试记录失败查询
	stats := NewStats()

	stats.RecordQuery(100*time.Millisecond, false)

	if stats.GetTotalQueries() != 1 {
		t.Errorf("expected total queries 1, got %d", stats.GetTotalQueries())
	}
	if stats.GetSuccessQueries() != 0 {
		t.Errorf("expected success queries 0, got %d", stats.GetSuccessQueries())
	}
	if stats.GetFailedQueries() != 1 {
		t.Errorf("expected failed queries 1, got %d", stats.GetFailedQueries())
	}
}

func TestRecordQuery_Multiple(t *testing.T) {
	// 测试记录多个查询
	stats := NewStats()

	stats.RecordQuery(100*time.Millisecond, true)
	stats.RecordQuery(200*time.Millisecond, true)
	stats.RecordQuery(150*time.Millisecond, false)

	if stats.GetTotalQueries() != 3 {
		t.Errorf("expected total queries 3, got %d", stats.GetTotalQueries())
	}
	if stats.GetSuccessQueries() != 2 {
		t.Errorf("expected success queries 2, got %d", stats.GetSuccessQueries())
	}
	if stats.GetFailedQueries() != 1 {
		t.Errorf("expected failed queries 1, got %d", stats.GetFailedQueries())
	}
}

func TestRecordCacheHit(t *testing.T) {
	// 测试记录缓存命中
	stats := NewStats()

	stats.RecordCacheHit()
	stats.RecordCacheHit()

	if stats.GetCacheHits() != 2 {
		t.Errorf("expected cache hits 2, got %d", stats.GetCacheHits())
	}
}

func TestRecordCacheMiss(t *testing.T) {
	// 测试记录缓存未命中
	stats := NewStats()

	stats.RecordCacheMiss()
	stats.RecordCacheMiss()
	stats.RecordCacheMiss()

	if stats.GetCacheMisses() != 3 {
		t.Errorf("expected cache misses 3, got %d", stats.GetCacheMisses())
	}
}

func TestGetSuccessRate(t *testing.T) {
	// 测试获取成功率
	stats := NewStats()

	stats.RecordQuery(100*time.Millisecond, true)
	stats.RecordQuery(100*time.Millisecond, true)
	stats.RecordQuery(100*time.Millisecond, false)

	rate := stats.GetSuccessRate()
	expected := float64(2) / float64(3) * 100

	if rate != expected {
		t.Errorf("expected success rate %.2f, got %.2f", expected, rate)
	}
}

func TestGetAverageLatency(t *testing.T) {
	// 测试获取平均延迟
	stats := NewStats()

	stats.RecordQuery(100*time.Millisecond, true)
	stats.RecordQuery(200*time.Millisecond, true)

	avgLatency := stats.GetAverageLatency()
	expected := 150.0 // (100 + 200) / 2

	if avgLatency < expected-1 || avgLatency > expected+1 {
		t.Errorf("expected average latency ~%.2f, got %.2f", expected, avgLatency)
	}
}

func TestGetStats(t *testing.T) {
	// 测试获取统计信息
	stats := NewStats()

	stats.RecordQuery(100*time.Millisecond, true)
	stats.RecordQuery(200*time.Millisecond, false)
	stats.RecordCacheHit()
	stats.RecordCacheMiss()

	statsMap := stats.GetStats()

	if statsMap["total_queries"] != int64(2) {
		t.Errorf("expected total_queries 2, got %v", statsMap["total_queries"])
	}
	if statsMap["success_queries"] != int64(1) {
		t.Errorf("expected success_queries 1, got %v", statsMap["success_queries"])
	}
	if statsMap["failed_queries"] != int64(1) {
		t.Errorf("expected failed_queries 1, got %v", statsMap["failed_queries"])
	}
	if statsMap["cache_hits"] != int64(1) {
		t.Errorf("expected cache_hits 1, got %v", statsMap["cache_hits"])
	}
	if statsMap["cache_misses"] != int64(1) {
		t.Errorf("expected cache_misses 1, got %v", statsMap["cache_misses"])
	}
}

func TestReset(t *testing.T) {
	// 测试重置统计信息
	stats := NewStats()

	stats.RecordQuery(100*time.Millisecond, true)
	stats.RecordCacheHit()

	if stats.GetTotalQueries() != 1 {
		t.Error("expected total queries 1 before reset")
	}

	stats.Reset()

	if stats.GetTotalQueries() != 0 {
		t.Error("expected total queries 0 after reset")
	}
	if stats.GetSuccessQueries() != 0 {
		t.Error("expected success queries 0 after reset")
	}
	if stats.GetCacheHits() != 0 {
		t.Error("expected cache hits 0 after reset")
	}
}

func TestGetStats_EmptyStats(t *testing.T) {
	// 测试获取空统计信息
	stats := NewStats()

	statsMap := stats.GetStats()

	if statsMap["total_queries"] != int64(0) {
		t.Error("expected total_queries 0")
	}
	if statsMap["success_rate"] != float64(0) {
		t.Error("expected success_rate 0")
	}
	if statsMap["avg_latency_ms"] != float64(0) {
		t.Error("expected avg_latency_ms 0")
	}
}

func TestConcurrentRecordQuery(t *testing.T) {
	// 测试并发记录查询
	stats := NewStats()

	// 并发记录查询
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			stats.RecordQuery(100*time.Millisecond, true)
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	if stats.GetTotalQueries() != 10 {
		t.Errorf("expected total queries 10, got %d", stats.GetTotalQueries())
	}
	if stats.GetSuccessQueries() != 10 {
		t.Errorf("expected success queries 10, got %d", stats.GetSuccessQueries())
	}
}
