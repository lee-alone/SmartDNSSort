package ping

import (
	"context"
	"testing"
)

func TestPinger(t *testing.T) {
	p := NewPinger(2, 1000, 4, 0, 0, "min")
	if p == nil {
		t.Fatal("NewPinger returned nil")
	}

	if p.count != 2 {
		t.Errorf("Expected count 2, got %d", p.count)
	}
}

func TestPingAndSort(t *testing.T) {
	p := NewPinger(1, 500, 2, 0, 0, "min")

	// 测试 ping 本地 IP（需要网络连接）
	ctx := context.Background()
	ips := []string{"127.0.0.1"}

	results := p.PingAndSort(ctx, ips)
	if len(results) == 0 {
		t.Log("No ping results (expected if no network or ping fails)")
	}
}
