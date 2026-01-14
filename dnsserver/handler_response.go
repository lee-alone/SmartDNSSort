package dnsserver

import (
	"net"
	"smartdnssort/logger"
	"time"

	"github.com/miekg/dns"
)

// buildSOARecord 构造 SOA 记录用于负响应（NXDOMAIN/NODATA）
// 根据 RFC 2308，负响应应在 Authority section 包含 SOA 记录
// SOA 记录的 MINIMUM 字段指示客户端应缓存负响应的时间
func (s *Server) buildSOARecord(domain string, ttl uint32) *dns.SOA {
	// 使用本地权威服务器名称
	// 这些值可以后续移到配置文件中
	mname := "ns.smartdnssort.local."
	rname := "admin.smartdnssort.local."

	return &dns.SOA{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(domain),
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    ttl,
		},
		Ns:      mname,
		Mbox:    rname,
		Serial:  uint32(time.Now().Unix()),
		Refresh: 3600,
		Retry:   600,
		Expire:  86400,
		Minttl:  ttl, // 这个字段指示负缓存的 TTL
	}
}

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

	// 进行IP去重
	ipSet := make(map[string]bool)
	for _, ip := range ips {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}

		// 对IP进行去重
		ipStr := parsedIP.String()
		if ipSet[ipStr] {
			continue // 跳过重复的IP
		}
		ipSet[ipStr] = true

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

	// 第一步：添加 CNAME 链（去重）
	cnameSet := make(map[string]bool)
	for _, target := range cnames {
		targetFqdn := dns.Fqdn(target)

		// 检查是否已经添加过这个CNAME
		cnamePair := currentName + "->" + targetFqdn
		if cnameSet[cnamePair] {
			continue // 跳过重复的CNAME
		}
		cnameSet[cnamePair] = true

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
	// 2. 然后添加目标域名的 A/AAAA 记录（进行IP去重）
	ipSet := make(map[string]bool) // 用于去重IP
	for _, ip := range ips {
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			continue
		}

		// 对IP进行去重
		ipStr := parsedIP.String()
		if ipSet[ipStr] {
			continue // 跳过重复的IP
		}
		ipSet[ipStr] = true

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

// buildGenericResponse 构造通用记录的 DNS 响应
// 参数说明：
//   - msg: DNS 响应消息（已设置 reply）
//   - cnames: CNAME 链列表
//   - records: 所有通用记录列表
//   - qtype: 查询的记录类型
//   - ttl: 响应 TTL
//   - authData: DNSSEC 验证标记
func (s *Server) buildGenericResponse(msg *dns.Msg, cnames []string, records []dns.RR, qtype uint16, ttl uint32, authData bool) {
	logger.Debugf("[buildGenericResponse] 构造通用响应: %d 条 CNAME, %d 条记录, 类型=%s, TTL=%d",
		len(cnames), len(records), dns.TypeToString[qtype], ttl)

	if authData {
		msg.AuthenticatedData = true
	}

	fqdn := dns.Fqdn(msg.Question[0].Name) // 原始查询域名

	// 第一步：添加 CNAME 链（去重）
	if len(cnames) > 0 {
		currentName := fqdn
		cnameSet := make(map[string]bool)
		for _, target := range cnames {
			targetFqdn := dns.Fqdn(target)

			// 检查是否已经添加过这个CNAME
			cnamePair := currentName + "->" + targetFqdn
			if cnameSet[cnamePair] {
				continue // 跳过重复的CNAME
			}
			cnameSet[cnamePair] = true

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
	}

	// 第二步：添加目标记录（筛选匹配查询类型的记录，并进行IP去重）
	ipSet := make(map[string]bool) // 用于去重IP
	for _, rr := range records {
		if rr.Header().Rrtype == qtype {
			// 对A和AAAA记录进行IP去重
			shouldAdd := true
			switch rec := rr.(type) {
			case *dns.A:
				ipStr := rec.A.String()
				if ipSet[ipStr] {
					shouldAdd = false
				} else {
					ipSet[ipStr] = true
				}
			case *dns.AAAA:
				ipStr := rec.AAAA.String()
				if ipSet[ipStr] {
					shouldAdd = false
				} else {
					ipSet[ipStr] = true
				}
			}

			if shouldAdd {
				// 创建记录的副本并更新 TTL
				rrCopy := dns.Copy(rr)
				rrCopy.Header().Ttl = ttl
				msg.Answer = append(msg.Answer, rrCopy)
			}
		}
	}

	// 第三步：处理空响应（NODATA）
	if len(msg.Answer) == 0 || (len(cnames) > 0 && len(msg.Answer) == len(cnames)) {
		msg.SetRcode(msg, dns.RcodeSuccess) // 成功但无记录
		logger.Debugf("[buildGenericResponse] NODATA 响应: %s (type=%s)",
			msg.Question[0].Name, dns.TypeToString[qtype])
	}
}
