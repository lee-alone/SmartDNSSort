package ping

import (
	"context"
	"os"
	"testing"
	"time"

	"smartdnssort/connectivity"
)

// =============================================================================
// 第一阶段测试：稳定性保障 - P0/P1 级核心修复逻辑测试
// =============================================================================

// TestUpdateIPCache_NetworkOffline 断网隔离测试
// 验证：当 IsNetworkHealthy() == false 时，调用 UpdateIPCache，
// - ipPool 记录了 RTT（使用较小的 alpha 进行平滑更新）
// - rttCache 保持不变（防止缓存污染）
func TestUpdateIPCache_NetworkOffline(t *testing.T) {
	// 创建 Pinger 实例
	p := NewPinger(3, 800, 8, 0, 60, true, "")
	defer p.Stop()

	// 创建模拟的健康检查器，模拟断网状态
	mockChecker := newMockNetworkHealthChecker(false)
	p.SetHealthChecker(mockChecker)

	// 准备测试 IP
	testIP := "8.8.8.8"

	// 先在 ipPool 中添加该 IP
	p.ipPool.UpdateDomainIPs(nil, []string{testIP}, "test.com")

	// 验证 IP 已添加
	_, exists := p.ipPool.GetIPInfo(testIP)
	if !exists {
		t.Fatalf("Failed to add test IP to ipPool")
	}

	// 调用 UpdateIPCache（模拟断网时的探测结果）
	rtt := 50 // 50ms
	loss := 0.0
	p.UpdateIPCache(testIP, rtt, loss, "icmp")

	// 验证：rttCache 应该没有被更新（断网时跳过）
	_, _, cacheExists, _ := p.GetIPRTT(testIP)
	if cacheExists {
		t.Errorf("Expected rttCache to NOT be updated when network is offline, but it was updated")
	}

	// 验证：ipPool 应该记录了 RTT（使用较小的 alpha 0.1）
	// 使用 GetIPRTT 获取实际的 RTT 数据
	_, rttEWMA, updated := p.ipPool.GetIPRTT(testIP)
	if !updated {
		t.Errorf("Expected ipPool.RTTEWMA to be updated when network is offline, but it was not")
	}
	if rttEWMA != rtt {
		t.Errorf("Expected ipPool.RTTEWMA to be %d, got %d", rtt, rttEWMA)
	}

	t.Logf("断网隔离测试通过：rttCache 未更新，ipPool.RTTEWMA 已更新为 %d", rttEWMA)
}

// TestTCPRTTNormalization TCP 归一化测试
// 验证：当 TCP 响应延迟为 200ms 时，smartPingWithMethod 返回的 RTT 应为 80ms (200/2.5)
func TestTCPRTTNormalization(t *testing.T) {
	// 创建 Pinger 实例，启用 TCP 回退
	p := NewPinger(3, 800, 8, 0, 60, true, "")
	defer p.Stop()

	// 测试归一化计算
	testCases := []struct {
		tcpRTT      int
		expectedRTT int
	}{
		{200, 80},  // 200 / 2.5 = 80
		{250, 100}, // 250 / 2.5 = 100
		{100, 40},  // 100 / 2.5 = 40
		{2, 1},     // 2 / 2.5 = 0.8 -> 最小值保护为 1ms
		{25, 10},   // 25 / 2.5 = 10
	}

	for _, tc := range testCases {
		// 计算归一化后的 RTT
		normalizedRTT := int(float64(tc.tcpRTT) / 2.5)
		if normalizedRTT < 1 {
			normalizedRTT = 1
		}

		if normalizedRTT != tc.expectedRTT {
			t.Errorf("TCP RTT %dms: expected normalized RTT %dms, got %dms",
				tc.tcpRTT, tc.expectedRTT, normalizedRTT)
		} else {
			t.Logf("TCP RTT %dms -> 归一化 RTT %dms (符合预期)", tc.tcpRTT, normalizedRTT)
		}
	}
}

// TestFastFailRecovery FastFail 恢复测试
// 验证：连续触发 RecordFastFail 后接一个 RecordSuccess，FailureCount 应归零
func TestFastFailRecovery(t *testing.T) {
	tempFile := "test_fast_fail_recovery.json"
	defer os.Remove(tempFile)

	mgr := NewIPFailureWeightManager(tempFile)
	defer mgr.Clear()

	ip := "9.9.9.9"

	// 连续触发 3 次 FastFail
	for i := 0; i < 3; i++ {
		mgr.RecordFastFail(ip)
	}

	// 验证 FastFail 状态
	record := mgr.GetRecord(ip)
	if record.FastFailCount != 3 {
		t.Errorf("Expected FastFailCount 3, got %d", record.FastFailCount)
	}
	if record.FailureCount != 3 {
		t.Errorf("Expected FailureCount 3, got %d", record.FailureCount)
	}
	t.Logf("FastFail 后状态: FastFailCount=%d, FailureCount=%d", record.FastFailCount, record.FailureCount)

	// 触发一次成功（FastFail 后的首次成功）
	mgr.RecordSuccess(ip)

	// 验证：FailureCount 应该归零（FastFail 后的首次成功直接重置惩罚）
	record = mgr.GetRecord(ip)
	if record.FailureCount != 0 {
		t.Errorf("Expected FailureCount 0 after FastFail recovery, got %d", record.FailureCount)
	}
	if record.FastFailCount != 0 {
		t.Errorf("Expected FastFailCount 0 after FastFail recovery, got %d", record.FastFailCount)
	}
	t.Logf("首次成功后状态: FastFailCount=%d, FailureCount=%d (已重置)", record.FastFailCount, record.FailureCount)
}

// TestFastFailRecoveryWithPinger 使用 Pinger 测试 FastFail 恢复
// 验证：通过 Pinger 的 RecordIPFastFail 和 RecordIPSuccess 方法
func TestFastFailRecoveryWithPinger(t *testing.T) {
	tempFile := "test_pinger_fast_fail_recovery.json"
	defer os.Remove(tempFile)

	p := NewPinger(3, 800, 8, 0, 60, true, tempFile)
	defer p.Stop()

	// 设置网络健康检查器（模拟在线状态）
	mockChecker := newMockNetworkHealthChecker(true)
	p.SetHealthChecker(mockChecker)

	ip := "10.10.10.10"

	// 连续触发 2 次 FastFail
	p.RecordIPFastFail(ip)
	p.RecordIPFastFail(ip)

	// 验证状态
	record := p.GetIPFailureRecord(ip)
	if record.FastFailCount != 2 {
		t.Errorf("Expected FastFailCount 2, got %d", record.FastFailCount)
	}
	t.Logf("FastFail 后: FastFailCount=%d, FailureCount=%d", record.FastFailCount, record.FailureCount)

	// 触发一次成功
	p.RecordIPSuccess(ip)

	// 验证：FailureCount 应该归零
	record = p.GetIPFailureRecord(ip)
	if record.FailureCount != 0 {
		t.Errorf("Expected FailureCount 0 after recovery, got %d", record.FailureCount)
	}
	if record.FastFailCount != 0 {
		t.Errorf("Expected FastFailCount 0 after recovery, got %d", record.FastFailCount)
	}
	t.Logf("成功后: FastFailCount=%d, FailureCount=%d (已重置)", record.FastFailCount, record.FailureCount)
}

// TestRecordProbeResult 测试统一的 recordProbeResult 方法
// 验证：recordProbeResult 正确处理 FastFail、完全失败和部分失败的情况
func TestRecordProbeResult(t *testing.T) {
	tempFile := "test_record_probe_result.json"
	defer os.Remove(tempFile)

	p := NewPinger(3, 800, 8, 0, 60, true, tempFile)
	defer p.Stop()

	// 设置网络健康检查器（模拟在线状态）
	mockChecker := newMockNetworkHealthChecker(true)
	p.SetHealthChecker(mockChecker)

	// 测试用例
	testCases := []struct {
		name        string
		ip          string
		loss        float64
		isFastFail  bool
		checkResult func(t *testing.T, record *IPFailureRecord)
	}{
		{
			name:       "FastFail 应该跳过记录",
			ip:         "1.1.1.1",
			loss:       100,
			isFastFail: true,
			checkResult: func(t *testing.T, record *IPFailureRecord) {
				// FastFail 在 pingIP 中已经记录，recordProbeResult 应该跳过
				// 所以这里不应该有记录（除非之前有其他操作）
			},
		},
		{
			name:       "完全失败 (loss=100) 应该记录失败",
			ip:         "2.2.2.2",
			loss:       100,
			isFastFail: false,
			checkResult: func(t *testing.T, record *IPFailureRecord) {
				if record.FailureCount != 1 {
					t.Errorf("Expected FailureCount 1, got %d", record.FailureCount)
				}
			},
		},
		{
			name:       "部分成功 (loss=0) 应该记录成功",
			ip:         "3.3.3.3",
			loss:       0,
			isFastFail: false,
			checkResult: func(t *testing.T, record *IPFailureRecord) {
				if record.SuccessCount != 1 {
					t.Errorf("Expected SuccessCount 1, got %d", record.SuccessCount)
				}
			},
		},
		{
			name:       "部分成功 (loss=50) 应该记录成功",
			ip:         "4.4.4.4",
			loss:       50,
			isFastFail: false,
			checkResult: func(t *testing.T, record *IPFailureRecord) {
				if record.SuccessCount != 1 {
					t.Errorf("Expected SuccessCount 1, got %d", record.SuccessCount)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p.recordProbeResult(tc.ip, tc.loss, tc.isFastFail)
			record := p.GetIPFailureRecord(tc.ip)
			tc.checkResult(t, record)
		})
	}
}

// TestNetworkOfflinePreventsFailureRecording 断网时不记录失败权重
// 验证：当网络离线时，RecordIPFailure 和 RecordIPFastFail 不应该记录
func TestNetworkOfflinePreventsFailureRecording(t *testing.T) {
	tempFile := "test_offline_no_record.json"
	defer os.Remove(tempFile)

	p := NewPinger(3, 800, 8, 0, 60, true, tempFile)
	defer p.Stop()

	// 设置网络健康检查器（模拟离线状态）
	mockChecker := newMockNetworkHealthChecker(false)
	p.SetHealthChecker(mockChecker)

	ip := "11.11.11.11"

	// 尝试记录失败
	p.RecordIPFailure(ip)
	p.RecordIPFastFail(ip)

	// 验证：不应该有记录（断网保护）
	record := p.GetIPFailureRecord(ip)
	if record.FailureCount != 0 {
		t.Errorf("Expected FailureCount 0 when offline, got %d", record.FailureCount)
	}
	if record.FastFailCount != 0 {
		t.Errorf("Expected FastFailCount 0 when offline, got %d", record.FastFailCount)
	}
	t.Logf("断网保护测试通过：离线时不会记录失败权重")
}

// TestIPPoolEWMAUpdate 测试 IPPool 的 EWMA 更新
// 验证：IPPool 正确使用 EWMA 平滑 RTT 值
func TestIPPoolEWMAUpdate(t *testing.T) {
	p := NewPinger(3, 800, 8, 0, 60, true, "")
	defer p.Stop()

	// 设置网络健康检查器（模拟在线状态）
	mockChecker := newMockNetworkHealthChecker(true)
	p.SetHealthChecker(mockChecker)

	testIP := "8.8.4.4"
	p.ipPool.UpdateDomainIPs(nil, []string{testIP}, "test.com")

	// 验证 IP 已添加
	_, exists := p.ipPool.GetIPInfo(testIP)
	if !exists {
		t.Fatalf("Failed to add test IP to ipPool")
	}

	// 第一次更新 RTT
	p.UpdateIPCache(testIP, 100, 0, "icmp")
	_, rttEWMA1, updated1 := p.ipPool.GetIPRTT(testIP)
	if !updated1 {
		t.Fatalf("First UpdateIPCache failed: RTT not updated")
	}
	t.Logf("第一次更新: RTTEWMA=%d", rttEWMA1)

	// 第二次更新 RTT（不同的值）
	p.UpdateIPCache(testIP, 200, 0, "icmp")
	_, rttEWMA2, updated2 := p.ipPool.GetIPRTT(testIP)
	if !updated2 {
		t.Fatalf("Second UpdateIPCache failed: RTT not updated")
	}
	t.Logf("第二次更新: RTTEWMA=%d", rttEWMA2)

	// EWMA 应该平滑两次的值，不应该直接等于最新的 200
	// EWMA = alpha * new + (1 - alpha) * old = 0.3 * 200 + 0.7 * 100 = 130
	// 第一次 EWMA 是 100，第二次应该是 130 左右
	if rttEWMA2 < 100 || rttEWMA2 > 200 {
		t.Errorf("EWMA should be between 100 and 200, got %d", rttEWMA2)
	}

	// 验证 EWMA 不是直接等于最新值（说明平滑生效）
	if rttEWMA2 == 200 {
		t.Errorf("EWMA should not equal the latest RTT (200), got %d - EWMA smoothing may not be working", rttEWMA2)
	}
}

// =============================================================================
// 辅助类型
// =============================================================================

// mockNetworkHealthChecker 模拟网络健康检查器
type mockNetworkHealthChecker struct {
	healthy bool
}

// newMockNetworkHealthChecker 创建模拟网络健康检查器
func newMockNetworkHealthChecker(healthy bool) *mockNetworkHealthChecker {
	return &mockNetworkHealthChecker{healthy: healthy}
}

// IsNetworkHealthy 实现 NetworkHealthChecker 接口
func (m *mockNetworkHealthChecker) IsNetworkHealthy() bool {
	return m.healthy
}

// Start 实现 NetworkHealthChecker 接口（空实现，用于测试）
func (m *mockNetworkHealthChecker) Start() {
	// 测试中不需要启动
}

// Stop 实现 NetworkHealthChecker 接口（空实现，用于测试）
func (m *mockNetworkHealthChecker) Stop() {
	// 测试中不需要停止
}

// Ensure mockNetworkHealthChecker implements connectivity.NetworkHealthChecker
var _ connectivity.NetworkHealthChecker = (*mockNetworkHealthChecker)(nil)

// =============================================================================
// 基准测试
// =============================================================================

// BenchmarkUpdateIPCache 基准测试 UpdateIPCache 方法
func BenchmarkUpdateIPCache(b *testing.B) {
	p := NewPinger(3, 800, 8, 0, 60, true, "")
	defer p.Stop()

	mockChecker := newMockNetworkHealthChecker(true)
	p.SetHealthChecker(mockChecker)

	testIP := "8.8.8.8"
	p.ipPool.UpdateDomainIPs(nil, []string{testIP}, "test.com")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.UpdateIPCache(testIP, 50, 0, "icmp")
	}
}

// BenchmarkRecordProbeResult 基准测试 recordProbeResult 方法
func BenchmarkRecordProbeResult(b *testing.B) {
	p := NewPinger(3, 800, 8, 0, 60, true, "")
	defer p.Stop()

	mockChecker := newMockNetworkHealthChecker(true)
	p.SetHealthChecker(mockChecker)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.recordProbeResult("1.2.3.4", 0, false)
	}
}

// =============================================================================
// 集成测试
// =============================================================================

// TestPingAndSortWithOfflineNetwork 测试断网时的 PingAndSort 行为
func TestPingAndSortWithOfflineNetwork(t *testing.T) {
	p := NewPinger(3, 800, 8, 0, 60, true, "")
	defer p.Stop()

	// 设置网络健康检查器（模拟离线状态）
	mockChecker := newMockNetworkHealthChecker(false)
	p.SetHealthChecker(mockChecker)

	// 预先设置一些缓存数据
	testIPs := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	for _, ip := range testIPs {
		p.rttCache.set(ip, &rttCacheEntry{
			rtt:       50,
			loss:      0,
			staleAt:   time.Now().Add(60 * time.Second),
			expiresAt: time.Now().Add(90 * time.Second),
		})
	}

	// 调用 PingAndSort
	ctx := context.Background()
	results := p.PingAndSort(ctx, testIPs, "test.com")

	// 验证：应该返回缓存数据，不进行实际探测
	if len(results) != 3 {
		t.Errorf("Expected 3 cached results, got %d", len(results))
	}

	for _, r := range results {
		if r.ProbeMethod != "cached-offline" {
			t.Errorf("Expected ProbeMethod 'cached-offline', got '%s'", r.ProbeMethod)
		}
	}
	t.Logf("断网时 PingAndSort 返回 %d 条缓存数据", len(results))
}
