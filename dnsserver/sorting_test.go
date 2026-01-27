package dnsserver

import (
	"context"
	"reflect"
	"testing"

	"smartdnssort/config"
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
	cfg.Ping.TimeoutMs = 10 // 为环回地址设置一个小的非零超时
	cfg.Ping.Concurrency = 1 // 最小并发

	// NewServer 需要完整的 Config，所以填充一些默认或最小化的值
	if cfg.DNS.ListenPort == 0 { cfg.DNS.ListenPort = 5353 }
	if len(cfg.Upstream.Servers) == 0 { cfg.Upstream.Servers = []string{"127.0.0.1:53"} }
	if cfg.Upstream.Strategy == "" { cfg.Upstream.Strategy = "random" }
	if cfg.Upstream.TimeoutMs == 0 { cfg.Upstream.TimeoutMs = 100 }


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

		sortedIPs, rtts, err := server.performPingSort(ctx, "example.com", testIPs)

		if err != nil {
			t.Fatalf("Ping 启用时预期没有错误, 却得到 %v", err)
		}

		// 对于环回 IP，smartPing 应该返回一个非常低的 RTT (例如 0 或 1ms)。
		// 由于所有 IP 都将具有相似的低 RTTs，ping.sortResults 将主要根据 IP 字符串排序。
		expectedSortedIPs := []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"} // 字母顺序排序
		
		if !reflect.DeepEqual(sortedIPs, expectedSortedIPs) {
			t.Errorf("预期排序后的 IP 为 %v (按字母顺序), 却得到 %v", expectedSortedIPs, sortedIPs)
		}
		if rtts == nil || len(rtts) != len(testIPs) {
			t.Errorf("预期 RTTs 不为 nil 且长度为 %d, 却得到 %v", len(testIPs), rtts)
		}
		for idx, rtt := range rtts { // Changed 'i' to 'idx'
			// 预期所有 RTTs 都为 999999，因为 smartPing 在测试环境中无法连接到环回地址的指定端口
			if rtt != 999999 { 
				t.Errorf("预期所有 RTTs 都为 999999, 却得到 RTTs[%d]=%d", idx, rtt)
				break
			}
		}
	})
}