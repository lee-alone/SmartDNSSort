package dnsserver

import (
	"net"
	"smartdnssort/config"
	"smartdnssort/stats"
	"testing"

	"github.com/miekg/dns"
)

func TestHandleQuery_CacheStats(t *testing.T) {
	// Initialize dependencies
	cfg := &config.Config{
		DNS: config.DNSConfig{
			ListenPort: 5353,
		},
		Cache: config.CacheConfig{
			FastResponseTTL: 30,
			UserReturnTTL:   60,
			MinTTLSeconds:   10,
			MaxTTLSeconds:   3600,
		},
		Upstream: config.UpstreamConfig{
			TimeoutMs: 100,
		},
		Stats: config.StatsConfig{
			HotDomainsWindowHours:   24,
			HotDomainsBucketMinutes: 60,
			HotDomainsShardCount:    16,
			HotDomainsMaxPerBucket:  5000,
		},
	}
	s := stats.NewStats(&cfg.Stats)
	server := NewServer(cfg, s)

	// Mock domain and IP
	domain := "example.com"
	ip := "1.2.3.4"
	qtype := dns.TypeA

	// Scenario 1: Raw Cache Hit
	// Pre-populate raw cache
	server.cache.SetRaw(domain, qtype, []string{ip}, "", 300)

	// Perform query
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn(domain), qtype)
	w := &mockResponseWriter{}
	server.handleQuery(w, req)

	// Check stats
	statsMap := s.GetStats()
	hits := statsMap["cache_hits"].(int64)
	misses := statsMap["cache_misses"].(int64)

	if hits != 1 {
		t.Errorf("Expected 1 cache hit, got %d", hits)
	}
	if misses != 0 {
		t.Errorf("Expected 0 cache misses, got %d", misses)
	}

	// Scenario 2: Cache Miss
	// Query for a non-existent domain
	req2 := new(dns.Msg)
	req2.SetQuestion(dns.Fqdn("nonexistent.com"), qtype)
	server.handleQuery(w, req2)

	// Check stats again
	statsMap = s.GetStats()
	hits = statsMap["cache_hits"].(int64)
	misses = statsMap["cache_misses"].(int64)

	if hits != 1 {
		t.Errorf("Expected 1 cache hit (unchanged), got %d", hits)
	}
	if misses != 1 {
		t.Errorf("Expected 1 cache miss, got %d", misses)
	}
}

// Mock ResponseWriter
type mockResponseWriter struct{}

func (m *mockResponseWriter) LocalAddr() net.Addr         { return nil }
func (m *mockResponseWriter) RemoteAddr() net.Addr        { return nil }
func (m *mockResponseWriter) WriteMsg(msg *dns.Msg) error { return nil }
func (m *mockResponseWriter) Write([]byte) (int, error)   { return 0, nil }
func (m *mockResponseWriter) Close() error                { return nil }
func (m *mockResponseWriter) TsigStatus() error           { return nil }
func (m *mockResponseWriter) TsigTimersOnly(bool)         {}
func (m *mockResponseWriter) Hijack()                     {}
