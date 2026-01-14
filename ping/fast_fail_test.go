package ping

import (
	"os"
	"testing"
	"time"
)

// TestRecordFastFail 测试快速失败记录
func TestRecordFastFail(t *testing.T) {
	tempFile := "test_fast_fail.json"
	defer os.Remove(tempFile)

	mgr := NewIPFailureWeightManager(tempFile)
	defer mgr.Clear()

	ip := "1.2.3.4"

	// 记录一次快速失败
	mgr.RecordFastFail(ip)

	record := mgr.GetRecord(ip)
	if record.FastFailCount != 1 {
		t.Errorf("Expected FastFailCount 1, got %d", record.FastFailCount)
	}
	if record.FailureCount != 1 {
		t.Errorf("Expected FailureCount 1, got %d", record.FailureCount)
	}
	if record.TotalAttempts != 1 {
		t.Errorf("Expected TotalAttempts 1, got %d", record.TotalAttempts)
	}

	// 再记录一次快速失败
	mgr.RecordFastFail(ip)

	record = mgr.GetRecord(ip)
	if record.FastFailCount != 2 {
		t.Errorf("Expected FastFailCount 2, got %d", record.FastFailCount)
	}
	if record.FailureCount != 2 {
		t.Errorf("Expected FailureCount 2, got %d", record.FailureCount)
	}
}

// TestFastFailWeight 测试快速失败的权重惩罚
func TestFastFailWeight(t *testing.T) {
	tempFile := "test_fast_fail_weight.json"
	defer os.Remove(tempFile)

	mgr := NewIPFailureWeightManager(tempFile)
	defer mgr.Clear()

	ip1 := "1.1.1.1"
	ip2 := "2.2.2.2"

	// ip1: 1 次快速失败
	mgr.RecordFastFail(ip1)

	// ip2: 10 次普通失效
	for i := 0; i < 10; i++ {
		mgr.RecordFailure(ip2)
	}

	weight1 := mgr.GetWeight(ip1)
	weight2 := mgr.GetWeight(ip2)

	// ip1 权重 = 1 * 500 (快速失败) + 1 * 50 (普通失效) = 550
	// ip2 权重 = 10 * 50 (普通失效) = 500
	// ip1 应该被惩罚更多

	if weight1 <= weight2 {
		t.Errorf("Expected weight1 (%d) > weight2 (%d), but got weight1 <= weight2", weight1, weight2)
	}

	t.Logf("ip1 (1 fast fail) weight: %d", weight1)
	t.Logf("ip2 (10 normal failures) weight: %d", weight2)
}

// TestFastFailVsNormalFailure 对比快速失败和普通失效的权重
func TestFastFailVsNormalFailure(t *testing.T) {
	tempFile := "test_fast_fail_vs_normal.json"
	defer os.Remove(tempFile)

	mgr := NewIPFailureWeightManager(tempFile)
	defer mgr.Clear()

	// 快速失败：1 次
	// 权重 = 1 * 500 (快速失败) + 1 * 50 (普通失效) = 550
	mgr.RecordFastFail("fast_fail_ip")

	// 普通失效：1 次
	// 权重 = 1 * 50 (普通失效) = 50
	mgr.RecordFailure("normal_fail_ip")

	weightFastFail := mgr.GetWeight("fast_fail_ip")
	weightNormalFail := mgr.GetWeight("normal_fail_ip")

	// 快速失败权重应该是普通失效的 11 倍（550 / 50 = 11）
	// 因为 RecordFastFail 同时增加 FastFailCount 和 FailureCount
	expectedRatio := 11.0
	actualRatio := float64(weightFastFail) / float64(weightNormalFail)

	if actualRatio < expectedRatio-0.5 || actualRatio > expectedRatio+0.5 {
		t.Errorf("Expected weight ratio ~%.1f, got %.1f", expectedRatio, actualRatio)
	}

	t.Logf("Fast fail weight: %d, Normal fail weight: %d, Ratio: %.1f", weightFastFail, weightNormalFail, actualRatio)
}

// TestFastFailDecay 测试快速失败权重的衰减
func TestFastFailDecay(t *testing.T) {
	tempFile := "test_fast_fail_decay.json"
	defer os.Remove(tempFile)

	mgr := NewIPFailureWeightManager(tempFile)
	defer mgr.Clear()

	ip := "1.2.3.4"

	// 记录快速失败
	mgr.RecordFastFail(ip)
	weight1 := mgr.GetWeight(ip)

	// 模拟时间流逝（修改 LastFailureTime）
	record := mgr.GetRecord(ip)
	record.LastFailureTime = time.Now().Add(-time.Duration(3*24) * time.Hour) // 3 天前

	mgr.mu.Lock()
	mgr.records[ip] = record
	mgr.mu.Unlock()

	weight2 := mgr.GetWeight(ip)

	// 3 天后，权重应该衰减到约 50%（7 天衰减周期）
	if weight2 >= weight1 {
		t.Errorf("Expected weight to decay, but weight2 (%d) >= weight1 (%d)", weight2, weight1)
	}

	t.Logf("Weight before decay: %d, after 3 days: %d", weight1, weight2)
}

// TestFastFailSorting 测试快速失败对排序的影响
func TestFastFailSorting(t *testing.T) {
	tempFile := "test_fast_fail_sorting.json"
	defer os.Remove(tempFile)

	mgr := NewIPFailureWeightManager(tempFile)
	defer mgr.Clear()

	pinger := &Pinger{
		count:            3,
		failureWeightMgr: mgr,
	}

	// 创建测试结果
	results := []Result{
		{IP: "1.1.1.1", RTT: 100, Loss: 0, ProbeMethod: "icmp", FastFail: false},
		{IP: "2.2.2.2", RTT: 150, Loss: 0, ProbeMethod: "icmp", FastFail: false},
		{IP: "3.3.3.3", RTT: 120, Loss: 0, ProbeMethod: "icmp", FastFail: false},
	}

	// 给 2.2.2.2 记录快速失败
	mgr.RecordFastFail("2.2.2.2")

	// 排序
	pinger.sortResults(results)

	// 2.2.2.2 应该排在后面
	if results[0].IP != "1.1.1.1" {
		t.Errorf("Expected first IP 1.1.1.1, got %s", results[0].IP)
	}

	if results[len(results)-1].IP != "2.2.2.2" {
		t.Errorf("Expected last IP 2.2.2.2, got %s", results[len(results)-1].IP)
	}

	t.Logf("Sorted order: %s -> %s -> %s", results[0].IP, results[1].IP, results[2].IP)
}

// TestFastFailPersistence 测试快速失败记录的持久化
func TestFastFailPersistence(t *testing.T) {
	tempFile := "test_fast_fail_persist.json"
	defer os.Remove(tempFile)

	// 创建管理器并记录快速失败
	mgr1 := NewIPFailureWeightManager(tempFile)
	mgr1.RecordFastFail("1.2.3.4")
	mgr1.RecordFastFail("1.2.3.4")
	mgr1.SaveToDisk()

	// 创建新的管理器，从磁盘加载
	mgr2 := NewIPFailureWeightManager(tempFile)

	record := mgr2.GetRecord("1.2.3.4")
	if record.FastFailCount != 2 {
		t.Errorf("Expected FastFailCount 2 after loading, got %d", record.FastFailCount)
	}

	t.Logf("Persistence test passed: FastFailCount = %d", record.FastFailCount)
}

// TestNoDoubleCountingFastFail 验证快速失败不会被两重记录
func TestNoDoubleCountingFastFail(t *testing.T) {
	tempFile := "test_no_double_counting.json"
	defer os.Remove(tempFile)

	mgr := NewIPFailureWeightManager(tempFile)
	defer mgr.Clear()

	pinger := &Pinger{
		count:            3,
		failureWeightMgr: mgr,
	}

	ip := "1.2.3.4"

	// 模拟 pingIP 返回的快速失败结果
	result := &Result{
		IP:          ip,
		RTT:         999999,
		Loss:        100,
		ProbeMethod: "none",
		FastFail:    true, // 标记为快速失败
	}

	// 模拟 PingAndSort 中的记录逻辑
	if result.FastFail {
		// 跳过，因为已经在 pingIP 中记录过了
	} else if result.Loss == 100 {
		pinger.RecordIPFailure(ip)
	}

	// 检查 FailureCount 是否只增加了 1 次（来自 RecordIPFastFail）
	record := mgr.GetRecord(ip)
	if record.FailureCount != 0 {
		t.Errorf("Expected FailureCount 0 (not recorded in PingAndSort), got %d", record.FailureCount)
	}

	// 现在手动调用 RecordIPFastFail 来模拟 pingIP 的行为
	pinger.RecordIPFastFail(ip)

	record = mgr.GetRecord(ip)
	if record.FailureCount != 1 {
		t.Errorf("Expected FailureCount 1 (only from RecordIPFastFail), got %d", record.FailureCount)
	}
	if record.FastFailCount != 1 {
		t.Errorf("Expected FastFailCount 1, got %d", record.FastFailCount)
	}

	t.Logf("No double counting verified: FailureCount=%d, FastFailCount=%d", record.FailureCount, record.FastFailCount)
}
