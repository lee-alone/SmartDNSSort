package resolver

import (
	"smartdnssort/config"
	"testing"

	"github.com/miekg/dns"
)

func TestNewServer(t *testing.T) {
	// 测试创建新的 DNS 服务器
	cfg := &config.RecursiveConfig{
		Enabled: true,
		Port:    5335,
	}

	server, err := NewServer(cfg, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	if server == nil {
		t.Error("server is nil")
	}
	if server.config == nil {
		t.Error("config is nil")
	}
	if server.resolver == nil {
		t.Error("resolver is nil")
	}
}

func TestNewServer_NilConfig(t *testing.T) {
	// 测试使用 nil 配置创建服务器
	_, err := NewServer(nil, nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestServerStart(t *testing.T) {
	// 测试启动服务器
	cfg := &config.RecursiveConfig{
		Enabled: true,
		Port:    15335, // 使用非标端口测试
	}

	server, err := NewServer(cfg, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	err = server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !server.IsRunning() {
		t.Error("server should be running")
	}

	server.Stop()
}

func TestServerStop(t *testing.T) {
	// 测试停止服务器
	cfg := &config.RecursiveConfig{
		Enabled: true,
		Port:    15336,
	}

	server, err := NewServer(cfg, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	server.Start()

	if !server.IsRunning() {
		t.Error("server should be running")
	}

	err = server.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if server.IsRunning() {
		t.Error("server should not be running")
	}
}

func TestServerStartAlreadyRunning(t *testing.T) {
	// 测试启动已运行的服务器
	cfg := &config.RecursiveConfig{
		Enabled: true,
		Port:    15337,
	}

	server, err := NewServer(cfg, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	server.Start()
	defer server.Stop()

	// 尝试再次启动
	err = server.Start()
	if err == nil {
		t.Error("expected error when starting already running server")
	}
}

func TestServerStopNotRunning(t *testing.T) {
	// 测试停止未运行的服务器
	cfg := &config.RecursiveConfig{}

	server, err := NewServer(cfg, nil)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// 尝试停止未运行的服务器
	err = server.Stop()
	if err == nil {
		t.Error("expected error when stopping non-running server")
	}
}

func TestHandleQuery_EmptyQuestion(t *testing.T) {
	// 测试处理空问题的查询
	cfg := &config.RecursiveConfig{}
	server, _ := NewServer(cfg, nil)

	msg := &dns.Msg{}
	msg.SetQuestion("example.com.", dns.TypeA)

	response := server.handleQuery(msg)

	if response == nil {
		t.Error("response is nil")
	}
	if response.Rcode != dns.RcodeSuccess && response.Rcode != dns.RcodeServerFailure {
		t.Errorf("unexpected rcode: %d", response.Rcode)
	}
}

func TestHandleQuery_RecursiveMode(t *testing.T) {
	// 测试递归模式下的查询处理
	cfg := &config.RecursiveConfig{Enabled: true}
	server, _ := NewServer(cfg, nil)

	msg := &dns.Msg{}
	msg.SetQuestion("example.com.", dns.TypeA)

	response := server.handleQuery(msg)

	if response == nil {
		t.Error("response is nil")
	}
	if !response.Response {
		t.Error("response should be a response message")
	}
}

// TestHandleQuery_ForwardingMode removed as forwarding mode is no longer supported in standalone resolver

func TestServerGetStats(t *testing.T) {
	// 测试获取统计信息
	cfg := &config.RecursiveConfig{}
	server, _ := NewServer(cfg, nil)

	stats := server.GetStats()

	if stats == nil {
		t.Error("stats is nil")
	}
	if _, ok := stats["running"]; !ok {
		t.Error("running not in stats")
	}
	if _, ok := stats["resolver"]; !ok {
		t.Error("resolver not in stats")
	}
}

func TestGetResolver(t *testing.T) {
	// 测试获取递归解析器
	cfg := &config.RecursiveConfig{}
	server, _ := NewServer(cfg, nil)

	resolver := server.GetResolver()

	if resolver == nil {
		t.Error("resolver is nil")
	}
}

// TestGetTransport removed

func TestIsRunning(t *testing.T) {
	// 测试检查服务器是否运行中
	cfg := &config.RecursiveConfig{Port: 15338}
	server, _ := NewServer(cfg, nil)

	if server.IsRunning() {
		t.Error("server should not be running initially")
	}

	server.Start()
	defer server.Stop()

	if !server.IsRunning() {
		t.Error("server should be running after start")
	}
}

func TestHandleQuery_MultipleQuestions(t *testing.T) {
	// 测试处理多个问题的查询
	cfg := &config.RecursiveConfig{Enabled: true}
	server, _ := NewServer(cfg, nil)

	msg := &dns.Msg{}
	msg.SetQuestion("example.com.", dns.TypeA)
	msg.Question = append(msg.Question, dns.Question{
		Name:   "example.org.",
		Qtype:  dns.TypeMX,
		Qclass: dns.ClassINET,
	})

	response := server.handleQuery(msg)

	if response == nil {
		t.Error("response is nil")
	}
	if !response.Response {
		t.Error("response should be a response message")
	}
}

func TestServerConcurrentQueries(t *testing.T) {
	// 测试并发查询处理
	cfg := &config.RecursiveConfig{Enabled: true}
	server, _ := NewServer(cfg, nil)

	// 并发处理查询
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			msg := &dns.Msg{}
			msg.SetQuestion("example.com.", dns.TypeA)
			response := server.handleQuery(msg)
			if response == nil {
				t.Error("response is nil")
			}
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestHandleQuery_InvalidMessage(t *testing.T) {
	// 测试处理无效的 DNS 消息
	cfg := &config.RecursiveConfig{}
	server, _ := NewServer(cfg, nil)

	msg := &dns.Msg{}
	// 不设置问题，创建一个无效的消息

	response := server.handleQuery(msg)

	if response == nil {
		t.Error("response is nil")
	}
	if response.Rcode != dns.RcodeFormatError {
		t.Errorf("expected rcode FormatError, got %d", response.Rcode)
	}
}

func TestServerContextTimeout(t *testing.T) {
	// 测试查询超时
	cfg := &config.RecursiveConfig{
		QueryTimeout: 1, // 非常短的超时
	}

	server, _ := NewServer(cfg, nil)

	msg := &dns.Msg{}
	msg.SetQuestion("example.com.", dns.TypeA)

	// 这个查询应该在超时后完成
	response := server.handleQuery(msg)

	if response == nil {
		t.Error("response is nil")
	}
}
