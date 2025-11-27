package transport

import (
	"context"
	"net"

	"github.com/miekg/dns"
)

type TCP struct {
	address string
}

func NewTCP(address string) *TCP {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "53")
	}
	return &TCP{address: address}
}

func (t *TCP) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	client := &dns.Client{
		Net: "tcp",
	}
	r, _, err := client.ExchangeContext(ctx, msg, t.address)
	return r, err
}

func (t *TCP) Address() string {
	return "tcp://" + t.address
}

func (t *TCP) Protocol() string {
	return "tcp"
}
