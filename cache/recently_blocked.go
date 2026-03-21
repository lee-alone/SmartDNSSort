package cache

import (
	"sync"
	"time"
)

// RecentlyBlockedTracker tracks recently blocked domains
type RecentlyBlockedTracker interface {
	Add(domain string)                     // Add a domain to the list
	GetAll() []string                      // Get all domains (max 20)
	GetAllWithTimeRange(days int) []string // Get domains within time range
	Clear()                                // Clear the list
	Len() int                              // Get current count
}

// recentlyBlockedImpl is the thread-safe implementation of RecentlyBlockedTracker
type recentlyBlockedImpl struct {
	mu      sync.RWMutex
	entries []blockedEntry
	maxSize int
}

type blockedEntry struct {
	domain    string
	timestamp time.Time
}

// NewRecentlyBlockedTracker creates a new recently blocked tracker with max 20 entries
func NewRecentlyBlockedTracker() RecentlyBlockedTracker {
	return &recentlyBlockedImpl{
		entries: make([]blockedEntry, 0, 20),
		maxSize: 20,
	}
}

// Add adds a domain to the recently blocked list
// If the list exceeds maxSize, the oldest entry (first in list) is removed
func (r *recentlyBlockedImpl) Add(domain string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Add domain to the end (most recent)
	r.entries = append(r.entries, blockedEntry{
		domain:    domain,
		timestamp: time.Now(),
	})

	// If we exceed max size, remove the oldest entry (first in list)
	if len(r.entries) > r.maxSize {
		r.entries = r.entries[1:]
	}
}

// GetAll returns a copy of all domains in the list
func (r *recentlyBlockedImpl) GetAll() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]string, len(r.entries))
	for i, entry := range r.entries {
		result[i] = entry.domain
	}
	return result
}

// GetAllWithTimeRange returns domains within the specified time range
func (r *recentlyBlockedImpl) GetAllWithTimeRange(days int) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cutoffTime := time.Now().AddDate(0, 0, -days)
	var result []string

	for _, entry := range r.entries {
		if entry.timestamp.After(cutoffTime) {
			result = append(result, entry.domain)
		}
	}

	return result
}

// Clear clears all domains from the list
func (r *recentlyBlockedImpl) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries = make([]blockedEntry, 0, r.maxSize)
}

// Len returns the current number of domains in the list
func (r *recentlyBlockedImpl) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.entries)
}
