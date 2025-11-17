package stats

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetTopDomains(t *testing.T) {
	s := NewStats()

	// Record some domain queries
	s.RecordDomainQuery("google.com")
	s.RecordDomainQuery("google.com")
	s.RecordDomainQuery("google.com")
	s.RecordDomainQuery("facebook.com")
	s.RecordDomainQuery("facebook.com")
	s.RecordDomainQuery("github.com")
	s.RecordDomainQuery("netflix.com")
	s.RecordDomainQuery("netflix.com")
	s.RecordDomainQuery("amazon.com")

	// Test with limit > number of domains
	top5 := s.GetTopDomains(5)
	assert.Len(t, top5, 5, "Expected 5 domains")
	assert.Equal(t, "google.com", top5[0].Domain)
	assert.Equal(t, int64(3), top5[0].Count)
	assert.Equal(t, "facebook.com", top5[1].Domain)
	assert.Equal(t, int64(2), top5[1].Count)
	assert.Equal(t, "netflix.com", top5[2].Domain)
	assert.Equal(t, int64(2), top5[2].Count)

	// Test with limit < number of domains
	top2 := s.GetTopDomains(2)
	assert.Len(t, top2, 2, "Expected 2 domains")
	assert.Equal(t, "google.com", top2[0].Domain)
	assert.Equal(t, int64(3), top2[0].Count)

	// Test with limit = 0
	top0 := s.GetTopDomains(0)
	assert.Len(t, top0, 0, "Expected 0 domains")

	// Test with empty stats
	s.Reset()
	topEmpty := s.GetTopDomains(5)
	assert.Len(t, topEmpty, 0, "Expected 0 domains from empty stats")
}
