package transport

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"

	"github.com/miekg/dns"
)

type DoT struct {
	address    string
	serverName string
}

func NewDoT(addr string) *DoT {
	// addr might be "tls://dns.google:853" or just "dns.google:853"
	// We need to parse it to get hostname for SNI

	u, err := url.Parse(addr)
	var host, port string
	if err == nil && u.Scheme != "" {
		host = u.Hostname()
		port = u.Port()
	} else {
		host, port, _ = net.SplitHostPort(addr)
	}

	if port == "" {
		port = "853"
	}

	// If host is empty (e.g. parse error), try to use original addr as host
	if host == "" {
		host = addr
	}

	return &DoT{
		address:    net.JoinHostPort(host, port),
		serverName: host,
	}
}

func (t *DoT) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	client := &dns.Client{
		Net: "tcp-tls",
		TLSConfig: &tls.Config{
			ServerName: t.serverName,
		},
	}
	r, _, err := client.ExchangeContext(ctx, msg, t.address)
	return r, err
}

func (t *DoT) Address() string {
	return "tls://" + t.address
}

func (t *DoT) Protocol() string {
	return "dot"
}
