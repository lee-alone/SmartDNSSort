package prefetch

import (
	"hash/fnv"
	"math"
	"math/bits"
	"sort"
	"strings"
	"sync"
	"time"

	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"smartdnssort/stats"

	"github.com/miekg/dns"
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

// runSampling selects domains to refresh.
func (p *Prefetcher) runSampling() {
	p.scoreMu.Lock()
	tableSize := len(p.scoreTable)

	// S_sample = max(500, min(1500, tableSize >> 5))
	sampleCount := tableSize >> 5 // Divide by 32
	if sampleCount < 500 {
		sampleCount = 500
	}
	if sampleCount > 1500 {
		sampleCount = 1500
	}

	// Extract all domains and scores (with lazy decay applied)
	type sampleItem struct {
		domain string
		score  float64
	}
	items := make([]sampleItem, 0, tableSize)
	currentCycle := time.Now().Unix() / DecayCycleSeconds

	for domain, entry := range p.scoreTable {
		p.applyLazyDecay(entry, currentCycle)
		items = append(items, sampleItem{domain: domain, score: entry.RawScore})
	}
	p.scoreMu.Unlock()

	if len(items) == 0 {
		return
	}

	// Sort by Score Descending
	sort.Slice(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})

	// Pick top N
	count := 0
	for _, item := range items {
		if count >= sampleCount {
			break
		}

		if p.checkEligibility(item.domain) {
			p.refresher.RefreshDomain(item.domain, dns.TypeA)
			p.refresher.RefreshDomain(item.domain, dns.TypeAAAA)
			count++
		}
	}
	logger.Debugf("[Prefetcher] Sampled and refreshed %d domains (Table Size: %d)", count, tableSize)
}

// checkEligibility implements the mathematical eligibility check.
func (p *Prefetcher) checkEligibility(domain string) bool {
	raw, ok := p.cache.GetRaw(domain, dns.TypeA) // Check A record primarily
	if !ok {
		return false // Not in cache -> Not eligible/Unknown
	}

	currentTTL := raw.UpstreamTTL
	if currentTTL < EligibilityTTL {
		return false
	}

	p.scoreMu.RLock()
	entry, exists := p.scoreTable[domain]
	p.scoreMu.RUnlock()

	if !exists {
		return false
	}

	currentHash := calculateSimHash(raw.IPs)
	// If LastSimHash is 0 (first time), assume eligible
	if entry.LastSimHash == 0 {
		return true
	}

	distance := hammingDistance(entry.LastSimHash, currentHash)
	return distance <= SimHashThreshold
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

// updateScore updates the score with lazy decay and strict capacity check.
func (p *Prefetcher) updateScore(domain string, weight float64) {
	p.scoreMu.Lock()
	defer p.scoreMu.Unlock()

	currentCycle := time.Now().Unix() / DecayCycleSeconds

	entry, exists := p.scoreTable[domain]
	if !exists {
		// Capacity Check
		if len(p.scoreTable) >= MaxScoreTableSize {
			p.evictMsg()
		}

		entry = &ScoreEntry{
			RawScore:        0,
			LastUpdateCycle: currentCycle,
			LastAccess:      time.Now(),
		}
		p.scoreTable[domain] = entry
	}

	// Lazy Decay
	p.applyLazyDecay(entry, currentCycle)

	// Add Score
	entry.RawScore += weight
	entry.LastAccess = time.Now()
}

// applyLazyDecay decays the score based on cycles passed.
func (p *Prefetcher) applyLazyDecay(entry *ScoreEntry, currentCycle int64) {
	delta := currentCycle - entry.LastUpdateCycle
	if delta > 0 {
		factor := math.Pow(DecayBase, float64(delta))
		entry.RawScore *= factor
		entry.LastUpdateCycle = currentCycle
	}
}

// evictMsg removes the lowest score item.
func (p *Prefetcher) evictMsg() {
	var minScore float64 = math.MaxFloat64
	var minDomain string

	for d, e := range p.scoreTable {
		if e.RawScore < minScore {
			minScore = e.RawScore
			minDomain = d
		}
	}

	if minDomain != "" {
		delete(p.scoreTable, minDomain)
	}
}

// ReportPingResults public for backward compatibility if needed
func (p *Prefetcher) ReportPingResults(results []ping.Result) {
}

// ReportPingResultWithDomain handles the granular blacklist update.
func (p *Prefetcher) ReportPingResultWithDomain(domain string, results []ping.Result) {
	now := time.Now()

	p.ipStatsMu.Lock()

	// Update IP Stats
	for _, res := range results {
		if res.Loss == 0 {
			// Success
			if len(p.ipStats) >= MaxIPStatsSize {
				for k := range p.ipStats {
					delete(p.ipStats, k)
					break
				}
			}
			p.ipStats[res.IP] = &IPStat{
				LastDelay: res.RTT,
				Updated:   now,
			}
		}
	}
	p.ipStatsMu.Unlock()

	// Update Blacklist
	p.blacklistMu.Lock()
	defer p.blacklistMu.Unlock()

	for _, res := range results {
		key := domain + "#" + res.IP
		if res.Loss == 100.0 {
			// Failure
			k := p.failureCounts[key] + 1
			p.failureCounts[key] = k

			// ban_seconds = 600 * 2^(k-1)
			banSeconds := 600 * int64(math.Pow(2, float64(k-1)))
			p.blacklist[key] = now.Unix() + banSeconds
			logger.Debugf("[Prefetcher] Banned %s for %ds (Failures: %d)", key, banSeconds, k)
		} else {
			// Success - Clear Ban
			if _, ok := p.failureCounts[key]; ok {
				delete(p.failureCounts, key)
				delete(p.blacklist, key)
			}
		}
	}
}

// GetFallbackRank returns a sorted list of IPs for the zero-traffic fallback.
func (p *Prefetcher) GetFallbackRank(domain string, ips []string) []string {
	if len(ips) == 0 {
		return ips
	}

	p.blacklistMu.RLock()
	// Emergency Unban Check
	bannedCount := 0
	availableCount := 0

	for _, ip := range ips {
		key := domain + "#" + ip
		if expire, ok := p.blacklist[key]; ok && expire > time.Now().Unix() {
			bannedCount++
		} else {
			availableCount++
		}
	}

	shouldEmergencyUnban := availableCount <= 1 && bannedCount >= 6
	p.blacklistMu.RUnlock()

	if shouldEmergencyUnban {
		logger.Warnf("[Prefetcher] Emergency Unban triggered for %s (Avail: %d, Banned: %d)", domain, availableCount, bannedCount)
		p.blacklistMu.Lock()
		for _, ip := range ips {
			key := domain + "#" + ip
			delete(p.blacklist, key)
			delete(p.failureCounts, key)
		}
		p.blacklistMu.Unlock()
	}

	// Prepare for sorting
	type rankedIP struct {
		ip             string
		effectiveDelay float64
		isBanned       bool
	}

	ranked := make([]rankedIP, 0, len(ips))
	nowUnix := time.Now().Unix()

	p.ipStatsMu.RLock()
	p.blacklistMu.RLock()

	for _, ip := range ips {
		key := domain + "#" + ip
		isBanned := false
		if expire, ok := p.blacklist[key]; ok && expire > nowUnix {
			isBanned = true
		}

		var delay float64 = 9999
		if stat, ok := p.ipStats[ip]; ok {
			deltaT := time.Since(stat.Updated).Seconds()
			delay = float64(stat.LastDelay) * math.Pow(2, deltaT/2400.0)
		}

		ranked = append(ranked, rankedIP{
			ip:             ip,
			effectiveDelay: delay,
			isBanned:       isBanned,
		})
	}
	p.blacklistMu.RUnlock()
	p.ipStatsMu.RUnlock()

	// Sort
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].isBanned != ranked[j].isBanned {
			return !ranked[i].isBanned
		}
		return ranked[i].effectiveDelay < ranked[j].effectiveDelay
	})

	// Jitter for 4+
	if len(ranked) > 3 {
		tail := ranked[3:]
		seed := int(time.Now().UnixNano())
		for i := len(tail) - 1; i > 0; i-- {
			j := (seed + i) % (i + 1)
			tail[i], tail[j] = tail[j], tail[i]
		}
	}

	result := make([]string, len(ranked))
	for i, r := range ranked {
		result[i] = r.ip
	}

	return result
}

// IsTopDomain checks if a domain is currently considered a top domain.
func (p *Prefetcher) IsTopDomain(domain string) bool {
	p.scoreMu.RLock()
	_, exists := p.scoreTable[domain]
	p.scoreMu.RUnlock()
	return exists
}

// Helpers

// calculateSimHash computes hash of IP set using FNV-1a
func calculateSimHash(ips []string) uint64 {
	if len(ips) == 0 {
		return 0
	}
	sortedIPs := make([]string, len(ips))
	copy(sortedIPs, ips)
	sort.Strings(sortedIPs)

	blob := strings.Join(sortedIPs, "\n")
	h := fnv.New64a()
	h.Write([]byte(blob))
	return h.Sum64()
}

func hammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

func (p *Prefetcher) UpdateSimHash(domain string, ips []string) {
	p.scoreMu.Lock()
	defer p.scoreMu.Unlock()

	if entry, ok := p.scoreTable[domain]; ok {
		entry.LastSimHash = calculateSimHash(ips)
	}
}
