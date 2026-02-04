package dnsserver

import (
	"fmt"
	"net"
	"smartdnssort/cache"
	"strings"

	"github.com/miekg/dns"
)

// buildNXDomainResponse builds an NXDOMAIN response for blocked domains.
// Used in: handler_adblock.go
func buildNXDomainResponse(w dns.ResponseWriter, r *dns.Msg, msgPool *cache.MsgPool, srv *Server, ttl int) {
	msg := msgPool.Get()
	msg.SetReply(r)
	msg.SetRcode(r, dns.RcodeNameError)
	msg.RecursionAvailable = true

	// 添加 SOA 记录到 Authority section（符合 RFC 2308）
	if len(r.Question) > 0 {
		domain := strings.TrimRight(r.Question[0].Name, ".")
		soa := srv.buildSOARecord(domain, uint32(ttl))
		msg.Ns = append(msg.Ns, soa)
	}

	w.WriteMsg(msg)
	msgPool.Put(msg)
}

// buildZeroIPResponse builds a response with a zero IP address for blocked domains.
// Used in: handler_adblock.go
func buildZeroIPResponse(w dns.ResponseWriter, r *dns.Msg, blockedIP string, blockedTTL int, msgPool *cache.MsgPool) {
	msg := msgPool.Get()
	msg.SetReply(r)
	msg.RecursionAvailable = true

	ip := net.ParseIP(blockedIP)
	if ip == nil {
		ip = net.ParseIP("0.0.0.0") // Default to 0.0.0.0
	}

	qtype := r.Question[0].Qtype
	domain := r.Question[0].Name

	if qtype == dns.TypeA && ip.To4() != nil {
		msg.Answer = append(msg.Answer, &dns.A{
			Hdr: dns.RR_Header{
				Name:   domain,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    uint32(blockedTTL),
			},
			A: ip,
		})
	} else if qtype == dns.TypeAAAA && ip.To4() == nil {
		msg.Answer = append(msg.Answer, &dns.AAAA{
			Hdr: dns.RR_Header{
				Name:   domain,
				Rrtype: dns.TypeAAAA,
				Class:  dns.ClassINET,
				Ttl:    uint32(blockedTTL),
			},
			AAAA: ip,
		})
	}

	w.WriteMsg(msg)
	msgPool.Put(msg)
}

// buildRefuseResponse builds a REFUSED response for blocked domains.
// Used in: handler_adblock.go
func buildRefuseResponse(w dns.ResponseWriter, r *dns.Msg, msgPool *cache.MsgPool, srv *Server, ttl int) {
	msg := msgPool.Get()
	msg.SetReply(r)
	msg.SetRcode(r, dns.RcodeRefused)
	msg.RecursionAvailable = true

	// 添加 SOA 记录到 Authority section（符合 RFC 2308）
	if len(r.Question) > 0 {
		domain := strings.TrimRight(r.Question[0].Name, ".")
		soa := srv.buildSOARecord(domain, uint32(ttl))
		msg.Ns = append(msg.Ns, soa)
	}

	w.WriteMsg(msg)
	msgPool.Put(msg)
}

// parseRcodeFromError extracts the DNS response code from an upstream query error.
// It parses error messages in the format "dns query failed: rcode=X" returned by the upstream package.
// Returns dns.RcodeNameError for NXDOMAIN errors, dns.RcodeServerFailure for other failures.
// Used in: handler_query.go
func parseRcodeFromError(err error) int {
	if err == nil {
		return dns.RcodeSuccess
	}

	errMsg := err.Error()

	// Parse error message format: "dns query failed: rcode=X"
	if strings.Contains(errMsg, "rcode=") {
		var rcode int
		_, scanErr := fmt.Sscanf(errMsg, "dns query failed: rcode=%d", &rcode)
		if scanErr == nil {
			return rcode
		}
	}

	// Fallback: check for common error patterns
	if strings.Contains(errMsg, "NXDOMAIN") || strings.Contains(errMsg, "no such host") {
		return dns.RcodeNameError
	}

	// Default to server failure for other errors (timeouts, network errors, etc.)
	return dns.RcodeServerFailure
}

// extractIPsFromRecords 提取 A/AAAA 记录中的 IP 地址
func extractIPsFromRecords(records []dns.RR) []string {
	ips := make([]string, 0)
	for _, r := range records {
		switch rr := r.(type) {
		case *dns.A:
			ips = append(ips, rr.A.String())
		case *dns.AAAA:
			ips = append(ips, rr.AAAA.String())
		}
	}
	return ips
}
