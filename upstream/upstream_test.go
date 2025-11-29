package upstream

import (
	"context"
	"smartdnssort/config"
	"smartdnssort/stats"
	"testing"
	"time"

	"smartdnssort/upstream/bootstrap"

	"github.com/miekg/dns"
)

func TestParallelQuery(t *testing.T) {
	// 创建测试用的上游服务器列表（使用公共 DNS）
	servers := []string{
		"8.8.8.8:53",
		"1.1.1.1:53",
		"223.5.5.5:53",
	}

	cfg := &config.StatsConfig{
		HotDomainsWindowHours:   24,
		HotDomainsBucketMinutes: 60,
		HotDomainsShardCount:    16,
		HotDomainsMaxPerBucket:  5000,
	}
	s := stats.NewStats(cfg)

	// 测试 parallel 策略
	t.Run("Parallel Strategy", func(t *testing.T) {
		boot := bootstrap.NewResolver([]string{"223.5.5.5:53"})
		var upstreams []Upstream
		for _, srv := range servers {
			u, _ := NewUpstream(srv, boot)
			upstreams = append(upstreams, u)
		}
		u := NewManager(upstreams, "parallel", 3000, 2, s, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := u.Query(ctx, "www.google.com", dns.TypeA)
		if err != nil {
			t.Fatalf("Parallel query failed: %v", err)
		}

		if len(result.IPs) == 0 {
			t.Fatal("Expected IPs but got none")
		}

		t.Logf("Parallel query returned %d IPs: %v", len(result.IPs), result.IPs)
		t.Logf("TTL: %d seconds", result.TTL)
	})

	// 测试 random 策略
	t.Run("Random Strategy", func(t *testing.T) {
		boot := bootstrap.NewResolver([]string{"223.5.5.5:53"})
		var upstreams []Upstream
		for _, srv := range servers {
			u, _ := NewUpstream(srv, boot)
			upstreams = append(upstreams, u)
		}
		u := NewManager(upstreams, "random", 3000, 2, s, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		result, err := u.Query(ctx, "www.google.com", dns.TypeA)
		if err != nil {
			t.Fatalf("Random query failed: %v", err)
		}

		if len(result.IPs) == 0 {
			t.Fatal("Expected IPs but got none")
		}

		t.Logf("Random query returned %d IPs: %v", len(result.IPs), result.IPs)
		t.Logf("TTL: %d seconds", result.TTL)
	})

	// 测试并发控制
	t.Run("Concurrency Control", func(t *testing.T) {
		// 使用较小的并发数
		boot := bootstrap.NewResolver([]string{"223.5.5.5:53"})
		var upstreams []Upstream
		for _, srv := range servers {
			u, _ := NewUpstream(srv, boot)
			upstreams = append(upstreams, u)
		}
		u := NewManager(upstreams, "parallel", 3000, 1, s, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		start := time.Now()
		result, err := u.Query(ctx, "www.baidu.com", dns.TypeA)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Query with concurrency=1 failed: %v", err)
		}

		if len(result.IPs) == 0 {
			t.Fatal("Expected IPs but got none")
		}

		t.Logf("Query with concurrency=1 took %v", elapsed)
		t.Logf("Returned %d IPs: %v", len(result.IPs), result.IPs)
	})
}

func TestParallelQueryFailover(t *testing.T) {
	// 测试当部分服务器失败时的容错能力
	servers := []string{
		"192.0.2.1:53",    // 无效的 IP（TEST-NET-1）
		"8.8.8.8:53",      // 有效的 Google DNS
		"198.51.100.1:53", // 无效的 IP（TEST-NET-2）
	}

	cfg := &config.StatsConfig{
		HotDomainsWindowHours:   24,
		HotDomainsBucketMinutes: 60,
		HotDomainsShardCount:    16,
		HotDomainsMaxPerBucket:  5000,
	}
	s := stats.NewStats(cfg)

	boot := bootstrap.NewResolver([]string{"223.5.5.5:53"})
	var upstreams []Upstream
	for _, srv := range servers {
		u, _ := NewUpstream(srv, boot)
		upstreams = append(upstreams, u)
	}
	u := NewManager(upstreams, "parallel", 1000, 3, s, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := u.Query(ctx, "www.google.com", dns.TypeA)
	if err != nil {
		t.Fatalf("Parallel query with failover failed: %v", err)
	}

	if len(result.IPs) == 0 {
		t.Fatal("Expected IPs but got none")
	}

	t.Logf("Failover test returned %d IPs: %v", len(result.IPs), result.IPs)

	// 检查统计信息
	statsData := s.GetStats()
	t.Logf("Stats: %+v", statsData)
}

func TestParallelQueryIPMerging(t *testing.T) {
	// 测试并行查询的IP汇总功能
	// 使用多个公共DNS服务器,它们可能返回不同的IP地址
	servers := []string{
		"8.8.8.8:53",         // Google DNS
		"1.1.1.1:53",         // Cloudflare DNS
		"223.5.5.5:53",       // 阿里 DNS
		"114.114.114.114:53", // 114 DNS
	}

	cfg := &config.StatsConfig{
		HotDomainsWindowHours:   24,
		HotDomainsBucketMinutes: 60,
		HotDomainsShardCount:    16,
		HotDomainsMaxPerBucket:  5000,
	}
	s := stats.NewStats(cfg)

	boot := bootstrap.NewResolver([]string{"223.5.5.5:53"})
	var upstreams []Upstream
	for _, srv := range servers {
		u, _ := NewUpstream(srv, boot)
		upstreams = append(upstreams, u)
	}
	u := NewManager(upstreams, "parallel", 3000, 4, s, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 查询一个有多个IP的域名
	result, err := u.Query(ctx, "www.baidu.com", dns.TypeA)
	if err != nil {
		t.Fatalf("Parallel query for IP merging failed: %v", err)
	}

	if len(result.IPs) == 0 {
		t.Fatal("Expected IPs but got none")
	}

	t.Logf("IP Merging test returned %d unique IPs: %v", len(result.IPs), result.IPs)
	t.Logf("TTL: %d seconds", result.TTL)

	// 验证IP去重功能
	ipSet := make(map[string]bool)
	for _, ip := range result.IPs {
		if ipSet[ip] {
			t.Errorf("Found duplicate IP: %s", ip)
		}
		ipSet[ip] = true
	}

	t.Logf("All %d IPs are unique (no duplicates)", len(result.IPs))
}
