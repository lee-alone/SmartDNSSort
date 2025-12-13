package adblock

import (
	"testing"
)

func TestSuffixMatcherBasic(t *testing.T) {
	matcher := NewSuffixMatcher()

	// Test AddRule and Match
	matcher.AddRule("example.com")

	// Exact match
	matched, rule := matcher.Match("example.com")
	if !matched {
		t.Errorf("Expected to match 'example.com', got %v", matched)
	}
	if rule != "||example.com^" {
		t.Errorf("Expected rule '||example.com^', got '%s'", rule)
	}

	// Subdomain match
	matched, rule = matcher.Match("sub.example.com")
	if !matched {
		t.Errorf("Expected to match 'sub.example.com', got %v", matched)
	}
	if rule != "||sub.example.com^" {
		t.Errorf("Expected rule '||sub.example.com^', got '%s'", rule)
	}

	// No match
	matched, _ = matcher.Match("example.org")
	if matched {
		t.Errorf("Expected to not match 'example.org', got %v", matched)
	}
}

func TestSuffixMatcherSingleLevel(t *testing.T) {
	matcher := NewSuffixMatcher()
	matcher.AddRule("com")

	// Should match .com domains
	matched, _ := matcher.Match("example.com")
	if !matched {
		t.Errorf("Expected to match 'example.com' with rule 'com'")
	}

	matched, _ = matcher.Match("google.com")
	if !matched {
		t.Errorf("Expected to match 'google.com' with rule 'com'")
	}

	// Should not match different TLD
	matched, _ = matcher.Match("example.org")
	if matched {
		t.Errorf("Expected to not match 'example.org' with rule 'com'")
	}
}

func TestSuffixMatcherMultiLevel(t *testing.T) {
	matcher := NewSuffixMatcher()
	matcher.AddRule("co.uk")

	matched, _ := matcher.Match("example.co.uk")
	if !matched {
		t.Errorf("Expected to match 'example.co.uk' with rule 'co.uk'")
	}

	matched, _ = matcher.Match("test.example.co.uk")
	if !matched {
		t.Errorf("Expected to match 'test.example.co.uk' with rule 'co.uk'")
	}

	matched, _ = matcher.Match("example.com")
	if matched {
		t.Errorf("Expected to not match 'example.com' with rule 'co.uk'")
	}
}

func TestSuffixMatcherCaseInsensitive(t *testing.T) {
	matcher := NewSuffixMatcher()
	matcher.AddRule("Example.Com")

	// Should match regardless of case
	matched, _ := matcher.Match("example.com")
	if !matched {
		t.Errorf("Expected to match 'example.com' with rule 'Example.Com'")
	}

	matched, _ = matcher.Match("EXAMPLE.COM")
	if !matched {
		t.Errorf("Expected to match 'EXAMPLE.COM' with rule 'Example.Com'")
	}

	matched, _ = matcher.Match("Sub.EXAMPLE.com")
	if !matched {
		t.Errorf("Expected to match 'Sub.EXAMPLE.com' with rule 'Example.Com'")
	}
}

func TestSuffixMatcherCount(t *testing.T) {
	matcher := NewSuffixMatcher()

	if matcher.Count() != 0 {
		t.Errorf("Expected 0 rules initially, got %d", matcher.Count())
	}

	matcher.AddRule("example.com")
	if matcher.Count() != 1 {
		t.Errorf("Expected 1 rule after adding 1, got %d", matcher.Count())
	}

	matcher.AddRule("test.com")
	if matcher.Count() != 2 {
		t.Errorf("Expected 2 rules after adding 2, got %d", matcher.Count())
	}

	// Adding duplicate should update the same entry, not add a new one
	matcher.AddRule("example.com")
	if matcher.Count() != 2 {
		t.Errorf("Expected 2 rules after adding duplicate, got %d", matcher.Count())
	}
}

func TestSuffixMatcherMultipleRules(t *testing.T) {
	matcher := NewSuffixMatcher()

	matcher.AddRule("google.com")
	matcher.AddRule("facebook.com")
	matcher.AddRule("twitter.com")

	tests := []struct {
		domain      string
		shouldMatch bool
	}{
		{"google.com", true},
		{"www.google.com", true},
		{"maps.google.com", true},
		{"facebook.com", true},
		{"fb.com", false},
		{"twitter.com", true},
		{"t.co", false},
		{"example.com", false},
	}

	for _, test := range tests {
		matched, _ := matcher.Match(test.domain)
		if matched != test.shouldMatch {
			t.Errorf("Domain '%s': expected %v, got %v", test.domain, test.shouldMatch, matched)
		}
	}
}

func TestSuffixMatcherLongDomain(t *testing.T) {
	matcher := NewSuffixMatcher()
	matcher.AddRule("test-123.example.co.uk")

	matched, _ := matcher.Match("test-123.example.co.uk")
	if !matched {
		t.Errorf("Expected to match 'test-123.example.co.uk'")
	}

	matched, _ = matcher.Match("sub.test-123.example.co.uk")
	if !matched {
		t.Errorf("Expected to match 'sub.test-123.example.co.uk'")
	}
}

func TestSuffixMatcherNumberedDomain(t *testing.T) {
	matcher := NewSuffixMatcher()
	matcher.AddRule("123.456.789")

	matched, _ := matcher.Match("123.456.789")
	if !matched {
		t.Errorf("Expected to match '123.456.789'")
	}

	matched, _ = matcher.Match("sub.123.456.789")
	if !matched {
		t.Errorf("Expected to match 'sub.123.456.789'")
	}
}

func BenchmarkSuffixMatcherAdd(b *testing.B) {
	matcher := NewSuffixMatcher()
	domains := []string{
		"google.com",
		"facebook.com",
		"twitter.com",
		"instagram.com",
		"youtube.com",
		"linkedin.com",
		"pinterest.com",
		"reddit.com",
		"tumblr.com",
		"flickr.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.AddRule(domains[i%len(domains)])
	}
}

func BenchmarkSuffixMatcherMatch(b *testing.B) {
	matcher := NewSuffixMatcher()

	// Add some rules
	for i := 0; i < 1000; i++ {
		matcher.AddRule("example" + string(rune(i)) + ".com")
	}

	testDomains := []string{
		"www.example0.com",
		"www.example100.com",
		"www.example500.com",
		"www.example999.com",
		"www.notexample.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.Match(testDomains[i%len(testDomains)])
	}
}
