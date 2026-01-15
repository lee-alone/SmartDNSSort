package dnsserver

import (
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/stats"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// handleErrorCacheHit 处理错误缓存命中 (NXDOMAIN/NODATA/SERVFAIL等)
// 返回 true 表示请求已处理
func (s *Server) handleErrorCacheHit(w dns.ResponseWriter, r *dns.Msg, domain string, qtype uint16, stats *stats.Stats) bool {
	if entry, ok := s.cache.GetError(domain, qtype); ok {
		stats.IncCacheHits()
		logger.Debugf("[handleQuery] 错误缓存命中: %s (type=%s, rcode=%d)",
			domain, dns.TypeToString[qtype], entry.Rcode)

		msg := s.msgPool.Get()
		msg.SetReply(r)
		msg.RecursionAvailable = true
		msg.Compress = false
		msg.SetRcode(r, entry.Rcode)

		// 计算剩余TTL（确保至少为1秒）
		elapsed := int(time.Since(entry.CachedAt).Seconds() + 0.5) // 四舍五入
		remainingTTL := uint32(max(1, entry.TTL-elapsed))

		// 根据 RFC 2308，为负响应添加 SOA 记录到 Authority section
		// 这样客户端就知道应该缓存负响应多久
		soa := s.buildSOARecord(domain, remainingTTL)
		msg.Ns = append(msg.Ns, soa)

		logger.Debugf("[handleQuery] 返回负响应，TTL=%d秒: %s (type=%s)",
			remainingTTL, domain, dns.TypeToString[qtype])

		w.WriteMsg(msg)
		s.msgPool.Put(msg)
		return true
	}
	return false
}

// handleSortedCacheHit 处理排序完成后的缓存命中
// 返回 true 表示请求已处理
func (s *Server) handleSortedCacheHit(w dns.ResponseWriter, r *dns.Msg, domain string, qtype uint16, cfg *config.Config, stats *stats.Stats) bool {
	sorted, ok := s.cache.GetSorted(domain, qtype)
	if !ok {
		return false
	}

	s.cache.RecordAccess(domain, qtype)                   // 记录访问
	s.prefetcher.RecordAccess(domain, uint32(sorted.TTL)) // Prefetcher Math Model Update
	stats.IncCacheHits()
	stats.RecordDomainQuery(domain) // ✅ 统计有效域名查询

	// 1. 判断数据“新鲜度” (RTT 层面)
	elapsed := time.Since(sorted.Timestamp)
	rttStale := elapsed.Seconds() >= float64(cfg.Ping.RttCacheTtlSeconds)

	// 2. 计算上游 DNS 层面是否过期 (DNS 层面)
	raw, hasRaw := s.cache.GetRaw(domain, qtype)
	dnsExpired := !hasRaw || raw.IsExpired()

	// 3. 计算用户视角下的 TTL
	userTTL := s.calculateUserTTL(sorted.TTL, elapsed, cfg, rttStale)

	// 4. 异步刷新策略：精准决策
	if rttStale {
		if dnsExpired {
			// DNS 也过期了，走最重的 RefreshTask (重新请求上游)
			logger.Debugf("[handleQuery] 排序缓存命中 (Stale: DNS+RTT), 触发异步全量刷新: %s", domain)
			s.RefreshDomain(domain, qtype)
		} else {
			// DNS 没过期，只是需要重新测速，走轻量的 sortIPsAsync
			logger.Debugf("[handleQuery] 排序缓存命中 (Stale: RTT only), 触发异步探测刷新: %s", domain)
			go s.sortIPsAsync(domain, qtype, raw.IPs, raw.UpstreamTTL, raw.AcquisitionTime)
		}
	}

	// 5. 构造响应
	msg := s.msgPool.Get()
	msg.SetReply(r)
	msg.RecursionAvailable = true
	msg.Compress = false

	var cnames []string
	if hasRaw && len(raw.CNAMEs) > 0 {
		cnames = raw.CNAMEs
	}

	if len(cnames) > 0 {
		s.buildDNSResponseWithCNAME(msg, domain, cnames, sorted.IPs, qtype, userTTL)
	} else {
		s.buildDNSResponse(msg, domain, sorted.IPs, qtype, userTTL)
	}

	w.WriteMsg(msg)
	s.msgPool.Put(msg)
	return true
}

// calculateUserTTL 抽取通用的用户 TTL 计算逻辑
func (s *Server) calculateUserTTL(originalTTL int, elapsed time.Duration, cfg *config.Config, isStale bool) uint32 {
	elapsedSec := int(elapsed.Seconds())
	remaining := max(0, originalTTL-elapsedSec)

	var userTTL uint32
	if remaining > 0 {
		if cfg.Cache.UserReturnTTL > 0 {
			cycleOffset := elapsedSec % cfg.Cache.UserReturnTTL
			cappedTTL := cfg.Cache.UserReturnTTL - cycleOffset
			userTTL = uint32(min(remaining, cappedTTL))
		} else {
			userTTL = uint32(remaining)
		}
	} else {
		userTTL = uint32(cfg.Cache.FastResponseTTL)
	}

	// 如果处于软过期状态，强行压缩 TTL 引导客户端快速重连
	if isStale {
		userTTL = min(userTTL, uint32(cfg.Cache.FastResponseTTL))
	}

	return max(userTTL, 1)
}

// handleRawCacheHit 处理原始缓存（上游DNS响应缓存）命中
// 返回 true 表示请求已处理
func (s *Server) handleRawCacheHit(w dns.ResponseWriter, r *dns.Msg, domain string, qtype uint16, cfg *config.Config, stats *stats.Stats) bool {
	raw, ok := s.cache.GetRaw(domain, qtype)
	if !ok {
		return false
	}

	s.cache.RecordAccess(domain, qtype)                // 记录访问
	s.prefetcher.RecordAccess(domain, raw.UpstreamTTL) // Prefetcher Math Model Update
	stats.IncCacheHits()
	stats.RecordDomainQuery(domain) // ✅ 统计有效域名查询
	logger.Debugf("[handleQuery] 原始缓存命中: %s (type=%s) -> %v, CNAMEs=%v (过期:%v)",
		domain, dns.TypeToString[qtype], raw.IPs, raw.CNAMEs, raw.IsExpired())

	// 3. 计算用户视角下的 TTL
	elapsed := time.Since(raw.AcquisitionTime)
	userTTL := s.calculateUserTTL(int(raw.EffectiveTTL), elapsed, cfg, raw.IsExpired())

	// 使用历史数据进行兜底排序 (Fallback Rank)
	// [Fix] 如果存在 CNAME，使用最终目标域名获取排序权重，因为 stats 是记在 target 上的
	rankDomain := domain
	if len(raw.CNAMEs) > 0 {
		rankDomain = strings.TrimRight(raw.CNAMEs[len(raw.CNAMEs)-1], ".")
	}
	fallbackIPs := s.prefetcher.GetFallbackRank(rankDomain, raw.IPs)

	msg := s.msgPool.Get()
	msg.RecursionAvailable = true
	msg.SetReply(r)
	msg.Compress = false
	// 仅当启用 DNSSEC 时才转发验证标记
	authData := raw.AuthenticatedData && cfg.Upstream.Dnssec
	if len(raw.CNAMEs) > 0 {
		s.buildDNSResponseWithCNAMEAndDNSSEC(msg, domain, raw.CNAMEs, fallbackIPs, qtype, userTTL, authData)
	} else {
		s.buildDNSResponseWithDNSSEC(msg, domain, fallbackIPs, qtype, userTTL, authData)
	}
	w.WriteMsg(msg)
	s.msgPool.Put(msg)

	if raw.IsExpired() {
		logger.Debugf("[handleQuery] 原始缓存已过期,触发异步刷新: %s (type=%s)",
			domain, dns.TypeToString[qtype])
		task := RefreshTask{Domain: domain, Qtype: qtype}
		s.refreshQueue.Submit(task)
	} else {
		go s.sortIPsAsync(domain, qtype, raw.IPs, raw.UpstreamTTL, raw.AcquisitionTime)
	}
	return true
}

// handleRawCacheHitGeneric 处理通用记录的原始缓存命中
// 返回 true 表示请求已处理
func (s *Server) handleRawCacheHitGeneric(w dns.ResponseWriter, r *dns.Msg, domain string, qtype uint16, cfg *config.Config, stats *stats.Stats) bool {
	// 如果是 A/AAAA 查询，不在这里处理
	if qtype == dns.TypeA || qtype == dns.TypeAAAA {
		return false
	}

	raw, ok := s.cache.GetRaw(domain, qtype)
	if !ok {
		return false
	}

	s.cache.RecordAccess(domain, qtype)
	s.prefetcher.RecordAccess(domain, raw.UpstreamTTL)
	stats.IncCacheHits()
	stats.RecordDomainQuery(domain)
	logger.Debugf("[handleRawCacheHitGeneric] 通用记录缓存命中: %s (type=%s) -> %d 条记录, CNAMEs=%v (过期:%v)",
		domain, dns.TypeToString[qtype], len(raw.Records), raw.CNAMEs, raw.IsExpired())

	// 计算 TTL
	elapsed := time.Since(raw.AcquisitionTime)
	userTTL := s.calculateUserTTL(int(raw.EffectiveTTL), elapsed, cfg, raw.IsExpired())

	// 构建通用响应
	msg := s.msgPool.Get()
	msg.SetReply(r)
	msg.RecursionAvailable = true
	msg.Compress = false
	authData := raw.AuthenticatedData && cfg.Upstream.Dnssec

	s.buildGenericResponse(msg, raw.CNAMEs, raw.Records, qtype, userTTL, authData)
	w.WriteMsg(msg)
	s.msgPool.Put(msg)

	if raw.IsExpired() {
		logger.Debugf("[handleRawCacheHitGeneric] 通用记录缓存已过期,触发异步刷新: %s (type=%s)",
			domain, dns.TypeToString[qtype])
		task := RefreshTask{Domain: domain, Qtype: qtype}
		s.refreshQueue.Submit(task)
	}

	return true
}
