package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"smartdnssort/upstream/bootstrap"
	"time"

	"github.com/miekg/dns"
)

type DoH struct {
	url       string
	client    *http.Client
	bootstrap *bootstrap.Resolver
}

func NewDoH(urlStr string, boot *bootstrap.Resolver) (*DoH, error) {
	_, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	// Create a custom transport
	transport := &http.Transport{
		ForceAttemptHTTP2: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Split host and port
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			// Resolve host using bootstrap resolver
			ip, err := boot.Resolve(ctx, host)
			if err != nil {
				return nil, err
			}

			// Dial to the resolved IP
			dialer := &net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
		},
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
	}

	return &DoH{
		url:       urlStr,
		bootstrap: boot,
		client: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Second, // Default timeout, will be overridden by context
		},
	}, nil
}

func (t *DoH) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	// Pack DNS message
	buf, err := msg.Pack()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("doh request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	r := new(dns.Msg)
	if err := r.Unpack(body); err != nil {
		return nil, err
	}

	r.Id = msg.Id // Restore ID as DoH might not preserve it or it's not relevant in HTTP
	return r, nil
}

func (t *DoH) Address() string {
	return t.url
}

func (t *DoH) Protocol() string {
	return "doh"
}
