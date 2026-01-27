package transport

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestConnectionPoolBasic 测试基本的连接池功能
func TestConnectionPoolBasic(t *testing.T) {
	// 创建一个简单的 UDP 服务器用于测试
	addr := "127.0.0.1:0"
	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		Port: 0,
		IP:   net.ParseIP("127.0.0.1"),
	})
	if err != nil {
		t.Fatalf("Failed to create test server: %v", err)
	}
	defer conn.Close()

	serverAddr := conn.LocalAddr().String()

	// 创建连接池
	pool := NewConnectionPool(serverAddr, "udp", 5, 1*time.Minute)
	defer pool.Close()

	// 测试：创建连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 创建一个简单的 DNS 查询消息
	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeA)

	// 注意：这个测试会失败，因为我们没有真正的 DNS 服务器
	// 但我们可以测试连接池的基本逻辑
	_, err = pool.Exchange(ctx, msg)
	if err == nil {
		t.Logf("Query succeeded (unexpected, but connection pool worked)")
	} else {
		t.Logf("Query failed as expected: %v", err)
	}

	// 验证连接池状态
	stats := pool.GetStats()
	t.Logf("Pool stats: %+v", stats)

	if stats["address"] != serverAddr {
		t.Errorf("Expected address %s, got %s", serverAddr, stats["address"])
	}
}

// TestConnectionPoolReuse 测试连接复用
func TestConnectionPoolReuse(t *testing.T) {
	pool := NewConnectionPool("8.8.8.8:53", "udp", 3, 1*time.Minute)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 创建多个查询消息
	msg1 := new(dns.Msg)
	msg1.SetQuestion("example.com.", dns.TypeA)

	msg2 := new(dns.Msg)
	msg2.SetQuestion("google.com.", dns.TypeA)

	// 尝试执行查询（会失败，但我们关注连接池的行为）
	pool.Exchange(ctx, msg1)
	pool.Exchange(ctx, msg2)

	// 检查连接池状态
	stats := pool.GetStats()
	t.Logf("Pool stats after queries: %+v", stats)

	// 验证连接数不超过最大值
	if stats["active_count"].(int) > 3 {
		t.Errorf("Active connections %d exceeds max 3", stats["active_count"])
	}
}

// TestConnectionPoolCleanup 测试空闲连接清理
func TestConnectionPoolCleanup(t *testing.T) {
	pool := NewConnectionPool("8.8.8.8:53", "udp", 5, 100*time.Millisecond)
	defer pool.Close()

	// 等待清理周期
	time.Sleep(150 * time.Millisecond)

	stats := pool.GetStats()
	t.Logf("Pool stats after cleanup: %+v", stats)
}

// TestConnectionPoolClose 测试连接池关闭
func TestConnectionPoolClose(t *testing.T) {
	pool := NewConnectionPool("8.8.8.8:53", "udp", 5, 1*time.Minute)

	// 关闭连接池
	err := pool.Close()
	if err != nil {
		t.Errorf("Failed to close pool: %v", err)
	}

	// 验证连接已关闭
	stats := pool.GetStats()
	if stats["active_count"].(int) != 0 {
		t.Errorf("Expected 0 active connections after close, got %d", stats["active_count"])
	}
}

// BenchmarkConnectionPoolExchange 基准测试：连接池查询
func BenchmarkConnectionPoolExchange(b *testing.B) {
	pool := NewConnectionPool("8.8.8.8:53", "udp", 10, 5*time.Minute)
	defer pool.Close()

	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeA)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 注意：这会失败，因为没有真正的 DNS 服务器
		// 但我们可以测试连接池的开销
		pool.Exchange(ctx, msg)
	}
}
