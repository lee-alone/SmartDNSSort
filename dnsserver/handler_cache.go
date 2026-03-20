package dnsserver

import (
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/ping"
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

	// 1. 判断数据"新鲜度" (RTT 层面)
	elapsed := time.Since(sorted.Timestamp)
	rttStale := elapsed.Seconds() >= float64(cfg.Ping.RttCacheTtlSeconds)

	// 2. 计算上游 DNS 层面是否过期 (DNS 层面)
	raw, hasRaw := s.cache.GetRaw(domain, qtype)
	dnsExpired := !hasRaw || raw.IsExpired()

	// 3. 【故障检测】检查所有 IP 是否均为失效状态 (RTT >= LogicDeadRTT)
	isDeadPool := true
	for _, rtt := range sorted.RTTs {
		if rtt < ping.LogicDeadRTT { // 只要有一个能通，就不算死局
			isDeadPool = false
			break
		}
	}

	// 4. 计算用户视角下的 TTL
	// 如果 DNS 已过期，强制返回 fast_response_ttl
	var userTTL uint32
	if isDeadPool {
		// 核心整改点：如果是全死 IP，强行降级为 1s
		// 目的：1秒缓存保护系统，1秒过期引导客户端再次解析以尝试获取新IP
		userTTL = 1
		dnsExpired = true
		logger.Warnf("[Guard] 域名 %s 所有IP均失效，强制降级TTL并标记过期", domain)
	} else if dnsExpired {
		userTTL = uint32(cfg.Cache.FastResponseTTL)
	} else {
		// 走您现有的复杂 TTL 计算逻辑，保持兼容性
		userTTL = s.calculateUserTTL(sorted.TTL, elapsed, cfg, rttStale)
	}

	// 5. 异步刷新策略：精准决策
	if isDeadPool || rttStale {
		if dnsExpired {
			// 主动触发全量刷新，去上游拿新节点
			logger.Debugf("[handleQuery] 排序缓存命中 (Stale: DNS+RTT), 触发异步全量刷新: %s", domain)
			s.RefreshDomain(domain, qtype)
		} else {
			// DNS 没过期，只是需要重新测速，走轻量的 sortIPsAsync
			logger.Debugf("[handleQuery] 排序缓存命中 (Stale: RTT only), 触发异步探测刷新: %s", domain)
			go s.sortIPsAsync(domain, qtype, raw.IPs, raw.UpstreamTTL, raw.AcquisitionTime)
		}
	}

	// 6. 动态洗牌：使用 IPPool 的最新 RTT 数据重新校验顺序
	// 遗留问题修复：确保返回给用户的顺序永远基于最新的测速数据
	// 即使命中了"新鲜"的排序缓存，也要用 IPPool 的最新数据微调顺序
	s.mu.RLock()
	pinger := s.pinger
	s.mu.RUnlock()

	ipsToReturn := sorted.IPs // 默认使用缓存顺序
	if pinger != nil {
		if ipPool := pinger.GetIPPool(); ipPool != nil {
			latestRttMap := ipPool.GetAllIPRTTs(sorted.IPs)
			if len(latestRttMap) > 0 {
				// 使用真理库（IPPool）的最新 RTT 动态覆盖缓存顺序
				ipsToReturn, _, _ = s.sortIPsByRTT(sorted.IPs, latestRttMap, domain)
				logger.Debugf("[handleSortedCacheHit] 使用 IPPool 实时数据对新鲜缓存进行重排: %s -> %v", domain, ipsToReturn)
			}
		}
	}

	// 7. 构造响应
	msg := s.msgPool.Get()
	msg.SetReply(r)
	msg.RecursionAvailable = true
	msg.Compress = false

	var cnames []string
	if hasRaw && len(raw.CNAMEs) > 0 {
		cnames = raw.CNAMEs
	}

	if len(cnames) > 0 {
		s.buildDNSResponseWithCNAME(msg, domain, cnames, ipsToReturn, qtype, userTTL)
	} else {
		s.buildDNSResponse(msg, domain, ipsToReturn, qtype, userTTL)
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
// 第二阶段改造：实现 Stale-While-Revalidate "抢跑回显"机制
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

	// 第二阶段改造：三段式过期判定
	// 优雅期：只要数据还在缓存中（未被清理），就允许通过 Stale-While-Revalidate 返回
	// 自动优化：如果内存压力极低（<50%），即使配置关闭了 KeepExpiredEntries，也自动允许使用陈旧数据以加速响应
	gracePeriod := uint32(cache.AncientLimitLowPressure) 
	useKeepExpired := cfg.Cache.KeepExpiredEntries || s.cache.GetMemoryUsagePercent() < 0.5
	cacheState := raw.GetStateWithConfig(useKeepExpired, gracePeriod)

	elapsed := time.Since(raw.AcquisitionTime)
	var userTTL uint32
	var needRefresh bool

	switch cacheState {
	case cache.FRESH:
		// Fresh 状态：直接返回，TTL 使用 UserReturnTTL（受 EffectiveTTL 余额限制）
		userTTL = s.calculateUserTTL(int(raw.EffectiveTTL), elapsed, cfg, false)
		logger.Debugf("[handleQuery] 原始缓存命中 (Fresh): %s (type=%s) -> %v, CNAMEs=%v, TTL=%d",
			domain, dns.TypeToString[qtype], raw.IPs, raw.CNAMEs, userTTL)
		// Fresh 状态下，只触发轻量的测速刷新，不查询上游
		needRefresh = true

	case cache.STALE:
		// Stale 状态：立即返回陈旧数据，但强制响应中的 TTL 为 FastResponseTTL
		// 同时触发后台异步任务去上游刷新 IP 并重新测速
		userTTL = uint32(cfg.Cache.FastResponseTTL)
		logger.Infof("[handleQuery] 原始缓存命中 (Stale-While-Revalidate): %s (type=%s) -> %v, CNAMEs=%v, TTL=%d [STALE-HIT]",
			domain, dns.TypeToString[qtype], raw.IPs, raw.CNAMEs, userTTL)

		// 触发后台异步刷新
		stats.IncCacheStaleRefresh()
		task := RefreshTask{Domain: domain, Qtype: qtype}
		s.refreshQueue.Submit(task)
		needRefresh = false // 已经提交了刷新任务

	case cache.EXPIRED:
		// Expired 状态：彻底过期，需要重新查询上游
		// 审计修复：EXPIRED 状态即便开启了 KeepExpiredEntries，也应该强制去上游查询一次
		// 确保数据的绝对可靠性
		logger.Debugf("[handleQuery] 原始缓存已过期 (Expired): %s (type=%s)",
			domain, dns.TypeToString[qtype])
		return false // 让上层去上游查询
	}

	// 优先使用排序缓存，如果不存在则使用历史数据进行兜底排序
	var ipsToReturn []string

	// 1. 首先尝试获取排序缓存（允许获取 Stale 数据，因为此时 Raw 缓存本身就是 Stale 的）
	if sorted, ok := s.cache.GetSortedWithStale(domain, qtype, true); ok {
		logger.Debugf("[handleQuery] 排序缓存命中 (可能为 Stale): %s (type=%s) -> %v", domain, dns.TypeToString[qtype], sorted.IPs)

		// 审计修复：Phase 3 遗漏 - 使用 IPPool 的最新 RTT 数据重新校验顺序
		// 即使命中排序缓存，也尝试用最新的 IPPool 数据微调顺序
		s.mu.RLock()
		pinger := s.pinger
		s.mu.RUnlock()

		if pinger != nil {
			ipPool := pinger.GetIPPool()
			if ipPool != nil {
				latestRttMap := ipPool.GetAllIPRTTs(sorted.IPs)
				if len(latestRttMap) > 0 {
					// 使用最新的 RTT 数据重新排序
					ipsToReturn, _, _ = s.sortIPsByRTT(sorted.IPs, latestRttMap, domain)
					logger.Debugf("[handleQuery] 使用 IPPool 最新 RTT 数据重新排序: %s -> %v", domain, ipsToReturn)
				} else {
					ipsToReturn = sorted.IPs
				}
			} else {
				ipsToReturn = sorted.IPs
			}
		} else {
			ipsToReturn = sorted.IPs
		}
	} else {
		// 2. 排序缓存不存在，使用历史数据进行兜底排序 (Fallback Rank)
		// [Fix] 如果存在 CNAME，使用最终目标域名获取排序权重，因为 stats 是记在 target 上的
		rankDomain := domain
		if len(raw.CNAMEs) > 0 {
			rankDomain = strings.TrimRight(raw.CNAMEs[len(raw.CNAMEs)-1], ".")
		}
		ipsToReturn = s.prefetcher.GetFallbackRank(rankDomain, raw.IPs)
		logger.Debugf("[handleQuery] 使用兜底排序: %s (type=%s) -> %v", domain, dns.TypeToString[qtype], ipsToReturn)
	}

	msg := s.msgPool.Get()
	msg.RecursionAvailable = true
	msg.SetReply(r)
	msg.Compress = false
	// 仅当启用 DNSSEC 时才转发验证标记
	authData := raw.AuthenticatedData && cfg.Upstream.Dnssec
	if len(raw.CNAMEs) > 0 {
		s.buildDNSResponseWithCNAMEAndDNSSEC(msg, domain, raw.CNAMEs, ipsToReturn, qtype, userTTL, authData)
	} else {
		s.buildDNSResponseWithDNSSEC(msg, domain, ipsToReturn, qtype, userTTL, authData)
	}
	w.WriteMsg(msg)
	s.msgPool.Put(msg)

	// 审计修复：移除重复的刷新任务提交逻辑
	// 原代码在 switch 中已经提交了刷新任务，这里不需要重复提交
	if needRefresh {
		// Fresh 状态下，只触发轻量的测速刷新，不查询上游
		go s.sortIPsAsync(domain, qtype, raw.IPs, raw.UpstreamTTL, raw.AcquisitionTime)
	}

	return true
}

// handleRawCacheHitGeneric 处理通用记录的原始缓存命中
// 审计修复：应用三段式过期判定逻辑，与 A/AAAA 记录保持一致
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

	// 审计修复：应用三段式过期判定逻辑
	gracePeriod := uint32(cache.AncientLimitLowPressure) 
	useKeepExpired := cfg.Cache.KeepExpiredEntries || s.cache.GetMemoryUsagePercent() < 0.5
	cacheState := raw.GetStateWithConfig(useKeepExpired, gracePeriod)

	elapsed := time.Since(raw.AcquisitionTime)
	var userTTL uint32

	switch cacheState {
	case cache.FRESH:
		// Fresh 状态：直接返回
		userTTL = s.calculateUserTTL(int(raw.EffectiveTTL), elapsed, cfg, false)
		logger.Debugf("[handleRawCacheHitGeneric] 通用记录缓存命中 (Fresh): %s (type=%s) -> %d 条记录, CNAMEs=%v, TTL=%d",
			domain, dns.TypeToString[qtype], len(raw.Records), raw.CNAMEs, userTTL)

	case cache.STALE:
		// Stale 状态：立即返回陈旧数据，但强制响应中的 TTL 为 FastResponseTTL
		userTTL = uint32(cfg.Cache.FastResponseTTL)
		logger.Infof("[handleRawCacheHitGeneric] 通用记录缓存命中 (Stale-While-Revalidate): %s (type=%s) -> %d 条记录, CNAMEs=%v, TTL=%d [STALE-HIT]",
			domain, dns.TypeToString[qtype], len(raw.Records), raw.CNAMEs, userTTL)

		// 触发后台异步刷新
		stats.IncCacheStaleRefresh()
		task := RefreshTask{Domain: domain, Qtype: qtype}
		s.refreshQueue.Submit(task)

	case cache.EXPIRED:
		// Expired 状态：彻底过期，需要重新查询上游
		logger.Debugf("[handleRawCacheHitGeneric] 通用记录缓存已过期 (Expired): %s (type=%s)",
			domain, dns.TypeToString[qtype])
		return false // 让上层去上游查询
	}

	// 构建通用响应
	msg := s.msgPool.Get()
	msg.SetReply(r)
	msg.RecursionAvailable = true
	msg.Compress = false
	authData := raw.AuthenticatedData && cfg.Upstream.Dnssec

	s.buildGenericResponse(msg, raw.CNAMEs, raw.Records, qtype, userTTL, authData)
	w.WriteMsg(msg)
	s.msgPool.Put(msg)

	return true
}
