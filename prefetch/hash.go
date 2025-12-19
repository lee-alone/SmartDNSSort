package prefetch

import (
	"hash/fnv"
	"math/bits"
	"sort"
	"strings"
)

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

// hammingDistance calculates the Hamming distance between two 64-bit integers.
func hammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// UpdateSimHash updates the SimHash for a domain.
func (p *Prefetcher) UpdateSimHash(domain string, ips []string) {
	p.scoreMu.Lock()
	defer p.scoreMu.Unlock()

	if entry, ok := p.scoreTable[domain]; ok {
		entry.LastSimHash = calculateSimHash(ips)
	}
}
