package dnsserver

import (
	"net"
	"smartdnssort/logger"

	"github.com/miekg/dns"
)

// buildDNSResponse 构造 DNS 响应
func (s *Server) buildDNSResponse(msg *dns.Msg, domain string, ips []string, qtype uint16, ttl uint32) {
	s.buildDNSResponseWithDNSSEC(msg, domain, ips, qtype, ttl, false)
}

// buildDNSResponseWithDNSSEC 构造带 DNSSEC 标记的 DNS 响应
func (s *Server) buildDNSResponseWithDNSSEC(msg *dns.Msg, domain string, ips []string, qtype uint16, ttl uint32, authData bool) {
	fqdn := dns.Fqdn(domain)
	if authData {
		logger.Debugf("[buildDNSResponse] 构造响应: %s (type=%s) 包含 %d 个IP, TTL=%d, DNSSEC验证=已",
			domain, dns.TypeToString[qtype], len(ips), ttl)
		msg.AuthenticatedData = true
	} else {
		logger.Debugf("[buildDNSResponse] 构造响应: %s (type=%s) 包含 %d 个IP, TTL=%d",
			domain, dns.TypeToString[qtype], len(ips), ttl)
	}

	for _, ip := range ips {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}

		switch qtype {
		case dns.TypeA:
			// 返回 IPv4
			if parsedIP.To4() != nil {
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{
						Name:   fqdn,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    ttl,
					},
					A: parsedIP,
				})
			}
		case dns.TypeAAAA:
			// 返回 IPv6
			if parsedIP.To4() == nil && parsedIP.To16() != nil {
				msg.Answer = append(msg.Answer, &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   fqdn,
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    ttl,
					},
					AAAA: parsedIP,
				})
			}
		}
	}
}

// buildDNSResponseWithCNAME 构造包含 CNAME 和 IP 的完整 DNS 响应
// 响应格式：
//
//	www.example.com.  300  IN  CNAME  cdn.example.com.
//	cdn.example.com.  300  IN  A      1.2.3.4
func (s *Server) buildDNSResponseWithCNAME(msg *dns.Msg, domain string, cnames []string, ips []string, qtype uint16, ttl uint32) {
	s.buildDNSResponseWithCNAMEAndDNSSEC(msg, domain, cnames, ips, qtype, ttl, false)
}

// buildDNSResponseWithCNAMEAndDNSSEC 构造包含 CNAME、IP 和 DNSSEC 标记的完整 DNS 响应
func (s *Server) buildDNSResponseWithCNAMEAndDNSSEC(msg *dns.Msg, domain string, cnames []string, ips []string, qtype uint16, ttl uint32, authData bool) {
	if len(cnames) == 0 {
		return
	}

	if authData {
		msg.AuthenticatedData = true
	}

	// We need to chain the CNAMEs.
	// domain -> cnames[0]
	// cnames[0] -> cnames[1] ...
	// cnames[n] -> ips

	currentName := dns.Fqdn(domain)

	for _, target := range cnames {
		targetFqdn := dns.Fqdn(target)
		msg.Answer = append(msg.Answer, &dns.CNAME{
			Hdr: dns.RR_Header{
				Name:   currentName,
				Rrtype: dns.TypeCNAME,
				Class:  dns.ClassINET,
				Ttl:    ttl,
			},
			Target: targetFqdn,
		})
		currentName = targetFqdn
	}

	// The IPs belong to the LAST CNAME target
	// 2. 然后添加目标域名的 A/AAAA 记录
	for _, ip := range ips {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}

		switch qtype {
		case dns.TypeA:
			// 返回 IPv4，记录名称使用 CNAME 目标
			if parsedIP.To4() != nil {
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{
						Name:   currentName, // 使用最后一个 CNAME 目标作为记录名
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    ttl,
					},
					A: parsedIP,
				})
			}
		case dns.TypeAAAA:
			// 返回 IPv6，记录名称使用 CNAME 目标
			if parsedIP.To4() == nil && parsedIP.To16() != nil {
				msg.Answer = append(msg.Answer, &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   currentName, // 使用最后一个 CNAME 目标作为记录名
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    ttl,
					},
					AAAA: parsedIP,
				})
			}
		}
	}
}
