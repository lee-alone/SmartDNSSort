package dnsserver

import (
	"context"
	"smartdnssort/logger"
	"strings"
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

	// Step 1: Initial query to upstream
	result, err := s.upstream.Query(ctx, domain, qtype)
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

		finalResult, resolveErr := s.resolveCNAME(ctx, lastCNAME, qtype)
		if resolveErr != nil {
			logger.Warnf("[refreshCacheAsync] 刷新缓存失败 (CNAME递归): %s, 错误: %v", lastCNAME, resolveErr)
			return
		}

		finalIPs = finalResult.IPs
		fullCNAMEs = append(result.CNAMEs, finalResult.CNAMEs...)
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

	// [Fix] Propagate the update across the entire CNAME chain
	s.cache.SetRaw(domain, qtype, finalIPs, fullCNAMEs, finalTTL)
	go s.sortIPsAsync(domain, qtype, finalIPs, finalTTL, time.Now())

	for i, cname := range fullCNAMEs {
		cnameDomain := strings.TrimRight(cname, ".")
		var subCNAMEs []string
		if i < len(fullCNAMEs)-1 {
			subCNAMEs = fullCNAMEs[i+1:]
		}
		logger.Debugf("[refreshCacheAsync] 正在为CNAME链中的 %s 更新缓存", cnameDomain)
		s.cache.SetRaw(cnameDomain, qtype, finalIPs, subCNAMEs, finalTTL)
		go s.sortIPsAsync(cnameDomain, qtype, finalIPs, finalTTL, time.Now())
	}
}

// RefreshDomain is the public method to trigger a cache refresh for a domain.
// It satisfies the prefetch.Refresher interface.
func (s *Server) RefreshDomain(domain string, qtype uint16) {
	// Run in a goroutine to avoid blocking the caller (e.g., the prefetcher loop)
	task := RefreshTask{Domain: domain, Qtype: qtype}
	s.refreshQueue.Submit(task)
}
