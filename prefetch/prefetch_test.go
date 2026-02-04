package prefetch

import (
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/ping"
	"smartdnssort/stats"
	"strconv"
	"sync"
	"testing"
	"time"

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
	key := domain + "#" + strconv.Itoa(int(qtype))
	entry, exists := m.sortedCache[key]
	return entry, exists
}

func (m *mockCache) GetRaw(domain string, qtype uint16) (*cache.RawCacheEntry, bool) {
	key := domain + "#" + strconv.Itoa(int(qtype))
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
		Enabled: true,
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

// TestConcurrentFailureCountsAccess tests that failureCounts is safely accessed from multiple goroutines
func TestConcurrentFailureCountsAccess(t *testing.T) {
	prefetchCfg := &config.PrefetchConfig{
		Enabled: true,
	}

	mockStats := &mockStats{}
	mockCache := &mockCache{}
	mockRefresher := &mockRefresher{}

	p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)

	// Simulate concurrent access to failureCounts from multiple goroutines
	var wg sync.WaitGroup
	numGoroutines := 10
	operationsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operationsPerGoroutine; j++ {
				// Simulate ping results being reported
				p.ReportPingResultWithDomain("test.com", []ping.Result{
					{IP: "1.1.1.1", RTT: 10, Loss: 0},
				})
			}
		}(i)
	}

	// Also call Stop() while other goroutines are running
	go func() {
		p.Stop()
	}()

	wg.Wait()
	// If we reach here without panic or deadlock, the test passes
	t.Log("Concurrent failure counts access test passed")
}

// TestStopConcurrency tests that Stop() is safe to call while other operations are in progress
func TestStopConcurrency(t *testing.T) {
	prefetchCfg := &config.PrefetchConfig{
		Enabled: true,
	}

	mockStats := &mockStats{}
	mockCache := &mockCache{}
	mockRefresher := &mockRefresher{}

	p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)

	var wg sync.WaitGroup

	// Goroutine 1: Record access
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			p.RecordAccess("test.com", 300)
		}
	}()

	// Goroutine 2: Report ping results
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			p.ReportPingResultWithDomain("test.com", []ping.Result{
				{IP: "1.1.1.1", RTT: 10, Loss: 0},
			})
		}
	}()

	// Goroutine 3: Call Stop
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.Stop()
	}()

	wg.Wait()
	t.Log("Stop concurrency test passed")
}

// TestConcurrentScoringAccess tests that scoring operations are thread-safe
func TestConcurrentScoringSampling(t *testing.T) {
	prefetchCfg := &config.PrefetchConfig{
		Enabled: true,
	}

	mockStats := &mockStats{}
	mockCache := &mockCache{
		rawCache: map[string]*cache.RawCacheEntry{
			"test.com#1": {UpstreamTTL: 300, IPs: []string{"1.1.1.1"}},
		},
	}
	mockRefresher := &mockRefresher{}

	p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)

	var wg sync.WaitGroup

	// Goroutine 1: Continuously record access
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			p.RecordAccess("test.com", 300)
		}
	}()

	// Goroutine 2: Continuously run sampling
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			p.runSampling()
		}
	}()

	wg.Wait()
	// If we reach here without panic or data race, the test passes
	t.Log("Concurrent scoring sampling test passed")
}

// TestEvictionUnderCapacityLimit tests that eviction works correctly when capacity is exceeded
func TestEvictionUnderCapacityLimit(t *testing.T) {
	prefetchCfg := &config.PrefetchConfig{
		Enabled: true,
	}

	mockStats := &mockStats{}
	mockCache := &mockCache{}
	mockRefresher := &mockRefresher{}

	p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)

	// Fill the score table to near capacity
	for i := 0; i < MaxScoreTableSize-10; i++ {
		domain := "domain" + strconv.Itoa(i) + ".com"
		p.RecordAccess(domain, 300)
	}

	p.scoreMu.RLock()
	initialSize := len(p.scoreTable)
	p.scoreMu.RUnlock()

	assert.Greater(t, initialSize, MaxScoreTableSize-100)

	// Add more domains to trigger eviction
	for i := 0; i < 100; i++ {
		domain := "new" + strconv.Itoa(i) + ".com"
		p.RecordAccess(domain, 300)
	}

	p.scoreMu.RLock()
	finalSize := len(p.scoreTable)
	p.scoreMu.RUnlock()

	// Should not exceed capacity
	assert.LessOrEqual(t, finalSize, MaxScoreTableSize)
	// Should have evicted some entries
	assert.Less(t, finalSize, initialSize+100)
}

// TestDecayAccuracy verifies that decay calculation is mathematically correct
func TestDecayAccuracy(t *testing.T) {
	prefetchCfg := &config.PrefetchConfig{
		Enabled: true,
	}

	mockStats := &mockStats{}
	mockCache := &mockCache{}
	mockRefresher := &mockRefresher{}

	p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)

	// Manually set up a score entry for testing
	p.scoreMu.Lock()
	p.scoreTable["test.com"] = &ScoreEntry{
		RawScore:        100.0,
		LastUpdateCycle: 100,
		LastAccess:      time.Now(),
	}
	p.scoreMu.Unlock()

	// Test decay at cycle 101 (1 cycle later)
	cycle101 := int64(101)
	entry := p.scoreTable["test.com"]
	p.applyLazyDecay(entry, cycle101)
	expected101 := 100.0 * 0.93 // 93.0
	assert.InDelta(t, expected101, entry.RawScore, 0.01)
	assert.Equal(t, cycle101, entry.LastUpdateCycle)

	// Test decay at cycle 102 (2 cycles later from original)
	cycle102 := int64(102)
	p.applyLazyDecay(entry, cycle102)
	expected102 := 100.0 * 0.93 * 0.93 // 86.49
	assert.InDelta(t, expected102, entry.RawScore, 0.01)
	assert.Equal(t, cycle102, entry.LastUpdateCycle)
}

// TestBlacklistExponentialBackoff verifies exponential backoff for failures
func TestBlacklistExponentialBackoff(t *testing.T) {
	prefetchCfg := &config.PrefetchConfig{
		Enabled: true,
	}

	mockStats := &mockStats{}
	mockCache := &mockCache{}
	mockRefresher := &mockRefresher{}

	p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)

	domain := "test.com"
	ip := "1.1.1.1"
	key := domain + "#" + ip

	// Simulate multiple failures
	failureResults := []ping.Result{
		{IP: ip, RTT: 999999, Loss: 100.0},
	}

	now := time.Now().Unix()

	// First failure: ban for 600 seconds
	p.ReportPingResultWithDomain(domain, failureResults)
	p.blacklistMu.RLock()
	expireTime1 := p.blacklist[key]
	p.blacklistMu.RUnlock()
	assert.Equal(t, now+600, expireTime1)

	// Second failure: ban for 1200 seconds
	p.ReportPingResultWithDomain(domain, failureResults)
	p.blacklistMu.RLock()
	expireTime2 := p.blacklist[key]
	p.blacklistMu.RUnlock()
	assert.Equal(t, now+1200, expireTime2)

	// Third failure: ban for 2400 seconds
	p.ReportPingResultWithDomain(domain, failureResults)
	p.blacklistMu.RLock()
	expireTime3 := p.blacklist[key]
	p.blacklistMu.RUnlock()
	assert.Equal(t, now+2400, expireTime3)
}

// TestEvictedDomainNotRefreshed verifies that evicted domains are not refreshed
func TestEvictedDomainNotRefreshed(t *testing.T) {
	prefetchCfg := &config.PrefetchConfig{
		Enabled: true,
	}

	mockStats := &mockStats{}
	mockCache := &mockCache{
		rawCache: map[string]*cache.RawCacheEntry{
			"test.com#1": {UpstreamTTL: 300, IPs: []string{"1.1.1.1"}},
		},
	}
	mockRefresher := &mockRefresher{}

	p := NewPrefetcher(prefetchCfg, mockStats, mockCache, mockRefresher)

	// Add a domain to score table
	p.RecordAccess("test.com", 300)

	// Verify it's in the table
	p.scoreMu.RLock()
	_, exists := p.scoreTable["test.com"]
	p.scoreMu.RUnlock()
	assert.True(t, exists)

	// Manually evict it
	p.scoreMu.Lock()
	delete(p.scoreTable, "test.com")
	p.scoreMu.Unlock()

	// Run sampling - should not crash and should not refresh the evicted domain
	p.runSampling()

	// Verify domain was not refreshed
	assert.False(t, mockRefresher.wasRefreshed("test.com"))
}
