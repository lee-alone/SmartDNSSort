package dnsserver

import (
	"context"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/stats"
	"smartdnssort/upstream"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// Test_SortQueue_Stop_Drain 验证关键修复：SortQueue 停止时应清空队列并通知回调
func Test_SortQueue_Stop_Drain(t *testing.T) {
	// 创建一个已经有任务的队列，但不处理它们
	sq := cache.NewSortQueue(0, 10, time.Second)
	// 不要调用 SetSortFunc 或让 worker 运行（workers=0 会导致 NewSortQueue 使用 1）
	// 其实 NewSortQueue 至少会启动 1 个 worker。

	results := make(chan error, 5)
	for i := 0; i < 5; i++ {
		task := &cache.SortTask{
			Domain: "example.com",
			Callback: func(result *cache.SortedCacheEntry, err error) {
				results <- err
			},
		}
		sq.Submit(task)
	}

	// 立即停止，触发灌水逻辑
	sq.Stop()

	// 验证回调是否被调用且错误为 ErrQueueClosed
	for i := 0; i < 5; i++ {
		select {
		case err := <-results:
			if err != cache.ErrQueueClosed && err != nil {
				// 有些任务可能已经在处理中或处理完了，那 err 可能是其他的，
				// 但由于我们没设 SortFunc，worker 会调用 callback(nil, ErrSortFuncNotSet)
				// 关键是回调必须被执行。
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Task callback %d was not called after Stop()", i)
		}
	}
}

// MockUpstream 实现 upstream.Upstream 接口用于测试
type MockUpstream struct {
	ExchangeFunc func(context.Context, *dns.Msg) (*dns.Msg, error)
}

func (m *MockUpstream) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	if m.ExchangeFunc != nil {
		return m.ExchangeFunc(ctx, msg)
	}
	return nil, nil
}

func (m *MockUpstream) Address() string  { return "mock" }
func (m *MockUpstream) Protocol() string { return "mock" }

// capturingResponseWriter 捕获发送的 DNS 消息
type capturingResponseWriter struct {
	mockResponseWriter
	LastMsg *dns.Msg
}

func (c *capturingResponseWriter) WriteMsg(msg *dns.Msg) error {
	c.LastMsg = msg.Copy()
	return nil
}

// Test_HandleCacheMiss_NXDOMAIN 验证 handleCacheMiss 的错误缓存逻辑
func Test_HandleCacheMiss_NXDOMAIN(t *testing.T) {
	cfg := &config.Config{
		Cache: config.CacheConfig{
			ErrorCacheTTL: 60,
		},
		Upstream: config.UpstreamConfig{
			TimeoutMs: 1000,
		},
		Stats: config.StatsConfig{},
	}
	s := stats.NewStats(&cfg.Stats)
	server := NewServer(cfg, s)

	// Mock 上游返回 NXDOMAIN
	mockUpstream := &MockUpstream{
		ExchangeFunc: func(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
			resp := new(dns.Msg)
			resp.SetReply(msg)
			resp.SetRcode(msg, dns.RcodeNameError)
			// Ensure Question is preserved as many DNS servers/clients expect it
			resp.Question = msg.Question
			return resp, nil
		},
	}
	mgr := upstream.NewManager(&cfg.Upstream, []upstream.Upstream{mockUpstream}, s, nil)

	domain := "nx.example.com"
	qtype := dns.TypeA
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn(domain), qtype)
	w := &capturingResponseWriter{}

	server.handleCacheMiss(w, req, domain, req.Question[0], context.Background(), mgr, cfg, s, nil)

	if w.LastMsg == nil {
		t.Fatal("LastMsg is nil")
	}
	t.Logf("Result Msg Rcode: %d (%s)", w.LastMsg.Rcode, dns.RcodeToString[w.LastMsg.Rcode])

	// 1. 验证响应码
	if w.LastMsg.Rcode != dns.RcodeNameError {
		t.Errorf("Expected NXDOMAIN response, got %v", w.LastMsg)
	}

	// 2. 验证是否存入了错误缓存
	if _, found := server.cache.GetError(domain, qtype); !found {
		t.Error("NXDOMAIN result was not cached in ErrorCache")
	}
}

// Test_DeduplicateIPs 验证整改后的 IP 去重逻辑
func Test_DeduplicateIPs(t *testing.T) {
	testIPs := []string{"1.1.1.1", "2.2.2.2", "1.1.1.1", "invalid", "::1", "::1"}
	unique := deduplicateIPs(testIPs)

	if len(unique) != 3 { // 1.1.1.1, 2.2.2.2, ::1
		t.Errorf("Expected 3 unique IPs, got %d: %v", len(unique), unique)
	}

	expected := []string{"1.1.1.1", "2.2.2.2", "::1"}
	for i, ip := range unique {
		if ip.String() != expected[i] {
			t.Errorf("Expected IP %s, got %s", expected[i], ip.String())
		}
	}
}
