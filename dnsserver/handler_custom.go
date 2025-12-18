package dnsserver

import (
	"net"
	"smartdnssort/logger"
	"strings"

	"github.com/miekg/dns"
)

// handleCustomResponse 处理自定义回复规则
// 返回 true 表示请求已处理
func (s *Server) handleCustomResponse(w dns.ResponseWriter, r *dns.Msg, domain string, qtype uint16) bool {
	if s.customRespManager == nil {
		return false
	}

	rules, matched := s.customRespManager.Match(domain, qtype)
	if !matched {
		return false
	}

	logger.Debugf("[CustomResponse] Matched: %s (type=%s), rules=%d", domain, dns.TypeToString[qtype], len(rules))

	msg := s.msgPool.Get()
	msg.SetReply(r)
	msg.RecursionAvailable = true
	msg.Compress = false

	// Check for CNAME
	var cnameRule *CustomRule
	var aRules []CustomRule

	for _, rule := range rules {
		if rule.Type == dns.TypeCNAME {
			cnameRule = &rule
			break // CNAME priority
		}
		if rule.Type == qtype {
			aRules = append(aRules, rule)
		}
	}

	if cnameRule != nil {
		// CNAME Response
		rr := new(dns.CNAME)
		rr.Hdr = dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: cnameRule.TTL}
		rr.Target = dns.Fqdn(cnameRule.Value)
		msg.Answer = append(msg.Answer, rr)
		w.WriteMsg(msg)
		s.msgPool.Put(msg)
		return true
	} else if len(aRules) > 0 {
		// A/AAAA Response
		for _, rule := range aRules {
			var rr dns.RR
			header := dns.RR_Header{Name: r.Question[0].Name, Rrtype: rule.Type, Class: dns.ClassINET, Ttl: rule.TTL}
			switch rule.Type {
			case dns.TypeA:
				rr = &dns.A{Hdr: header, A: net.ParseIP(rule.Value)}
			case dns.TypeAAAA:
				rr = &dns.AAAA{Hdr: header, AAAA: net.ParseIP(rule.Value)}
			}
			if rr != nil {
				msg.Answer = append(msg.Answer, rr)
			}
		}
		w.WriteMsg(msg)
		s.msgPool.Put(msg)
		return true
	}
	s.msgPool.Put(msg)

	return false
}

// handleLocalRules applies a set of hardcoded rules to block or redirect common bogus queries.
// It returns true if the query was handled, meaning the caller should stop processing.
func (s *Server) handleLocalRules(w dns.ResponseWriter, r *dns.Msg, msg *dns.Msg, domain string, question dns.Question) bool {
	// Rule: Single-label domain (no dots)
	if !strings.Contains(domain, ".") {
		logger.Debugf("[QueryFilter] REFUSED: single-label domain query for '%s'", domain)
		msg.SetRcode(r, dns.RcodeRefused)
		w.WriteMsg(msg)
		return true
	}

	// Rule: localhost
	if domain == "localhost" {
		logger.Debugf("[QueryFilter] STATIC: localhost query for '%s'", domain)
		var ips []string
		switch question.Qtype {
		case dns.TypeA:
			ips = []string{"127.0.0.1"}
		case dns.TypeAAAA:
			ips = []string{"::1"}
		}
		s.buildDNSResponse(msg, domain, ips, question.Qtype, 3600) // 1 hour TTL
		w.WriteMsg(msg)
		return true
	}

	// Rule: Reverse DNS queries
	if strings.HasSuffix(domain, ".in-addr.arpa") || strings.HasSuffix(domain, ".ip6.arpa") {
		logger.Debugf("[QueryFilter] REFUSED: reverse DNS query for '%s'", domain)
		msg.SetRcode(r, dns.RcodeRefused)
		w.WriteMsg(msg)
		return true
	}

	// Rule: Blocklist for specific domains and suffixes
	// Using a map for exact matches is efficient.
	blockedDomains := map[string]int{
		"local":                     dns.RcodeRefused,
		"corp":                      dns.RcodeRefused,
		"home":                      dns.RcodeRefused,
		"lan":                       dns.RcodeRefused,
		"internal":                  dns.RcodeRefused,
		"intranet":                  dns.RcodeRefused,
		"private":                   dns.RcodeRefused,
		"home.arpa":                 dns.RcodeRefused,
		"wpad":                      dns.RcodeNameError, // NXDOMAIN is better for wpad
		"isatap":                    dns.RcodeRefused,
		"teredo.ipv6.microsoft.com": dns.RcodeNameError,
	}

	if rcode, ok := blockedDomains[domain]; ok {
		logger.Debugf("[QueryFilter] Rule match for '%s', responding with %s", domain, dns.RcodeToString[rcode])
		msg.SetRcode(r, rcode)
		w.WriteMsg(msg)
		return true
	}

	return false // Not handled by filter
}
