package prefetch

import (
	"math"
	"sort"
	"time"

	"smartdnssort/logger"

	"github.com/miekg/dns"
)

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
