package dnsserver

import (
	"testing"

	"github.com/miekg/dns"
)

func TestCustomResponseManager_ParseContent(t *testing.T) {
	manager := NewCustomResponseManager("dummy.txt")

	validContent := `
# This is a comment
example.com A 1.2.3.4 300
test.org AAAA ::1 60
cname.net CNAME target.com 120
`
	rules, err := manager.parseContent(validContent)
	if err != nil {
		t.Fatalf("Failed to parse valid content: %v", err)
	}

	if len(rules) != 3 {
		t.Errorf("Expected 3 rules, got %d", len(rules))
	}

	// Check A record
	if r, ok := rules["example.com"]; !ok || r[0].Type != dns.TypeA || r[0].Value != "1.2.3.4" || r[0].TTL != 300 {
		t.Errorf("Invalid parsed rule for example.com: %+v", r)
	}

	// Check AAAA record
	if r, ok := rules["test.org"]; !ok || r[0].Type != dns.TypeAAAA || r[0].Value != "::1" || r[0].TTL != 60 {
		t.Errorf("Invalid parsed rule for test.org: %+v", r)
	}

	// Check CNAME record
	if r, ok := rules["cname.net"]; !ok || r[0].Type != dns.TypeCNAME || r[0].Value != "target.com" || r[0].TTL != 120 {
		t.Errorf("Invalid parsed rule for cname.net: %+v", r)
	}
}

func TestCustomResponseManager_ParseInvalidContent(t *testing.T) {
	manager := NewCustomResponseManager("dummy.txt")

	invalidCases := []string{
		"example.com A 1.2.3.4",       // Missing TTL
		"example.com INVALID 1.2.3.4", // Invalid Type
		"example.com A 999.9.9.9 300", // Invalid IP
		"example.com A 1.2.3.4 abc",   // Invalid TTL
	}

	for _, content := range invalidCases {
		_, err := manager.parseContent(content)
		if err == nil {
			t.Errorf("Expected error for invalid content: '%s', but got none", content)
		}
	}
}

func TestCustomResponseManager_Match(t *testing.T) {
	manager := NewCustomResponseManager("dummy.txt")
	content := `
example.com A 1.2.3.4 300
root.com A 10.0.0.1 60
root.com AAAA ::1 60
`
	rules, _ := manager.parseContent(content)
	manager.rules = rules

	// Test Exact Match
	if matches, ok := manager.Match("example.com", dns.TypeA); !ok || len(matches) != 1 {
		t.Errorf("Expected match for example.com A")
	}

	// Test Case Insensitivity
	if matches, ok := manager.Match("EXAMPLE.COM", dns.TypeA); !ok || len(matches) != 1 {
		t.Errorf("Expected match for EXAMPLE.COM A")
	}

	// Test Subdomain (Exact match required logic in current impl, so subdomain shouldn't match unless we changed logic)
	// The requirement was "Custom Domain Responses", usually implies exact match or maybe wildcard if implemented.
	// Based on implementation `rules[domain]`, it is exact match.
	if _, ok := manager.Match("sub.example.com", dns.TypeA); ok {
		t.Errorf("Did not expect match for sub.example.com (Exact match only)")
	}

	// Test Type Mismatch
	if matches, ok := manager.Match("example.com", dns.TypeAAAA); ok {
		// Wait, the implementation logic in server.go handles filtering by type?
		// Let's check Match implementation:
		// if rules, ok := m.rules[domain]; ok { ... if r.Type == qtype ... matched = append }
		// So if we query AAAA but only have A, Match returns false/empty?
		// Actually Match returns `matched` slice.
		// `if len(matched) > 0 { return matched, true }`
		t.Errorf("Did not expect match for example.com AAAA, got %d matches", len(matches))
	}
}
