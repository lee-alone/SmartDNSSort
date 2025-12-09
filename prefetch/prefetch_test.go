package prefetch

import (
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/stats"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockStats allows us to control the output of GetTopDomains.
type mockStats struct {
	topDomains []stats.DomainCount
}

func (m *mockStats) GetTopDomains(limit int) []stats.DomainCount {
	return m.topDomains
}

// mockCache allows us to control the output of GetSorted.
type mockCache struct {
	sortedCache map[string]*cache.SortedCacheEntry
	rawCache    map[string]*cache.RawCacheEntry
}

func (m *mockCache) GetSorted(domain string, qtype uint16) (*cache.SortedCacheEntry, bool) {
	key := domain + "#" + strconv.Itoa(int(int(qtype)))
	entry, exists := m.sortedCache[key]
	return entry, exists
}

func (m *mockCache) GetRaw(domain string, qtype uint16) (*cache.RawCacheEntry, bool) {
	key := domain + "#" + strconv.Itoa(int(int(qtype)))
	if m.rawCache == nil {
		return nil, false
	}
	entry, exists := m.rawCache[key]
	return entry, exists
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
	if m.refreshedDomains == nil {
		return false
	}
	_, exists := m.refreshedDomains[domain]
	return exists
}

func TestRunPrefetch(t *testing.T) {
	prefetchCfg := &config.PrefetchConfig{
		Enabled:                    true,
		TopDomainsLimit:            10,
		RefreshBeforeExpireSeconds: 30, // Refresh if expires within 30s
	}

	domainToRefresh := "expiring.com"

	// --- Scenario 1: One domain is about to expire, one is fresh ---
	t.Run("refreshes eligible domain", func(t *testing.T) {
		// Setup mocks
		mockStats := &mockStats{}
		mockCache := &mockCache{
			rawCache: map[string]*cache.RawCacheEntry{
				"expiring.com#1": {UpstreamTTL: 300, IPs: []string{"1.1.1.1"}},
			},
		}
		mockRefresher := &mockRefresher{}

		p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)

		// Populate ScoreTable via RecordAccess
		p.RecordAccess(domainToRefresh, 300)

		// Trigger Sampling
		p.runSampling()

		// Assertions:
		// SimHash is 0 -> Eligible.
		// TTL 300 >= 300 -> Eligible.
		// Should refresh (Type A).
		assert.True(t, mockRefresher.wasRefreshed(domainToRefresh))
	})

	t.Run("does not refresh inexperienced domain", func(t *testing.T) {
		// New domain that hasn't been accessed
		mockStats := &mockStats{}
		mockCache := &mockCache{}
		mockRefresher := &mockRefresher{}
		p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)

		p.runSampling()
		assert.False(t, mockRefresher.wasRefreshed("unknown.com"))
	})
}
