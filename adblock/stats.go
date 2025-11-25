package adblock

import (
	"sync"
	"sync/atomic"
	"time"
)

// AdBlockStats holds statistics about adblock activity.
type AdBlockStats struct {
	Enabled       bool     `json:"enabled"`
	Engine        string   `json:"engine"`
	TotalRules    int      `json:"total_rules"`
	BlockedToday  int64    `json:"blocked_today"`
	BlockedTotal  int64    `json:"blocked_total"`
	LastUpdate    string   `json:"last_update"`
	SourcesCount  int      `json:"sources_count"`
	FailedSources []string `json:"failed_sources"`
}

// Stats manages adblock statistics.
type Stats struct {
	blockedTotal int64
	blockedToday int64
	lastReset    time.Time
	mu           sync.RWMutex
}

// NewStats creates a new Stats manager.
func NewStats() *Stats {
	return &Stats{
		lastReset: time.Now(),
	}
}

// RecordBlock increments the block counters.
func (s *Stats) RecordBlock(domain, rule string) {
	atomic.AddInt64(&s.blockedTotal, 1)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if now.Day() != s.lastReset.Day() || now.Month() != s.lastReset.Month() || now.Year() != s.lastReset.Year() {
		atomic.StoreInt64(&s.blockedToday, 0)
		s.lastReset = now
	}
	atomic.AddInt64(&s.blockedToday, 1)
}

// GetStats returns the current adblock statistics.
func (s *Stats) GetStats(enabled bool, engine string, totalRules int, sourcesCount int, failedSources []string, lastUpdate time.Time) AdBlockStats {
	return AdBlockStats{
		Enabled:       enabled,
		Engine:        engine,
		TotalRules:    totalRules,
		BlockedToday:  atomic.LoadInt64(&s.blockedToday),
		BlockedTotal:  atomic.LoadInt64(&s.blockedTotal),
		LastUpdate:    lastUpdate.Format(time.RFC3339),
		SourcesCount:  sourcesCount,
		FailedSources: failedSources,
	}
}