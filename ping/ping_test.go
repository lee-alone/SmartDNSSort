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

func TestSortResults(t *testing.T) {
	p := &Pinger{}

	results := []Result{
		{IP: "1.1.1.1", RTT: 200, Loss: 0},
		{IP: "2.2.2.2", RTT: 20, Loss: 30}, // Low RTT but has loss
		{IP: "3.3.3.3", RTT: 500, Loss: 0},
		{IP: "4.4.4.4", RTT: 9999, Loss: 100},
	}

	p.sortResults(results)

	// Expected order:
	// 1. 1.1.1.1 (Loss 0, RTT 200)
	// 2. 3.3.3.3 (Loss 0, RTT 500)
	// 3. 2.2.2.2 (Loss 30)
	// 4. 4.4.4.4 (Loss 100)

	if results[0].IP != "1.1.1.1" {
		t.Errorf("Expected first IP 1.1.1.1, got %s", results[0].IP)
	}
	if results[1].IP != "3.3.3.3" {
		t.Errorf("Expected second IP 3.3.3.3, got %s", results[1].IP)
	}
	if results[2].IP != "2.2.2.2" {
		t.Errorf("Expected third IP 2.2.2.2, got %s", results[2].IP)
	}
	if results[3].IP != "4.4.4.4" {
		t.Errorf("Expected fourth IP 4.4.4.4, got %s", results[3].IP)
	}
}
