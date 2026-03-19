package bootstrap

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"smartdnssort/logger"

	"github.com/miekg/dns"
)

type Resolver struct {
	servers        []string
	cache          sync.Map // map[string]*cacheEntry
	networkChecker NetworkHealthChecker
	circuitOpen    bool
	circuitMu      sync.RWMutex
	lastFailure    time.Time
}

// NetworkHealthChecker 网络健康检查器接口
type NetworkHealthChecker interface {
	IsNetworkHealthy() bool
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

// SetNetworkHealthChecker 设置网络健康检查器
func (r *Resolver) SetNetworkHealthChecker(checker NetworkHealthChecker) {
	r.circuitMu.Lock()
	defer r.circuitMu.Unlock()
	r.networkChecker = checker
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

	// 3. Circuit breaker: check network health before querying
	r.circuitMu.RLock()
	circuitOpen := r.circuitOpen
	r.circuitMu.RUnlock()

	if circuitOpen {
		// Circuit is open, check if we should try again (half-open state)
		r.circuitMu.Lock()
		if time.Since(r.lastFailure) > 30*time.Second {
			// Half-open: try one query to see if network recovered
			logger.Debugf("[Bootstrap] Circuit half-open, attempting recovery for %s", host)
			r.circuitOpen = false
			r.circuitMu.Unlock()
		} else {
			r.circuitMu.Unlock()
			logger.Debugf("[Bootstrap] Circuit open, skipping bootstrap resolution for %s", host)
			return "", fmt.Errorf("bootstrap circuit breaker open, network unavailable")
		}
	}

	// 4. Network health check (if checker is available)
	if r.networkChecker != nil && !r.networkChecker.IsNetworkHealthy() {
		logger.Debugf("[Bootstrap] Network unhealthy, skipping bootstrap resolution for %s", host)
		return "", fmt.Errorf("network unhealthy, bootstrap resolution skipped")
	}

	// 5. Query DNS
	// 简单轮询
	var lastErr error
	var success bool
	for _, server := range r.servers {
		ip, err := r.queryOne(ctx, server, host)
		if err == nil {
			// Cache result (TTL 10 min for simplicity, or parse from msg)
			r.cache.Store(host, &cacheEntry{
				ip:        ip,
				expiresAt: time.Now().Add(10 * time.Minute),
			})

			// Reset circuit breaker on success
			r.circuitMu.Lock()
			if r.circuitOpen {
				logger.Info("[Bootstrap] Circuit breaker reset after successful resolution")
				r.circuitOpen = false
			}
			r.circuitMu.Unlock()

			success = true
			return ip, nil
		}
		lastErr = err
	}

	// All queries failed, open circuit breaker
	if !success {
		r.circuitMu.Lock()
		if !r.circuitOpen {
			logger.Warnf("[Bootstrap] All bootstrap servers failed, opening circuit breaker for %s", host)
			r.circuitOpen = true
			r.lastFailure = time.Now()
		}
		r.circuitMu.Unlock()
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
