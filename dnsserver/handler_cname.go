package dnsserver

import (
	"context"
	"fmt"
	"smartdnssort/logger"
	"smartdnssort/upstream"
	"strings"

	"github.com/miekg/dns"
)

// resolveCNAME 递归解析 CNAME，直到找到 IP 地址.
// 它返回最终的 IP 和在解析过程中发现的 *所有* CNAME。
func (s *Server) resolveCNAME(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*upstream.QueryResultWithTTL, error) {
	const maxRedirects = 10
	currentDomain := domain
	var accumulatedCNAMEs []string

	var finalResult *upstream.QueryResultWithTTL

	// 用于CNAME去重
	cnameSet := make(map[string]bool)

	for i := range maxRedirects {
		logger.Debugf("[resolveCNAME] 递归查询 #%d: %s (type=%s)", i+1, currentDomain, dns.TypeToString[qtype])

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		queryDomain := strings.TrimRight(currentDomain, ".")

		// Create a new request for the CNAME
		req := new(dns.Msg)
		req.SetQuestion(dns.Fqdn(queryDomain), qtype)
		if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
			req.SetEdns0(4096, true)
		}

		result, err := s.upstream.Query(ctx, req, dnssec)
		if err != nil {
			return nil, fmt.Errorf("cname resolution failed for %s: %v", queryDomain, err)
		}

		// 累加发现的 CNAME（去重）
		if len(result.CNAMEs) > 0 {
			for _, cname := range result.CNAMEs {
				if !cnameSet[cname] {
					cnameSet[cname] = true
					accumulatedCNAMEs = append(accumulatedCNAMEs, cname)
				}
			}
		}

		// 如果找到了 IP，解析结束
		if len(result.IPs) > 0 {
			logger.Debugf("[resolveCNAME] 成功解析到 IP: %v for domain %s", result.IPs, queryDomain)
			finalResult = result
			break
		}

		// 如果没有 IP 但有 CNAME，继续重定向
		if len(result.CNAMEs) > 0 {
			lastCNAME := result.CNAMEs[len(result.CNAMEs)-1]
			logger.Debugf("[resolveCNAME] 发现下一跳 CNAME: %s -> %s", queryDomain, lastCNAME)
			currentDomain = lastCNAME
			continue
		}

		// 如果既没有 IP 也没有 CNAME，说明解析中断 (NODATA for last CNAME)
		// 在这种情况下，我们仍认为解析是"成功"的，但返回空 IP 列表
		finalResult = result
		break
	}

	if finalResult == nil {
		return nil, fmt.Errorf("cname resolution failed: exceeded max redirects for %s", domain)
	}

	// 确保返回的 CNAME 链是完整的
	// Create a copy to avoid mutating the shared result from singleflight
	newResult := *finalResult
	newResult.CNAMEs = accumulatedCNAMEs
	return &newResult, nil
}
