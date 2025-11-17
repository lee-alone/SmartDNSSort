package prefetch

import (
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/stats"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
)

// mockStats allows us to control the output of GetTopDomains.
type mockStats struct {
	stats.Stats
	topDomains []stats.DomainCount
}

func (m *mockStats) GetTopDomains(limit int) []stats.DomainCount {
	if len(m.topDomains) > limit {
		return m.topDomains[:limit]
	}
	return m.topDomains
}

// mockCache allows us to control the output of GetSorted.
type mockCache struct {
	cache.Cache
	sortedCache map[string]*cache.SortedCacheEntry
}

func (m *mockCache) GetSorted(domain string, qtype uint16) (*cache.SortedCacheEntry, bool) {
	key := domain + "#" + string(rune(qtype))
	entry, exists := m.sortedCache[key]
	if !exists || entry.IsExpired() {
		return nil, false
	}
	return entry, true
}

// mockRefresher records calls to RefreshDomain.
type mockRefresher struct {
	refreshedDomains map[string]uint16
	mu               sync.Mutex
}

func (m *mockRefresher) RefreshDomain(domain string, qtype uint16) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refreshedDomains == nil {
		m.refreshedDomains = make(map[string]uint16)
	}
	m.refreshedDomains[domain] = qtype
}

func (m *mockRefresher) wasRefreshed(domain string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.refreshedDomains[domain]
	return exists
}

func TestRunPrefetch(t *testing.T) {
	prefetchCfg := &config.PrefetchConfig{
		Enabled:                  true,
		TopDomainsLimit:          10,
		RefreshBeforeExpireSeconds: 30, // Refresh if expires within 30s
	}

	domainToRefresh := "expiring.com"
	domainToKeep := "fresh.com"

	// --- Scenario 1: One domain is about to expire, one is fresh ---
	t.Run("refreshes expiring domain and keeps fresh one", func(t *testing.T) {
		// Setup mocks
		mockStats := &mockStats{
			topDomains: []stats.DomainCount{
				{Domain: domainToRefresh, Count: 100},
				{Domain: domainToKeep, Count: 90},
			},
		}
		mockCache := &mockCache{
			sortedCache: map[string]*cache.SortedCacheEntry{
				// This one expires in 20 seconds, so it should be refreshed
				"expiring.com#1": {
					IPs:       []string{"1.1.1.1"},
					Timestamp: time.Now().Add(-80 * time.Second),
					TTL:       100, // Expires in 20s
					IsValid:   true,
				},
				// This one expires in 270 seconds, so it should be kept
				"fresh.com#1": {
					IPs:       []string{"2.2.2.2"},
					Timestamp: time.Now().Add(-30 * time.Second),
					TTL:       300, // Expires in 270s
					IsValid:   true,
				},
			},
		}
		mockRefresher := &mockRefresher{}

		// Create prefetcher and run a cycle
		p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)
		_ = p.runPrefetchAndGetNextInterval() // Call the new method

		// Assertions
		assert.True(t, mockRefresher.wasRefreshed(domainToRefresh), "Expected expiring.com to be refreshed")
		assert.False(t, mockRefresher.wasRefreshed(domainToKeep), "Expected fresh.com not to be refreshed")
	})

	// --- Scenario 2: No domains need refreshing ---
	t.Run("does nothing when no domains need refreshing", func(t *testing.T) {
		mockStats := &mockStats{
			topDomains: []stats.DomainCount{{Domain: domainToKeep, Count: 90}},
		}
		mockCache := &mockCache{
			sortedCache: map[string]*cache.SortedCacheEntry{
				"fresh.com#1": {
					IPs:       []string{"2.2.2.2"},
					Timestamp: time.Now(),
					TTL:       300,
					IsValid:   true,
				},
			},
		}
		mockRefresher := &mockRefresher{}

		p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)
		_ = p.runPrefetchAndGetNextInterval() // Call the new method

		assert.False(t, mockRefresher.wasRefreshed(domainToKeep), "Expected no domains to be refreshed")
	})

	// --- Scenario 3: Domain is in stats but not in cache ---
	t.Run("does nothing for domain not in cache", func(t *testing.T) {
		mockStats := &mockStats{
			topDomains: []stats.DomainCount{{Domain: "not-in-cache.com", Count: 80}},
		}
		mockCache := &mockCache{sortedCache: map[string]*cache.SortedCacheEntry{}} // Empty cache
		mockRefresher := &mockRefresher{}

		p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)
		_ = p.runPrefetchAndGetNextInterval() // Call the new method

		assert.False(t, mockRefresher.wasRefreshed("not-in-cache.com"), "Expected no domains to be refreshed")
	})
}
