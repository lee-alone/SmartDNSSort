package cache

import (
	"sync"
)

// RecentlyBlockedTracker tracks recently blocked domains
type RecentlyBlockedTracker interface {
	Add(domain string) // Add a domain to the list
	GetAll() []string  // Get all domains (max 20)
	Clear()            // Clear the list
	Len() int          // Get current count
}

// recentlyBlockedImpl is the thread-safe implementation of RecentlyBlockedTracker
type recentlyBlockedImpl struct {
	mu      sync.RWMutex
	domains []string
	maxSize int
}

// NewRecentlyBlockedTracker creates a new recently blocked tracker with max 20 entries
func NewRecentlyBlockedTracker() RecentlyBlockedTracker {
	return &recentlyBlockedImpl{
		domains: make([]string, 0, 20),
		maxSize: 20,
	}
}

// Add adds a domain to the recently blocked list
// If the list exceeds maxSize, the oldest entry (first in list) is removed
func (r *recentlyBlockedImpl) Add(domain string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Add domain to the end (most recent)
	r.domains = append(r.domains, domain)

	// If we exceed max size, remove the oldest entry (first in list)
	if len(r.domains) > r.maxSize {
		r.domains = r.domains[1:]
	}
}

// GetAll returns a copy of all domains in the list
func (r *recentlyBlockedImpl) GetAll() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]string, len(r.domains))
	copy(result, r.domains)
	return result
}

// Clear clears all domains from the list
func (r *recentlyBlockedImpl) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.domains = make([]string, 0, r.maxSize)
}

// Len returns the current number of domains in the list
func (r *recentlyBlockedImpl) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.domains)
}
