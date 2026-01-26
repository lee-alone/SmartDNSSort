package transport

import (
	"context"
	"net"

	"github.com/miekg/dns"
)

type UDP struct {
	address string
}

func NewUDP(address string) *UDP {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "53")
	}
	return &UDP{address: address}
}

func (t *UDP) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	client := &dns.Client{
		Net: "udp",
	}

	// [优化] EDNS0 自适应：限制 UDP Payload Size 为 1232 字节
	// 1232 是 IPv6 MTU (1280) 减去 IPv6 头部 (40) 和 UDP 头部 (8) 的安全值
	// 这能有效避免在双栈环境下因分片导致的丢包
	opt := msg.IsEdns0()
	if opt == nil {
		msg.SetEdns0(1232, false)
	} else if opt.UDPSize() > 1232 {
		opt.SetUDPSize(1232)
	}

	r, _, err := client.ExchangeContext(ctx, msg, t.address)
	return r, err
}

func (t *UDP) Address() string {
	return "udp://" + t.address
}

func (t *UDP) Protocol() string {
	return "udp"
}
