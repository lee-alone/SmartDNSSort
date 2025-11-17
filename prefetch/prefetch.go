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
}

// NewPrefetcher creates a new Prefetcher.
func NewPrefetcher(cfg *config.PrefetchConfig, s Stats, c Cache, r Refresher) *Prefetcher {
	return &Prefetcher{
		cfg:       cfg,
		stats:     s,
		cache:     c,
		refresher: r,
		stopChan:  make(chan struct{}),
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

	// Initial sleep can be short to start quickly, then it becomes adaptive
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
	topDomains := p.stats.GetTopDomains(p.cfg.TopDomainsLimit)
	
	if len(topDomains) == 0 {
		log.Println("[Prefetcher] No domains to prefetch. Sleeping for a default interval.")
		return 5 * time.Minute // Default long poll if no domains
	}

	prefetchedCount := 0
	minTimeToNextRefresh := 24 * time.Hour // Initialize with a very long time

	for _, domainStat := range topDomains {
		// For now, we assume we should prefetch for both A and AAAA records if they exist.
		// A more advanced implementation could track query types as well.
		for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
			sortedEntry, exists := p.cache.GetSorted(domainStat.Domain, qtype)
			if !exists {
				continue // This domain (for this type) is not in the optimized cache, so nothing to prefetch.
			}

			expiresIn := time.Until(sortedEntry.Timestamp.Add(time.Duration(sortedEntry.TTL) * time.Second))
			
			// Condition to refresh NOW
			if expiresIn.Seconds() < float64(p.cfg.RefreshBeforeExpireSeconds) {
				log.Printf("[Prefetcher] Prefetching %s (type %s), expires in %.1f seconds.",
					domainStat.Domain, dns.TypeToString[qtype], expiresIn.Seconds())
				p.refresher.RefreshDomain(domainStat.Domain, qtype)
				prefetchedCount++
			} else {
				// This one doesn't need a refresh now, but let's see when it *will* need one.
				// The time until it hits the refresh window is `expiresIn - refresh_before_expire_seconds`.
				timeToNextRefresh := expiresIn - (time.Duration(p.cfg.RefreshBeforeExpireSeconds) * time.Second)
				
				// Ensure timeToNextRefresh is positive and update minTimeToNextRefresh
				if timeToNextRefresh > 0 && timeToNextRefresh < minTimeToNextRefresh {
					minTimeToNextRefresh = timeToNextRefresh
				}
			}
		}
	}
	if prefetchedCount > 0 {
		log.Printf("[Prefetcher] Prefetched %d entries.", prefetchedCount)
	}

	// Add a small buffer to avoid waking up too early due to timing inaccuracies
	// Also, ensure a minimum sleep duration to prevent busy-looping if minTimeToNextRefresh is very small.
	if minTimeToNextRefresh < 1 * time.Second {
		minTimeToNextRefresh = 1 * time.Second // Minimum sleep
	}
	
	log.Printf("[Prefetcher] Next prefetch cycle in %.1f seconds.", minTimeToNextRefresh.Seconds())
	return minTimeToNextRefresh
}
