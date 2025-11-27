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
	r, _, err := client.ExchangeContext(ctx, msg, t.address)
	return r, err
}

func (t *UDP) Address() string {
	return "udp://" + t.address
}

func (t *UDP) Protocol() string {
	return "udp"
}
