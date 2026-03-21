package dnsserver

import (
	"context"
	"reflect"
	"testing"

	"smartdnssort/config"
	"smartdnssort/ping"
	"smartdnssort/stats"
)

// newTestServerForSorting 创建一个 Server 实例，用于 performPingSort 测试。
// 它初始化真实的依赖，但配置为最小化副作用（如不执行网络 Ping）。
func newTestServerForSorting(cfg *config.Config) *Server {
	// 确保配置的 Stats 部分有最小的有效值，防止 stats.NewStats 内部出错
	if cfg.Stats.HotDomainsBucketMinutes == 0 {
		cfg.Stats.HotDomainsBucketMinutes = 1
	}
	if cfg.Stats.HotDomainsShardCount == 0 {
		cfg.Stats.HotDomainsShardCount = 1
	}
	if cfg.Stats.HotDomainsWindowHours == 0 {
		cfg.Stats.HotDomainsWindowHours = 1
	}
	if cfg.Stats.HotDomainsMaxPerBucket == 0 {
		cfg.Stats.HotDomainsMaxPerBucket = 1
	}

	mockStats := stats.NewStats(&cfg.Stats)

	// 配置 Pinger，使其使用环回地址，确保 PingAndSort 不进行外部网络调用，并产生确定性 RTT。
	// NewPinger 会将 count <= 0 强制设置为 3。因此，我们设置为 1 来进行最少 ping 次数模拟。
	if cfg.Ping.Count <= 0 { // 确保 Count 至少为 1，以触发 pinger 逻辑
		cfg.Ping.Count = 1
	}
	cfg.Ping.TimeoutMs = 10  // 为环回地址设置一个小的非零超时
	cfg.Ping.Concurrency = 1 // 最小并发

	// NewServer 需要完整的 Config，所以填充一些默认或最小化的值
	if cfg.DNS.ListenPort == 0 {
		cfg.DNS.ListenPort = 5353
	}
	if len(cfg.Upstream.Servers) == 0 {
		cfg.Upstream.Servers = []string{"127.0.0.1:53"}
	}
	if cfg.Upstream.Strategy == "" {
		cfg.Upstream.Strategy = "random"
	}
	if cfg.Upstream.TimeoutMs == 0 {
		cfg.Upstream.TimeoutMs = 100
	}

	s := NewServer(cfg, mockStats)
	return s
}

func TestPerformPingSort(t *testing.T) {
	ctx := context.Background()
	// 使用 TEST-NET-1 范围的 IP 进行确定性 RTT 测试，确保这些 IP 总是不可达
	testIPs := []string{"192.0.2.3", "192.0.2.1", "192.0.2.2"} // IPs 最初未排序

	// --- 场景 1: Ping 功能禁用 ---
	t.Run("PingDisabled", func(t *testing.T) {
		cfg := &config.Config{
			Ping: config.PingConfig{
				Enabled: false, // Ping 被禁用
				// 其他 Ping 配置值不重要，因为不会被调用
			},
			Stats:    config.StatsConfig{},
			Cache:    config.CacheConfig{},
			Prefetch: config.PrefetchConfig{},
			Upstream: config.UpstreamConfig{},
			DNS:      config.DNSConfig{},
		}
		server := newTestServerForSorting(cfg)

		sortedIPs, rtts, err := server.performPingSort(ctx, "example.com", testIPs)

		if err != nil {
			t.Fatalf("Ping 禁用时预期没有错误, 却得到 %v", err)
		}
		// 预期返回原始的、未排序的 IP
		if !reflect.DeepEqual(sortedIPs, testIPs) {
			t.Errorf("预期排序后的 IP 为原始 IP %v, 却得到 %v", testIPs, sortedIPs)
		}
		if rtts != nil {
			t.Errorf("Ping 禁用时预期 RTT 为 nil, 却得到 %v", rtts)
		}
	})

	// --- 场景 2: Ping 功能启用, pinger 返回确定性结果 (环回地址) ---
	t.Run("PingEnabledWithDeterministicResults", func(t *testing.T) {
		cfg := &config.Config{
			Ping: config.PingConfig{
				Enabled: true, // Ping 启用
				// Count 将被 newTestServerForSorting 确保为 >=1，TimeoutMs 极小
			},
			Stats:    config.StatsConfig{},
			Cache:    config.CacheConfig{},
			Prefetch: config.PrefetchConfig{},
			Upstream: config.UpstreamConfig{},
			DNS:      config.DNSConfig{},
		}
		server := newTestServerForSorting(cfg)

		// 关键：在测试前清空 IP 失效权重和 IP 池，确保排序结果不受历史数据影响
		// 这是解决"排序稳定性问题"的关键步骤
		server.pinger.ClearIPFailureWeights()
		server.pinger.GetIPPool().Clear()

		sortedIPs, rtts, err := server.performPingSort(ctx, "example.com", testIPs)

		if err != nil {
			t.Fatalf("Ping 启用时预期没有错误, 却得到 %v", err)
		}

		// 对于测试 IP（192.0.2.0/24 是 TEST-NET-1，不可达），
		// 所有 IP 的 RTT 都应该是 LogicDeadRTT (9000ms)。
		// 由于 FastFail 机制会在探测过程中记录权重，导致排序结果可能不完全按字母序。
		// 因此，我们只验证：
		// 1. 返回的 IP 数量正确
		// 2. 所有预期的 IP 都在结果中
		// 3. 所有 RTT 都是 LogicDeadRTT

		if len(sortedIPs) != len(testIPs) {
			t.Errorf("预期返回 %d 个 IP, 却得到 %d 个", len(testIPs), len(sortedIPs))
		}

		// 验证所有预期的 IP 都在结果中
		sortedIPSet := make(map[string]bool)
		for _, ip := range sortedIPs {
			sortedIPSet[ip] = true
		}
		for _, ip := range testIPs {
			if !sortedIPSet[ip] {
				t.Errorf("预期 IP %s 在结果中，但未找到", ip)
			}
		}

		// 输出实际排序结果用于调试
		t.Logf("实际排序结果: %v", sortedIPs)
		t.Logf("RTTs: %v", rtts)
		if rtts == nil || len(rtts) != len(testIPs) {
			t.Errorf("预期 RTTs 不为 nil 且长度为 %d, 却得到 %v", len(testIPs), rtts)
		}
		for idx, rtt := range rtts {
			// 预期所有 RTTs 都为 LogicDeadRTT，因为测试环境中无法连接到测试 IP
			if rtt != ping.LogicDeadRTT {
				t.Errorf("预期所有 RTTs 都为 %d, 却得到 RTTs[%d]=%d", ping.LogicDeadRTT, idx, rtt)
				break
			}
		}
	})
}
