package dnsserver

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// CustomRule represents a single custom DNS rule
type CustomRule struct {
	Domain string
	Type   uint16
	Value  string // IP address or CNAME target
	TTL    uint32
}

// CustomResponseManager manages custom DNS rules
type CustomResponseManager struct {
	mu        sync.RWMutex
	rules     map[string][]CustomRule // domain -> rules
	filePath  string
	lastLoad  time.Time
}

// NewCustomResponseManager creates a new manager
func NewCustomResponseManager(filePath string) *CustomResponseManager {
	return &CustomResponseManager{
		rules:    make(map[string][]CustomRule),
		filePath: filePath,
	}
}

// Load reads rules from the file
func (m *CustomResponseManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If file doesn't exist, just clear rules and return nil
	if _, err := os.Stat(m.filePath); os.IsNotExist(err) {
		m.rules = make(map[string][]CustomRule)
		return nil
	}

	content, err := os.ReadFile(m.filePath)
	if err != nil {
		return fmt.Errorf("failed to read custom response file: %w", err)
	}

	rules, err := m.parseContent(string(content))
	if err != nil {
		return err
	}

	m.rules = rules
	m.lastLoad = time.Now()
	return nil
}

// parseContent parses rule content and validates it
func (m *CustomResponseManager) parseContent(content string) (map[string][]CustomRule, error) {
	rules := make(map[string][]CustomRule)
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 4 {
			return nil, fmt.Errorf("line %d: invalid format, expected 'domain type value ttl'", lineNum)
		}

		domain := strings.ToLower(strings.TrimRight(parts[0], "."))
		recordType := strings.ToUpper(parts[1])
		value := parts[2]
		ttlStr := parts[3]

		var qtype uint16
		switch recordType {
		case "A":
			qtype = dns.TypeA
			if net.ParseIP(value) == nil || strings.Contains(value, ":") {
				return nil, fmt.Errorf("line %d: invalid A record IP '%s'", lineNum, value)
			}
		case "AAAA":
			qtype = dns.TypeAAAA
			if net.ParseIP(value) == nil {
				return nil, fmt.Errorf("line %d: invalid AAAA record IP '%s'", lineNum, value)
			}
		case "CNAME":
			qtype = dns.TypeCNAME
			// Basic domain validation could go here if needed
		default:
			return nil, fmt.Errorf("line %d: unsupported record type '%s'", lineNum, recordType)
		}

		ttl, err := strconv.ParseUint(ttlStr, 10, 32)
		if err != nil || ttl == 0 {
			return nil, fmt.Errorf("line %d: invalid TTL '%s' (must be a positive integer)", lineNum, ttlStr)
		}

		rule := CustomRule{
			Domain: domain,
			Type:   qtype,
			Value:  value,
			TTL:    uint32(ttl),
		}

		rules[domain] = append(rules[domain], rule)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading content: %w", err)
	}

	return rules, nil
}

// ValidateRules validates the raw content without applying it
func (m *CustomResponseManager) ValidateRules(content string) error {
	_, err := m.parseContent(content)
	return err
}

// Match checks if there is a custom rule for the given domain and type
func (m *CustomResponseManager) Match(domain string, qtype uint16) ([]CustomRule, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	domain = strings.ToLower(strings.TrimRight(domain, "."))
	
	// Direct match
	if rules, ok := m.rules[domain]; ok {
		var matched []CustomRule
		for _, r := range rules {
			// Return exact matches or CNAME (CNAME usually overrides everything, but we'll return it if requested or if it's the only thing)
			// Generally if we query A and have CNAME, we should return CNAME.
			if r.Type == qtype || r.Type == dns.TypeCNAME {
				matched = append(matched, r)
			}
		}
		if len(matched) > 0 {
			return matched, true
		}
	}
	return nil, false
}
