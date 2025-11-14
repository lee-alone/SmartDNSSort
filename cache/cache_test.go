package cache

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestCache(t *testing.T) {
	c := NewCache()

	entry := &CacheEntry{
		IPs:       []string{"1.1.1.1", "8.8.8.8"},
		RTTs:      []int{10, 20},
		Timestamp: time.Now(),
		TTL:       300,
	}

	c.Set("example.com", dns.TypeA, entry)

	retrieved, ok := c.Get("example.com", dns.TypeA)
	if !ok {
		t.Fatal("Expected to find cache entry")
	}

	if len(retrieved.IPs) != 2 {
		t.Errorf("Expected 2 IPs, got %d", len(retrieved.IPs))
	}
}

func TestCacheExpiration(t *testing.T) {
	c := NewCache()

	entry := &CacheEntry{
		IPs:       []string{"1.1.1.1"},
		RTTs:      []int{50},
		Timestamp: time.Now().Add(-400 * time.Second),
		TTL:       300,
	}

	c.Set("expired.com", dns.TypeA, entry)

	_, ok := c.Get("expired.com", dns.TypeA)
	if ok {
		t.Fatal("Expected expired entry to be invalid")
	}
}
