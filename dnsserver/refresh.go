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

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.Upstream.TimeoutMs)*time.Millisecond)
	defer cancel()

	// 查询上游 DNS
	result, err := s.upstream.Query(ctx, domain, qtype)
	if err != nil {
		logger.Warnf("[refreshCacheAsync] 刷新缓存失败: %s (type=%s), 错误: %v",
			domain, dns.TypeToString[qtype], err)
		return
	}

	if result == nil || len(result.IPs) == 0 {
		logger.Debugf("[refreshCacheAsync] 刷新缓存返回空结果: %s (type=%s)",
			domain, dns.TypeToString[qtype])
		return
	}

	logger.Debugf("[refreshCacheAsync] 刷新缓存成功，获得 %d 个IP: %v", len(result.IPs), result.IPs)

	// 更新原始缓存
	s.cache.SetRaw(domain, qtype, result.IPs, result.CNAME, result.TTL)

	// 异步排序更新
	go s.sortIPsAsync(domain, qtype, result.IPs, result.TTL, time.Now())
}

// RefreshDomain is the public method to trigger a cache refresh for a domain.
// It satisfies the prefetch.Refresher interface.
func (s *Server) RefreshDomain(domain string, qtype uint16) {
	// Run in a goroutine to avoid blocking the caller (e.g., the prefetcher loop)
	task := RefreshTask{Domain: domain, Qtype: qtype}
	s.refreshQueue.Submit(task)
}
