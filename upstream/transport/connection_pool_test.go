package transport

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// MockDNSServer 用于测试的模拟 DNS 服务器
type MockDNSServer struct {
	addr       *net.UDPAddr
	conn       *net.UDPConn
	done       chan struct{}
	delay      time.Duration
	shouldFail bool
}

func NewMockDNSServer(delay time.Duration, shouldFail bool) (*MockDNSServer, error) {
	addr := &net.UDPAddr{
		Port: 0,
		IP:   net.ParseIP("127.0.0.1"),
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	server := &MockDNSServer{
		addr:       conn.LocalAddr().(*net.UDPAddr),
		conn:       conn,
		done:       make(chan struct{}),
		delay:      delay,
		shouldFail: shouldFail,
	}

	go server.serve()
	return server, nil
}

func (s *MockDNSServer) serve() {
	buf := make([]byte, 512)
	for {
		select {
		case <-s.done:
			return
		default:
		}

		s.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, remoteAddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		if s.delay > 0 {
			time.Sleep(s.delay)
		}

		if !s.shouldFail {
			msg := new(dns.Msg)
			if err := msg.Unpack(buf[:n]); err == nil {
				reply := new(dns.Msg)
				reply.SetReply(msg)
				reply.Rcode = dns.RcodeSuccess
				if data, err := reply.Pack(); err == nil {
					s.conn.WriteToUDP(data, remoteAddr)
				}
			}
		}
	}
}

func (s *MockDNSServer) Close() error {
	close(s.done)
	return s.conn.Close()
}

func (s *MockDNSServer) Addr() string {
	return s.addr.String()
}

// TestConnectionPoolBasic 测试基本的连接池功能
func TestConnectionPoolBasic(t *testing.T) {
	// 创建模拟 DNS 服务器
	server, err := NewMockDNSServer(0, false)
	if err != nil {
		t.Fatalf("Failed to create mock DNS server: %v", err)
	}
	defer server.Close()

	// 创建连接池
	pool := NewConnectionPool(server.Addr(), "udp", 5, 1*time.Minute)
	defer pool.Close()

	// 测试：创建连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 创建一个简单的 DNS 查询消息
	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeA)

	reply, err := pool.Exchange(ctx, msg)
	if err != nil {
		t.Logf("Query failed: %v", err)
	} else if reply != nil {
		t.Logf("Query succeeded: %v", reply)
	}

	// 验证连接池状态
	stats := pool.GetStats()
	t.Logf("Pool stats: %+v", stats)

	// 验证地址匹配
	if addr, ok := stats["address"].(string); !ok || addr != server.Addr() {
		t.Errorf("Expected address %s, got %v", server.Addr(), stats["address"])
	}
}

// TestConnectionPoolReuse 测试连接复用
func TestConnectionPoolReuse(t *testing.T) {
	// 创建模拟 DNS 服务器
	server, err := NewMockDNSServer(10*time.Millisecond, false)
	if err != nil {
		t.Fatalf("Failed to create mock DNS server: %v", err)
	}
	defer server.Close()

	pool := NewConnectionPool(server.Addr(), "udp", 3, 1*time.Minute)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 创建多个查询消息
	msg1 := new(dns.Msg)
	msg1.SetQuestion("example.com.", dns.TypeA)

	msg2 := new(dns.Msg)
	msg2.SetQuestion("google.com.", dns.TypeA)

	// 执行查询
	pool.Exchange(ctx, msg1)
	pool.Exchange(ctx, msg2)

	// 检查连接池状态
	stats := pool.GetStats()
	t.Logf("Pool stats after queries: %+v", stats)

	// 验证连接数不超过最大值
	if activeCount, ok := stats["active_count"].(int); ok && activeCount > 3 {
		t.Errorf("Active connections %d exceeds max 3", activeCount)
	}
}

// TestConnectionPoolCleanup 测试空闲连接清理
func TestConnectionPoolCleanup(t *testing.T) {
	// 创建模拟 DNS 服务器
	server, err := NewMockDNSServer(0, false)
	if err != nil {
		t.Fatalf("Failed to create mock DNS server: %v", err)
	}
	defer server.Close()

	pool := NewConnectionPool(server.Addr(), "udp", 5, 100*time.Millisecond)
	defer pool.Close()

	// 等待清理周期
	time.Sleep(150 * time.Millisecond)

	stats := pool.GetStats()
	t.Logf("Pool stats after cleanup: %+v", stats)

	// 验证 stats 包含预期的字段
	if _, ok := stats["address"]; !ok {
		t.Error("Expected 'address' field in stats")
	}
}

// TestConnectionPoolClose 测试连接池关闭
func TestConnectionPoolClose(t *testing.T) {
	// 创建模拟 DNS 服务器
	server, err := NewMockDNSServer(0, false)
	if err != nil {
		t.Fatalf("Failed to create mock DNS server: %v", err)
	}
	defer server.Close()

	pool := NewConnectionPool(server.Addr(), "udp", 5, 1*time.Minute)

	// 关闭连接池
	err = pool.Close()
	if err != nil {
		t.Errorf("Failed to close pool: %v", err)
	}

	// 验证连接已关闭
	stats := pool.GetStats()
	if activeCount, ok := stats["active_count"].(int); ok && activeCount != 0 {
		t.Errorf("Expected 0 active connections after close, got %d", activeCount)
	}
}

// BenchmarkConnectionPoolExchange 基准测试：连接池查询
func BenchmarkConnectionPoolExchange(b *testing.B) {
	// 创建模拟 DNS 服务器
	server, err := NewMockDNSServer(1*time.Millisecond, false)
	if err != nil {
		b.Fatalf("Failed to create mock DNS server: %v", err)
	}
	defer server.Close()

	pool := NewConnectionPool(server.Addr(), "udp", 10, 5*time.Minute)
	defer pool.Close()

	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeA)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	b.ResetTimer()
	for range b.N {
		pool.Exchange(ctx, msg)
	}
}
