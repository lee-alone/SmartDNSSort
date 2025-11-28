package upstream

import (
	"context"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestFastResponseMechanism 测试快速响应机制
func TestFastResponseMechanism(t *testing.T) {
	// 创建多个上游服务器
	servers := []Upstream{
		mustCreateUpstream("udp://8.8.8.8:53"),
		mustCreateUpstream("udp://1.1.1.1:53"),
		mustCreateUpstream("udp://114.114.114.114:53"),
	}

	// 创建管理器
	manager := NewManager(servers, "parallel", 5000, 3, nil)

	// 设置缓存更新回调
	var callbackCalled bool
	var callbackIPs []string
	var callbackMu sync.Mutex

	manager.SetCacheUpdateCallback(func(domain string, qtype uint16, ips []string, cname string, ttl uint32) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		callbackCalled = true
		callbackIPs = ips
		log.Printf("[TEST] 缓存更新回调被调用: domain=%s, IP数量=%d, IPs=%v\n", domain, len(ips), ips)
	})

	// 执行查询
	ctx := context.Background()
	startTime := time.Now()
	result, err := manager.Query(ctx, "www.baidu.com", dns.TypeA)
	responseTime := time.Since(startTime)

	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	log.Printf("[TEST] 快速响应时间: %v\n", responseTime)
	log.Printf("[TEST] 快速响应返回的IP数量: %d, IPs: %v\n", len(result.IPs), result.IPs)

	// 等待后台收集完成（最多等待 10 秒）
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("等待后台收集超时")
		case <-ticker.C:
			callbackMu.Lock()
			called := callbackCalled
			callbackMu.Unlock()

			if called {
				log.Printf("[TEST] 后台收集完成！\n")
				callbackMu.Lock()
				finalIPs := callbackIPs
				callbackMu.Unlock()

				log.Printf("[TEST] 最终收集到的IP数量: %d, IPs: %v\n", len(finalIPs), finalIPs)

				// 验证结果
				if len(finalIPs) < len(result.IPs) {
					t.Errorf("后台收集的IP数量(%d)少于快速响应的IP数量(%d)", len(finalIPs), len(result.IPs))
				}

				// 验证快速响应的IP都在最终结果中
				for _, ip := range result.IPs {
					found := false
					for _, finalIP := range finalIPs {
						if ip == finalIP {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("快速响应的IP %s 不在最终结果中", ip)
					}
				}

				log.Printf("[TEST] ✅ 测试通过：快速响应机制工作正常\n")
				return
			}
		}
	}
}

// mustCreateUpstream 创建上游服务器，如果失败则 panic
func mustCreateUpstream(url string) Upstream {
	u, err := NewUpstream(url, nil)
	if err != nil {
		panic(err)
	}
	return u
}
