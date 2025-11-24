package prefetch

import (
	"log"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/stats"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Refresher defines the interface for an object that can refresh a domain's cache.
type Refresher interface {
	RefreshDomain(domain string, qtype uint16)
}

// Cache defines the interface for the cache that the prefetcher needs to interact with.
type Cache interface {
	GetSorted(domain string, qtype uint16) (*cache.SortedCacheEntry, bool)
}

// Stats defines the interface for the stats collector that the prefetcher needs to interact with.
type Stats interface {
	GetTopDomains(limit int) []stats.DomainCount
}

// Prefetcher is responsible for prefetching popular domains before their cache expires.
type Prefetcher struct {
	cfg       *config.PrefetchConfig
	stats     Stats // Use the interface type
	cache     Cache // Use the interface type
	refresher Refresher
	stopChan  chan struct{}
	wg        sync.WaitGroup

	// Internal state for quick lookups
	topDomainsMu sync.RWMutex
	topDomains   map[string]bool
}

// NewPrefetcher creates a new Prefetcher.
func NewPrefetcher(cfg *config.PrefetchConfig, s Stats, c Cache, r Refresher) *Prefetcher {
	return &Prefetcher{
		cfg:        cfg,
		stats:      s,
		cache:      c,
		refresher:  r,
		stopChan:   make(chan struct{}),
		topDomains: make(map[string]bool),
	}
}

// Start begins the prefetching loop.
func (p *Prefetcher) Start() {
	if !p.cfg.Enabled {
		log.Println("[Prefetcher] Prefetcher is disabled.")
		return
	}
	p.wg.Add(1)
	go p.prefetchLoop()
	log.Println("[Prefetcher] Prefetcher started.")
}

// Stop gracefully stops the prefetcher.
func (p *Prefetcher) Stop() {
	if !p.cfg.Enabled {
		return
	}
	close(p.stopChan)
	p.wg.Wait()
	log.Println("[Prefetcher] Prefetcher stopped.")
}

func (p *Prefetcher) prefetchLoop() {
	defer p.wg.Done()

	nextSleepDuration := 5 * time.Second

	for {
		select {
		case <-time.After(nextSleepDuration):
			nextSleepDuration = p.runPrefetchAndGetNextInterval()
		case <-p.stopChan:
			return
		}
	}
}

// runPrefetchAndGetNextInterval runs a prefetch cycle and returns the duration until the next cycle should run.
func (p *Prefetcher) runPrefetchAndGetNextInterval() time.Duration {
	log.Println("[Prefetcher] Running prefetch cycle.")
	topDomainsList := p.stats.GetTopDomains(p.cfg.TopDomainsLimit)

	newTopDomains := make(map[string]bool, len(topDomainsList))
	for _, d := range topDomainsList {
		newTopDomains[d.Domain] = true
	}
	p.topDomainsMu.Lock()
	p.topDomains = newTopDomains
	p.topDomainsMu.Unlock()

	if len(topDomainsList) == 0 {
		log.Println("[Prefetcher] No domains to prefetch. Sleeping for a default interval.")
		return 5 * time.Minute
	}

	prefetchedCount := 0
	minTimeToNextRefresh := 24 * time.Hour

	for _, domainStat := range topDomainsList {
		for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
			sortedEntry, exists := p.cache.GetSorted(domainStat.Domain, qtype)
			if !exists {
				continue
			}

			expiresIn := time.Until(sortedEntry.Timestamp.Add(time.Duration(sortedEntry.TTL) * time.Second))

			// 移除 min_prefetch_interval 检查，直接依靠过期时间判断
			// min_ttl_seconds 配置已经提供了足够的保护，防止过度预取
			threshold := float64(p.cfg.RefreshBeforeExpireSeconds)

			if expiresIn.Seconds() < threshold {
				log.Printf("[Prefetcher] Prefetching %s (type %s), expires in %.1f seconds.",
					domainStat.Domain, dns.TypeToString[qtype], expiresIn.Seconds())
				p.refresher.RefreshDomain(domainStat.Domain, qtype)
				prefetchedCount++
			} else {
				timeToNextRefresh := expiresIn - time.Duration(threshold*float64(time.Second))
				if timeToNextRefresh > 0 && timeToNextRefresh < minTimeToNextRefresh {
					minTimeToNextRefresh = timeToNextRefresh
				}
			}
		}
	}
	if prefetchedCount > 0 {
		log.Printf("[Prefetcher] Prefetched %d entries.", prefetchedCount)
	}

	if minTimeToNextRefresh < 1*time.Second {
		minTimeToNextRefresh = 1 * time.Second
	}

	log.Printf("[Prefetcher] Next prefetch cycle in %.1f seconds.", minTimeToNextRefresh.Seconds())
	return minTimeToNextRefresh
}

// IsTopDomain checks if a domain is currently considered a top domain.
func (p *Prefetcher) IsTopDomain(domain string) bool {
	p.topDomainsMu.RLock()
	defer p.topDomainsMu.RUnlock()
	_, exists := p.topDomains[domain]
	return exists
}
