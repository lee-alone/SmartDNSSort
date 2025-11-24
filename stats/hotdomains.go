package stats

import (
	"container/heap"
	"hash/fnv"
	"smartdnssort/config"
	"sync"
	"sync/atomic"
	"time"
)

type HotDomainsTracker struct {
	cfg      *config.StatsConfig
	mu       sync.RWMutex
	buckets  []*TimeBucket
	current  int
	stopChan chan struct{}
}

type TimeBucket struct {
	timestamp time.Time
	shards    []*DomainShard
}

type DomainShard struct {
	mu      sync.RWMutex
	domains map[string]*int64
	size    int
}

func NewHotDomainsTracker(cfg *config.StatsConfig) *HotDomainsTracker {
	// Calculate number of buckets
	numBuckets := (cfg.HotDomainsWindowHours * 60) / cfg.HotDomainsBucketMinutes
	if numBuckets < 1 {
		numBuckets = 1
	}

	tracker := &HotDomainsTracker{
		cfg:      cfg,
		buckets:  make([]*TimeBucket, numBuckets),
		stopChan: make(chan struct{}),
	}

	// Initialize buckets
	for i := 0; i < numBuckets; i++ {
		tracker.buckets[i] = newTimeBucket(cfg.HotDomainsShardCount)
	}
	// Set current bucket timestamp
	tracker.buckets[0].timestamp = time.Now()

	go tracker.startRotation()

	return tracker
}

func newTimeBucket(shardCount int) *TimeBucket {
	bucket := &TimeBucket{
		shards: make([]*DomainShard, shardCount),
	}
	for i := 0; i < shardCount; i++ {
		bucket.shards[i] = &DomainShard{
			domains: make(map[string]*int64),
		}
	}
	return bucket
}

func (t *HotDomainsTracker) Stop() {
	close(t.stopChan)
}

func (t *HotDomainsTracker) RecordQuery(domain string) {
	t.mu.RLock()
	currentBucket := t.buckets[t.current]
	t.mu.RUnlock()

	// Hash domain to select shard
	h := fnv.New32a()
	h.Write([]byte(domain))
	shardIdx := int(h.Sum32()) % len(currentBucket.shards)
	shard := currentBucket.shards[shardIdx]

	// Fast path: check if exists
	shard.mu.RLock()
	counter, exists := shard.domains[domain]
	shard.mu.RUnlock()

	if exists {
		atomic.AddInt64(counter, 1)
		return
	}

	// Slow path: create new entry
	shard.mu.Lock()
	// Double check
	if counter, exists = shard.domains[domain]; exists {
		shard.mu.Unlock()
		atomic.AddInt64(counter, 1)
		return
	}

	if shard.size < t.cfg.HotDomainsMaxPerBucket {
		newCounter := int64(1)
		shard.domains[domain] = &newCounter
		shard.size++
	}
	// Else: bucket full, ignore
	shard.mu.Unlock()
}

func (t *HotDomainsTracker) GetTopDomains(k int) []DomainCount {
	if k <= 0 {
		return nil
	}
	aggregated := make(map[string]int64)

	t.mu.RLock()
	// Iterate over all buckets
	for _, bucket := range t.buckets {
		// Iterate over all shards
		for _, shard := range bucket.shards {
			shard.mu.RLock()
			for domain, counter := range shard.domains {
				aggregated[domain] += atomic.LoadInt64(counter)
			}
			shard.mu.RUnlock()
		}
	}
	t.mu.RUnlock()

	// Use MinHeap to find Top-K
	h := &MinHeap{}
	heap.Init(h)

	for domain, count := range aggregated {
		if h.Len() < k {
			heap.Push(h, DomainCount{Domain: domain, Count: count})
		} else {
			top := (*h)[0]
			isBetter := false
			if count > top.Count {
				isBetter = true
			} else if count == top.Count && domain < top.Domain {
				isBetter = true
			}

			if isBetter {
				heap.Pop(h)
				heap.Push(h, DomainCount{Domain: domain, Count: count})
			}
		}
	}

	// Convert to sorted array (descending)
	result := make([]DomainCount, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		result[i] = heap.Pop(h).(DomainCount)
	}
	return result
}

func (t *HotDomainsTracker) startRotation() {
	ticker := time.NewTicker(time.Duration(t.cfg.HotDomainsBucketMinutes) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.rotateBucket()
		case <-t.stopChan:
			return
		}
	}
}

func (t *HotDomainsTracker) rotateBucket() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Move to next bucket
	t.current = (t.current + 1) % len(t.buckets)

	// Reset the new current bucket
	bucket := t.buckets[t.current]
	bucket.timestamp = time.Now()
	for _, shard := range bucket.shards {
		shard.mu.Lock()
		shard.domains = make(map[string]*int64)
		shard.size = 0
		shard.mu.Unlock()
	}
}

func (t *HotDomainsTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, bucket := range t.buckets {
		for _, shard := range bucket.shards {
			shard.mu.Lock()
			shard.domains = make(map[string]*int64)
			shard.size = 0
			shard.mu.Unlock()
		}
	}
}

// MinHeap implementation
type MinHeap []DomainCount

func (h MinHeap) Len() int { return len(h) }
func (h MinHeap) Less(i, j int) bool {
	if h[i].Count != h[j].Count {
		return h[i].Count < h[j].Count
	}
	return h[i].Domain > h[j].Domain // Higher domain is "smaller/worse" in min-heap
}
func (h MinHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *MinHeap) Push(x interface{}) {
	*h = append(*h, x.(DomainCount))
}

func (h *MinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
