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
	currentCycle := time.Now().Unix() / DecayCycleSeconds

	// Phase 1: Snapshot the score table with minimal lock time
	p.scoreMu.RLock()
	tableSize := len(p.scoreTable)

	// 使用配置中的采样数
	sampleCount := p.cfg.MaxRefreshDomains
	if tableSize < 500 {
		// 表太小，按比例但设置上限
		sampleCount = tableSize >> 2
		if sampleCount < 50 {
			sampleCount = 50
		}
	}

	// Extract snapshot of all domains with their current state
	type snapshot struct {
		domain          string
		score           float64
		lastUpdateCycle int64
		stableCycles    int
	}
	items := make([]snapshot, 0, tableSize)

	for domain, entry := range p.scoreTable {
		items = append(items, snapshot{
			domain:          domain,
			score:           entry.RawScore,
			lastUpdateCycle: entry.LastUpdateCycle,
			stableCycles:    entry.StableCycles,
		})
	}
	p.scoreMu.RUnlock()

	if len(items) == 0 {
		return
	}

	// Phase 2: Apply decay calculation (no lock held)
	for i := range items {
		delta := currentCycle - items[i].lastUpdateCycle
		if delta > 0 {
			factor := math.Pow(DecayBase, float64(delta))
			items[i].score *= factor
		}

		// 新增：稳定性惩罚：IP 越稳定，预取优先级越低
		if items[i].stableCycles > 0 {
			// 稳定惩罚因子：每稳定一个周期，乘以配置中的因子
			stabilityPenalty := math.Pow(p.cfg.StabilityPenaltyFactor, float64(items[i].stableCycles))
			items[i].score *= stabilityPenalty
		}
	}

	// Phase 3: Write back the updated scores and cycles
	p.scoreMu.Lock()
	for i := range items {
		if entry, exists := p.scoreTable[items[i].domain]; exists {
			entry.RawScore = items[i].score
			entry.LastUpdateCycle = currentCycle
		}
	}
	p.scoreMu.Unlock()

	// Phase 4: Sort and refresh (no lock held)
	sort.Slice(items, func(i, j int) bool {
		return items[i].score > items[j].score
	})

	// 使用配置中的最小分数阈值
	minScoreThreshold := p.cfg.MinScoreThreshold

	// Pick top N - verify domain still exists before refreshing
	count := 0
	for _, item := range items {
		if count >= sampleCount {
			break
		}

		// 新增：跳过低分域名
		if item.score < minScoreThreshold {
			logger.Debugf("[Prefetcher] 跳过低分域名: %s (score=%.2f)", item.domain, item.score)
			p.recordSkippedLowScore()
			break
		}

		// Verify domain still exists in scoreTable (may have been evicted)
		p.scoreMu.RLock()
		_, exists := p.scoreTable[item.domain]
		p.scoreMu.RUnlock()

		if !exists {
			// Domain was evicted, skip it
			continue
		}

		if p.checkEligibility(item.domain) {
			p.refresher.RefreshDomain(item.domain, dns.TypeA)
			p.refresher.RefreshDomain(item.domain, dns.TypeAAAA)
			p.recordRefresh()
			count++
		}
	}
	logger.Debugf("[Prefetcher] 采样并刷新 %d 个域名 (Table Size: %d, MinScore: %.2f)", count, tableSize, minScoreThreshold)
}

// checkEligibility implements the mathematical eligibility check.
func (p *Prefetcher) checkEligibility(domain string) bool {
	raw, ok := p.cache.GetRaw(domain, dns.TypeA) // Check A record primarily
	if !ok {
		return false // Not in cache -> Not eligible/Unknown
	}

	currentTTL := raw.UpstreamTTL
	if currentTTL < p.cfg.EligibilityTTL {
		return false
	}

	p.scoreMu.RLock()
	entry, exists := p.scoreTable[domain]
	p.scoreMu.RUnlock()

	if !exists {
		return false
	}

	// 新增：跳过 IP 非常稳定的域名
	if entry.StableCycles >= int(p.cfg.StableCycleThreshold) {
		p.recordSkippedStable()
		return false
	}

	currentHash := calculateSimHash(raw.IPs)
	// If LastSimHash is 0 (first time), assume eligible
	if entry.LastSimHash == 0 {
		return true
	}

	distance := hammingDistance(entry.LastSimHash, currentHash)
	if distance > SimHashThreshold {
		p.recordSkippedSimilarHash()
		return false
	}
	return true
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
			StableCycles:    0,
			FirstAccess:     time.Now(), // 记录首次访问时间
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

// evictMsg removes the lowest score items in batch.
func (p *Prefetcher) evictMsg() {
	// Batch eviction: remove bottom 10% of entries to reduce eviction frequency
	evictCount := len(p.scoreTable) / 10
	if evictCount < 1 {
		evictCount = 1
	}

	type item struct {
		domain string
		score  float64
	}

	items := make([]item, 0, len(p.scoreTable))
	for d, e := range p.scoreTable {
		items = append(items, item{domain: d, score: e.RawScore})
	}

	// Sort by score ascending (lowest first)
	sort.Slice(items, func(i, j int) bool {
		return items[i].score < items[j].score
	})

	// Remove the lowest scoring entries
	for i := 0; i < evictCount && i < len(items); i++ {
		delete(p.scoreTable, items[i].domain)
	}
}
