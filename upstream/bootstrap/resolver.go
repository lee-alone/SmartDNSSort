package bootstrap

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type Resolver struct {
	servers []string
	cache   sync.Map // map[string]*cacheEntry
}

type cacheEntry struct {
	ip        string
	expiresAt time.Time
}

func NewResolver(servers []string) *Resolver {
	return &Resolver{
		servers: servers,
	}
}

// Resolve 解析域名为 IP
// 简单轮询 bootstrap dns
func (r *Resolver) Resolve(ctx context.Context, host string) (string, error) {
	// 1. Check cache
	if val, ok := r.cache.Load(host); ok {
		entry := val.(*cacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.ip, nil
		}
		r.cache.Delete(host)
	}

	// 2. Check if host is already an IP
	if ip := net.ParseIP(host); ip != nil {
		return host, nil
	}

	// 3. Query DNS
	// 简单轮询
	var lastErr error
	for _, server := range r.servers {
		ip, err := r.queryOne(ctx, server, host)
		if err == nil {
			// Cache result (TTL 10 min for simplicity, or parse from msg)
			r.cache.Store(host, &cacheEntry{
				ip:        ip,
				expiresAt: time.Now().Add(10 * time.Minute),
			})
			return ip, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no bootstrap servers available")
}

func (r *Resolver) queryOne(ctx context.Context, server string, host string) (string, error) {
	c := new(dns.Client)
	c.Net = "udp"

	// Ensure server has port
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, "53")
	}

	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(host), dns.TypeA)
	m.RecursionDesired = true

	reply, _, err := c.ExchangeContext(ctx, m, server)
	if err != nil {
		return "", err
	}

	if reply.Rcode != dns.RcodeSuccess {
		return "", fmt.Errorf("dns query failed with rcode: %d", reply.Rcode)
	}

	for _, ans := range reply.Answer {
		if a, ok := ans.(*dns.A); ok {
			return a.A.String(), nil
		}
	}

	return "", fmt.Errorf("no A record found")
}
