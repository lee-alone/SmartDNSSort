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

// isSameIPList 精确比对 IP 列表
func isSameIPList(old, new []string) bool {
	if len(old) != len(new) {
		return false
	}

	oldSorted := make([]string, len(old))
	newSorted := make([]string, len(new))
	copy(oldSorted, old)
	copy(newSorted, new)
	sort.Strings(oldSorted)
	sort.Strings(newSorted)

	for i := range oldSorted {
		if oldSorted[i] != newSorted[i] {
			return false
		}
	}
	return true
}

// UpdateSimHash 更新域名的 SimHash 和 IP 稳定性标记
func (p *Prefetcher) UpdateSimHash(domain string, ips []string) {
	p.scoreMu.Lock()
	defer p.scoreMu.Unlock()

	if entry, ok := p.scoreTable[domain]; ok {
		newHash := calculateSimHash(ips)

		// 精确比对 IP 列表
		if isSameIPList(entry.LastIPList, ips) {
			entry.StableCycles++ // IP 没变，稳定周期 +1
		} else {
			entry.StableCycles = 0 // IP 变了，重置
		}

		entry.LastSimHash = newHash
		// 深拷贝 IP 列表
		entry.LastIPList = append([]string{}, ips...)
	}
}
