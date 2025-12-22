package ping

import (
	"os"
	"testing"
	"time"
)

func TestIPFailureWeightManager(t *testing.T) {
	tmpFile := "test_ip_failure_weights.json"
	defer os.Remove(tmpFile)

	manager := NewIPFailureWeightManager(tmpFile)

	// 测试记录失效
	manager.RecordFailure("8.8.8.8")
	manager.RecordFailure("8.8.8.8")
	manager.RecordFailure("1.1.1.1")

	// 验证失效计数
	record := manager.GetRecord("8.8.8.8")
	if record.FailureCount != 2 {
		t.Errorf("Expected 2 failures for 8.8.8.8, got %d", record.FailureCount)
	}

	record = manager.GetRecord("1.1.1.1")
	if record.FailureCount != 1 {
		t.Errorf("Expected 1 failure for 1.1.1.1, got %d", record.FailureCount)
	}

	// 验证失效率
	if record.FailureRate != 1.0 {
		t.Errorf("Expected 100%% failure rate for 1.1.1.1, got %.2f%%", record.FailureRate*100)
	}

	// 测试权重计算
	weight := manager.GetWeight("8.8.8.8")
	if weight <= 0 {
		t.Errorf("Expected positive weight for 8.8.8.8, got %d", weight)
	}

	// 测试成功恢复
	manager.RecordSuccess("8.8.8.8")
	manager.RecordSuccess("8.8.8.8")
	manager.RecordSuccess("8.8.8.8")

	record = manager.GetRecord("8.8.8.8")
	if record.FailureCount != 1 {
		t.Errorf("Expected 1 failure after 3 successes, got %d", record.FailureCount)
	}

	// 测试持久化
	if err := manager.SaveToDisk(); err != nil {
		t.Errorf("Failed to save to disk: %v", err)
	}

	// 创建新管理器并加载
	manager2 := NewIPFailureWeightManager(tmpFile)
	record2 := manager2.GetRecord("8.8.8.8")
	if record2.FailureCount != record.FailureCount {
		t.Errorf("Expected %d failures after reload, got %d", record.FailureCount, record2.FailureCount)
	}
}

func TestIPFailureWeightDecay(t *testing.T) {
	manager := NewIPFailureWeightManager("")

	// 记录失效
	manager.RecordFailure("8.8.8.8")
	manager.RecordFailure("8.8.8.8")

	// 获取初始权重
	initialWeight := manager.GetWeight("8.8.8.8")

	// 模拟时间流逝（修改最后失效时间）
	manager.mu.Lock()
	record := manager.records["8.8.8.8"]
	record.LastFailureTime = time.Now().Add(-8 * 24 * time.Hour) // 8天前
	manager.mu.Unlock()

	// 权重应该衰减到0
	decayedWeight := manager.GetWeight("8.8.8.8")
	if decayedWeight != 0 {
		t.Errorf("Expected weight to decay to 0 after 8 days, got %d", decayedWeight)
	}

	// 验证初始权重大于衰减后的权重
	if initialWeight <= decayedWeight {
		t.Errorf("Initial weight (%d) should be greater than decayed weight (%d)", initialWeight, decayedWeight)
	}
}

func TestSortResultsWithFailureWeight(t *testing.T) {
	tmpFile := "test_sort_failure_weights.json"
	defer os.Remove(tmpFile)

	pinger := NewPinger(3, 800, 8, 0, 300, false, tmpFile)
	defer pinger.Stop()

	// 记录IP失效
	pinger.RecordIPFailure("8.8.8.8")
	pinger.RecordIPFailure("8.8.8.8")
	pinger.RecordIPFailure("8.8.8.8")

	// 创建测试结果
	results := []Result{
		{IP: "8.8.8.8", RTT: 50, Loss: 0},
		{IP: "1.1.1.1", RTT: 100, Loss: 0},
	}

	// 排序
	pinger.sortResults(results)

	// 8.8.8.8 应该排在后面（因为有失效权重）
	if results[0].IP != "1.1.1.1" {
		t.Errorf("Expected 1.1.1.1 to be first, got %s", results[0].IP)
	}
	if results[1].IP != "8.8.8.8" {
		t.Errorf("Expected 8.8.8.8 to be second, got %s", results[1].IP)
	}
}

func TestMaxFailureCount(t *testing.T) {
	manager := NewIPFailureWeightManager("")

	// 记录超过最大失效次数的失效
	for i := 0; i < 150; i++ {
		manager.RecordFailure("8.8.8.8")
	}

	record := manager.GetRecord("8.8.8.8")
	if record.FailureCount > manager.maxFailureCount {
		t.Errorf("Failure count exceeded max: %d > %d", record.FailureCount, manager.maxFailureCount)
	}
}

func TestFailureRateCalculation(t *testing.T) {
	manager := NewIPFailureWeightManager("")

	// 记录3次失效，2次成功
	manager.RecordFailure("8.8.8.8")
	manager.RecordFailure("8.8.8.8")
	manager.RecordFailure("8.8.8.8")
	manager.RecordSuccess("8.8.8.8")
	manager.RecordSuccess("8.8.8.8")

	record := manager.GetRecord("8.8.8.8")
	expectedRate := 3.0 / 5.0 // 60%
	if record.FailureRate != expectedRate {
		t.Errorf("Expected failure rate %.2f%%, got %.2f%%", expectedRate*100, record.FailureRate*100)
	}
}

func TestGetAllRecords(t *testing.T) {
	manager := NewIPFailureWeightManager("")

	// 记录多个IP的失效
	manager.RecordFailure("8.8.8.8")
	manager.RecordFailure("1.1.1.1")
	manager.RecordFailure("1.1.1.1")

	records := manager.GetAllRecords()
	if len(records) != 2 {
		t.Errorf("Expected 2 records, got %d", len(records))
	}

	// 验证记录内容
	found := make(map[string]bool)
	for _, r := range records {
		found[r.IP] = true
	}

	if !found["8.8.8.8"] || !found["1.1.1.1"] {
		t.Errorf("Expected to find both IPs in records")
	}
}
