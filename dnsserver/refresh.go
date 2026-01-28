package dnsserver

import (
	"context"
	"smartdnssort/logger"
	"time"

	"github.com/miekg/dns"
)

// refreshCacheAsync 异步刷新缓存（用于缓存过期后）
// 重新查询上游 DNS 并排序，更新缓存
func (s *Server) refreshCacheAsync(task RefreshTask) {
	domain := task.Domain
	qtype := task.Qtype

	logger.Debugf("[refreshCacheAsync] 开始异步刷新缓存: %s (type=%s)", domain, dns.TypeToString[qtype])

	// For refreshes, use a slightly longer, fixed timeout as it runs in the background.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Create a new request for the query, since we don't have one from a client
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn(domain), qtype)
	dnssec := s.cfg.Upstream.Dnssec
	if dnssec {
		req.SetEdns0(4096, true)
	}

	// Step 1: Initial query to upstream
	result, err := s.upstream.Query(ctx, req, dnssec)
	if err != nil {
		logger.Warnf("[refreshCacheAsync] 刷新缓存失败 (上游查询): %s, 错误: %v", domain, err)
		return
	}

	var finalIPs []string
	var fullCNAMEs []string
	var finalTTL uint32

	if len(result.IPs) == 0 && len(result.CNAMEs) > 0 {
		// Scenario 1: Got a CNAME, need to resolve it recursively
		lastCNAME := result.CNAMEs[len(result.CNAMEs)-1]
		logger.Debugf("[refreshCacheAsync] 发现CNAME %v, 递归解析 %s", result.CNAMEs, lastCNAME)

		finalResult, resolveErr := s.resolveCNAME(ctx, lastCNAME, qtype, req, dnssec)
		if resolveErr != nil {
			logger.Warnf("[refreshCacheAsync] 刷新缓存失败 (CNAME递归): %s, 错误: %v", lastCNAME, resolveErr)
			return
		}

		finalIPs = finalResult.IPs
		// 合并CNAME链（去重）
		cnameSet := make(map[string]bool)
		for _, cname := range result.CNAMEs {
			cnameSet[cname] = true
			fullCNAMEs = append(fullCNAMEs, cname)
		}
		for _, cname := range finalResult.CNAMEs {
			if !cnameSet[cname] {
				fullCNAMEs = append(fullCNAMEs, cname)
			}
		}
		finalTTL = finalResult.TTL
	} else {
		// Scenario 2: Got IPs directly, or an empty result
		finalIPs = result.IPs
		fullCNAMEs = result.CNAMEs
		finalTTL = result.TTL
	}

	// If we still have no IPs, there's nothing to sort or update.
	if len(finalIPs) == 0 {
		logger.Debugf("[refreshCacheAsync] 刷新缓存对于 %s 返回空IP结果，不更新缓存。", domain)
		return
	}

	logger.Debugf("[refreshCacheAsync] 刷新成功: %s -> %v -> %v (TTL: %d)", domain, fullCNAMEs, finalIPs, finalTTL)

	// 只为原始查询域名创建缓存，不为CNAME链中的其他域名创建缓存
	// 原因：CNAME链中的每个域名可能有不同的IP，不应该都关联到相同的IP列表
	// 这会导致直接查询CNAME时返回错误的IP，造成证书错误
	s.cache.SetRaw(domain, qtype, finalIPs, fullCNAMEs, finalTTL)
	go s.sortIPsAsync(domain, qtype, finalIPs, finalTTL, time.Now())

	// ========== 关键修复：删除为CNAME创建缓存的循环 ==========
	// 修复前的代码会为CNAME链中的每个域名都创建缓存，导致所有CNAME都关联到相同的IP
	// 这是导致"域名和IP不匹配"问题的根本原因
	//
	// 修复后：只为原始查询域名创建缓存
	// 如果用户直接查询CNAME，会触发新的查询，而不是返回错误的缓存IP
}

// RefreshDomain is the public method to trigger a cache refresh for a domain.
// It satisfies the prefetch.Refresher interface.
func (s *Server) RefreshDomain(domain string, qtype uint16) {
	// Run in a goroutine to avoid blocking the caller (e.g., the prefetcher loop)
	task := RefreshTask{Domain: domain, Qtype: qtype}
	s.refreshQueue.Submit(task)
}
