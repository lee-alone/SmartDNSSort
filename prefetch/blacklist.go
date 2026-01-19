package prefetch

import (
	"math"
	"time"

	"smartdnssort/logger"
	"smartdnssort/ping"
)

// ReportPingResultWithDomain handles the granular blacklist update.
func (p *Prefetcher) ReportPingResultWithDomain(domain string, results []ping.Result) {
	if !p.cfg.Enabled {
		return
	}
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
