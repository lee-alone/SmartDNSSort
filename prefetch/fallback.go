package prefetch

import (
	"math"
	"sort"
	"time"

	"smartdnssort/logger"
)

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
