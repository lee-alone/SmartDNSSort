package prefetch

import (
	"sync"
	"time"

	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"smartdnssort/stats"
)

// Constants for math model
const (
	MaxScoreTableSize = 60000
	MaxIPStatsSize    = 200000
	DecayBase         = 0.93
	DecayCycleSeconds = 300 // 5 minutes
	EligibilityTTL    = 300
	SimHashThreshold  = 14 // Max Hamming distance allowed
)

// Refresher defines the interface for an object that can refresh a domain's cache.
type Refresher interface {
	RefreshDomain(domain string, qtype uint16)
}

// Cache defines the interface for the cache that the prefetcher needs to interact with.
type Cache interface {
	GetSorted(domain string, qtype uint16) (*cache.SortedCacheEntry, bool)
	GetRaw(domain string, qtype uint16) (*cache.RawCacheEntry, bool)
}

// Stats defines the interface for the stats collector that the prefetcher needs to interact with.
type Stats interface {
	GetTopDomains(limit int) []stats.DomainCount
}

// ScoreEntry tracks the priority score and eligibility of a domain
type ScoreEntry struct {
	RawScore        float64
	LastUpdateCycle int64
	LastSimHash     uint64
	LastAccess      time.Time // For LRU eviction if strictly needed, though we use MinScore for eviction usually
}

// IPStat tracks the historical delay of an IP
type IPStat struct {
	LastDelay int // ms
	Updated   time.Time
}

// Prefetcher is responsible for prefetching popular domains based on a mathematical model.
type Prefetcher struct {
	cfg       *config.PrefetchConfig
	stats     Stats
	cache     Cache
	refresher Refresher
	stopChan  chan struct{}
	wg        sync.WaitGroup

	// Score Table: Domain -> Score (Max 60k)
	scoreMu    sync.RWMutex
	scoreTable map[string]*ScoreEntry

	// IP Stats: IP -> Delay (Max 200k)
	ipStatsMu sync.RWMutex
	ipStats   map[string]*IPStat

	// Blacklist: Domain#IP -> BanExpireUnix
	blacklistMu sync.RWMutex
	blacklist   map[string]int64

	// Failure Counts for exponential backoff: Domain#IP -> count
	failureCounts map[string]int
}

// NewPrefetcher creates a new Prefetcher.
func NewPrefetcher(cfg *config.PrefetchConfig, s Stats, c Cache, r Refresher) *Prefetcher {
	return &Prefetcher{
		cfg:           cfg,
		stats:         s,
		cache:         c,
		refresher:     r,
		stopChan:      make(chan struct{}),
		scoreTable:    make(map[string]*ScoreEntry),
		ipStats:       make(map[string]*IPStat),
		blacklist:     make(map[string]int64),
		failureCounts: make(map[string]int),
	}
}

// Start begins the prefetcher background tasks.
func (p *Prefetcher) Start() {
	if !p.cfg.Enabled {
		logger.Info("[Prefetcher] Disabled.")
		return
	}
	p.wg.Add(1)
	go p.prefetchLoop()
	logger.Info("[Prefetcher] Started with math model (Cap: 60k).")
}

// Stop gracefully stops the prefetcher.
func (p *Prefetcher) Stop() {
	if !p.cfg.Enabled {
		return
	}
	close(p.stopChan)
	p.wg.Wait()
	logger.Info("[Prefetcher] Stopped.")
}

// prefetchLoop handles periodic sampling and refreshing.
func (p *Prefetcher) prefetchLoop() {
	defer p.wg.Done()

	// Initial sleep to let cache warm up
	time.Sleep(30 * time.Second)

	for {
		// Run Sampling
		p.runSampling()

		// Wait for next cycle
		sleepDuration := 300*time.Second + time.Duration(time.Now().UnixNano()%600)*time.Second

		select {
		case <-time.After(sleepDuration):
			continue
		case <-p.stopChan:
			return
		}
	}
}

// RecordAccess is called when a domain is queried by a real client.
func (p *Prefetcher) RecordAccess(domain string, ttl uint32) {
	// Formula: w(d) = min(3.0, TTL/3600)
	w := float64(ttl) / 3600.0
	if w > 3.0 {
		w = 3.0
	}

	p.updateScore(domain, w)
}

// ReportPingResults public for backward compatibility if needed
func (p *Prefetcher) ReportPingResults(results []ping.Result) {
}

// IsTopDomain checks if a domain is currently considered a top domain.
func (p *Prefetcher) IsTopDomain(domain string) bool {
	p.scoreMu.RLock()
	_, exists := p.scoreTable[domain]
	p.scoreMu.RUnlock()
	return exists
}
