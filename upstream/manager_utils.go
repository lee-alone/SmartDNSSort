package upstream

import (
	"sort"
	"strings"

	"github.com/miekg/dns"
)

// ExtractRecords 从 DNS 响应中提取所有记录、CNAMEs 和最小 TTL（导出版本）
// 返回值：记录列表、CNAME 列表、最小 TTL（秒）
func ExtractRecords(msg *dns.Msg) ([]dns.RR, []string, uint32) {
	return extractRecords(msg)
}

// extractRecords 从 DNS 响应中提取所有记录、CNAMEs 和最小 TTL
// 返回值：记录列表、CNAME 列表、最小 TTL（秒）
func extractRecords(msg *dns.Msg) ([]dns.RR, []string, uint32) {
	var records []dns.RR
	var cnames []string
	var minTTL uint32 = 0 // 0 表示未设置

	for _, answer := range msg.Answer {
		records = append(records, answer) // 直接追加所有类型的记录

		// 单独提取 CNAME 记录
		if cname, ok := answer.(*dns.CNAME); ok {
			cnames = append(cnames, cname.Target)
		}

		// 取最小 TTL
		if minTTL == 0 || answer.Header().Ttl < minTTL {
			minTTL = answer.Header().Ttl
		}
	}

	// 如果没有找到任何记录，使用默认 TTL（60 秒）
	if minTTL == 0 {
		minTTL = 60
	}

	return records, cnames, minTTL
}

// extractIPs 从 DNS 响应中提取 IP 地址、CNAMEs 和最小 TTL
// 返回值：IP 列表、CNAME 列表、最小 TTL（秒）
func extractIPs(msg *dns.Msg) ([]string, []string, uint32) {
	var ips []string
	var cnames []string
	var minTTL uint32 = 0 // 0 表示未设置

	for _, answer := range msg.Answer {
		switch rr := answer.(type) {
		case *dns.A:
			ips = append(ips, rr.A.String())
			// 取最小 TTL
			if minTTL == 0 || rr.Hdr.Ttl < minTTL {
				minTTL = rr.Hdr.Ttl
			}
		case *dns.AAAA:
			ips = append(ips, rr.AAAA.String())
			// 取最小 TTL
			if minTTL == 0 || rr.Hdr.Ttl < minTTL {
				minTTL = rr.Hdr.Ttl
			}
		case *dns.CNAME:
			cnames = append(cnames, rr.Target)
			if minTTL == 0 || rr.Hdr.Ttl < minTTL {
				minTTL = rr.Hdr.Ttl
			}
		}
	}

	// 如果没有找到任何记录，使用默认 TTL（60 秒）
	if minTTL == 0 {
		minTTL = 60
	}

	return ips, cnames, minTTL
}

// extractNegativeTTL 从 NXDOMAIN 响应的 SOA 记录中提取否定缓存 TTL
// 返回值：TTL（秒）
func extractNegativeTTL(msg *dns.Msg) uint32 {
	// 尝试从 Ns (Authority) 部分提取 SOA 记录的 TTL
	for _, ns := range msg.Ns {
		if soa, ok := ns.(*dns.SOA); ok {
			// SOA 记录的 Minimum 字段表示否定缓存的 TTL
			// 同时也要考虑 SOA 记录本身的 TTL
			ttl := soa.Hdr.Ttl
			minttl := min(soa.Minttl, ttl)
			return minttl
		}
	}

	// 如果没有找到 SOA 记录，使用默认的否定缓存 TTL（300 秒 = 5 分钟）
	return 300
}

// getSortedHealthyServers 按健康度和延迟排序服务器
func (u *Manager) getSortedHealthyServers() []*HealthAwareUpstream {
	// 简单实现：优先使用未熔断的服务器，然后按延迟升序排序
	healthy := make([]*HealthAwareUpstream, 0, len(u.servers))
	unhealthy := make([]*HealthAwareUpstream, 0)

	for _, server := range u.servers {
		if !server.ShouldSkipTemporarily() {
			healthy = append(healthy, server)
		} else {
			unhealthy = append(unhealthy, server)
		}
	}

	// 核心改动：对"健康"列表按延迟升序排序
	sort.Slice(healthy, func(i, j int) bool {
		// 按延迟升序排序，延迟越低排越前
		return healthy[i].GetHealth().GetLatency() < healthy[j].GetHealth().GetLatency()
	})

	// 健康的服务器优先，然后是不健康的
	return append(healthy, unhealthy...)
}

// isDNSError 检查是否是 DNS 错误
func isDNSError(err error) bool {
	if err == nil {
		return false
	}
	// 简单的检查：DNS 错误通常包含 "dns" 字样或是特定的 DNS 库错误类型
	return strings.Contains(err.Error(), "dns") || strings.Contains(err.Error(), "rcode")
}

// isDNSNXDomain 检查是否是 NXDOMAIN 错误
func isDNSNXDomain(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "rcode=3") || strings.Contains(err.Error(), "NXDOMAIN")
}
