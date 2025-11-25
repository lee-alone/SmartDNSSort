package adblock

import (
	"strings"
	"sync"
)

// SimpleFilter is a basic adblock filter engine.
type SimpleFilter struct {
	exactMatcher  *ExactMatcher
	suffixMatcher *SuffixMatcher
	hostsMatcher  *HostsMatcher
	mu            sync.RWMutex
}

// NewSimpleFilter creates a new SimpleFilter.
func NewSimpleFilter() *SimpleFilter {
	return &SimpleFilter{
		exactMatcher:  NewExactMatcher(),
		suffixMatcher: NewSuffixMatcher(),
		hostsMatcher:  NewHostsMatcher(),
	}
}

// CheckHost implements the FilterEngine interface.
func (f *SimpleFilter) CheckHost(domain string) (bool, string) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	// 1. 检查精确匹配 (黑名单)
	if matched, rule := f.exactMatcher.Match(domain); matched {
		return true, "Blacklist: " + rule
	}

	// 2. 检查 Hosts 匹配
	if matched, rule := f.hostsMatcher.Match(domain); matched {
		return true, "Hosts: " + rule
	}

	// 3. 检查后缀匹配 (AdBlock ||example.com^)
	if matched, rule := f.suffixMatcher.Match(domain); matched {
		return true, "AdBlock: " + rule
	}

	return false, ""
}

// LoadRules implements the FilterEngine interface.
// It parses rules and adds them to the appropriate matcher.
func (f *SimpleFilter) LoadRules(rules []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Reset matchers
	f.exactMatcher = NewExactMatcher()
	f.suffixMatcher = NewSuffixMatcher()
	f.hostsMatcher = NewHostsMatcher()

	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" || strings.HasPrefix(rule, "!") || strings.HasPrefix(rule, "#") {
			continue // Skip empty lines and comments
		}

		// AdBlock Plus style rules
		if strings.HasPrefix(rule, "||") && strings.HasSuffix(rule, "^") {
			domain := strings.TrimPrefix(rule, "||")
			domain = strings.TrimSuffix(domain, "^")
			f.suffixMatcher.AddRule(domain)
			continue
		}

		// Hosts file style rules
		if strings.Contains(rule, " ") || strings.Contains(rule, "\t") {
			parts := strings.Fields(rule)
			if len(parts) >= 2 {
				// Typically "127.0.0.1 domain.com" or "0.0.0.0 domain.com"
				// We just care about the domain part
				f.hostsMatcher.AddRule(rule)
				continue
			}
		}

		// Plain domain (exact match)
		f.exactMatcher.AddRule(rule)
	}
	return nil
}

// Count implements the FilterEngine interface.
func (f *SimpleFilter) Count() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.exactMatcher.Count() + f.suffixMatcher.Count() + f.hostsMatcher.Count()
}