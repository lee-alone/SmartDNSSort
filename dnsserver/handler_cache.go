package dnsserver

import (
	"fmt"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/stats"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// handleErrorCacheHit 处理错误缓存命中 (NXDOMAIN)
// 返回 true 表示请求已处理
func (s *Server) handleErrorCacheHit(w dns.ResponseWriter, r *dns.Msg, domain string, qtype uint16, stats *stats.Stats) bool {
	if _, ok := s.cache.GetError(domain, qtype); ok {
		stats.IncCacheHits()
		logger.Debugf("[handleQuery] NXDOMAIN 缓存命中: %s (type=%s)",
			domain, dns.TypeToString[qtype])

		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.RecursionAvailable = true
		msg.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(msg)
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

	// [优化] Stale-While-Revalidate 模式
	// 使用 s.cfg.Ping.RttCacheTtlSeconds 作为 "新鲜度" 阈值
	elapsed := time.Since(sorted.Timestamp)
	isFresh := elapsed.Seconds() < float64(cfg.Ping.RttCacheTtlSeconds)

	// 计算剩余 TTL (通用逻辑)
	remaining := int(sorted.TTL) - int(elapsed.Seconds())
	remaining = max(remaining, 0)

	// 计算目标 TTL: 如果上游 TTL 未过期，使用 UserReturnTTL 逻辑；否则使用 FastResponseTTL
	var calculatedUserTTL uint32
	if remaining > 0 {
		if cfg.Cache.UserReturnTTL > 0 {
			cycleOffset := int(elapsed.Seconds()) % cfg.Cache.UserReturnTTL
			cappedTTL := cfg.Cache.UserReturnTTL - cycleOffset
			if remaining < cappedTTL {
				calculatedUserTTL = uint32(remaining)
			} else {
				calculatedUserTTL = uint32(cappedTTL)
			}
		} else {
			calculatedUserTTL = uint32(remaining)
		}
	} else {
		calculatedUserTTL = uint32(cfg.Cache.FastResponseTTL)
	}

	var userTTL uint32
	if isFresh {
		// === 场景 1: 数据新鲜 ===
		userTTL = calculatedUserTTL
		logger.Debugf("[handleQuery] 排序缓存命中 (Fresh): %s (type=%s) -> %v (TTL=%d)",
			domain, dns.TypeToString[qtype], sorted.IPs, userTTL)
	} else {
		// === 场景 2: 数据陈旧 (SWR) ===
		// [Fix] 当返回陈旧数据时，强制使用较短的 FastResponseTTL
		// 这样客户端会在短时间内再次查询 (e.g. 15s)，届时后台刷新早已完成，客户端即可获得最新数据
		// 避免客户端被锁定在长 TTL (UserReturnTTL) 中导致长时间使用失效 IP
		fastTTL := uint32(cfg.Cache.FastResponseTTL)
		userTTL = min(calculatedUserTTL, fastTTL)
		logger.Debugf("[handleQuery] 排序缓存命中 (Stale): %s (type=%s) -> %v (TTL=%d, Force FastTTL)",
			domain, dns.TypeToString[qtype], sorted.IPs, userTTL)

		// 触发异步刷新，无论原始缓存状态如何
		go func() {
			sfKey := fmt.Sprintf("refresh:%s:%d", domain, qtype)
			s.requestGroup.Do(sfKey, func() (any, error) {
				// 双重检查防抖: 10秒内不重复刷新
				// 检查最新的 sorted entry 是否在最近被刷新过
				if latest, ok := s.cache.GetSorted(domain, qtype); ok {
					if time.Since(latest.Timestamp) < 10*time.Second {
						logger.Debugf("[handleQuery] Stale cache refresh skipped, recently updated for %s", domain)
						return nil, nil
					}
				}
				logger.Debugf("[handleQuery] Stale cache, triggering async refresh for %s", domain)
				task := RefreshTask{Domain: domain, Qtype: qtype}
				s.refreshQueue.Submit(task)
				return nil, nil
			})
		}()
	}

	// 检查是否有 CNAME（从原始缓存获取）
	var cnames []string
	if raw, ok := s.cache.GetRaw(domain, qtype); ok && len(raw.CNAMEs) > 0 {
		cnames = raw.CNAMEs
	}

	// 构造响应
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.RecursionAvailable = true
	msg.Compress = false
	if len(cnames) > 0 {
		s.buildDNSResponseWithCNAME(msg, domain, cnames, sorted.IPs, qtype, userTTL)
	} else {
		s.buildDNSResponse(msg, domain, sorted.IPs, qtype, userTTL)
	}
	w.WriteMsg(msg)
	return true
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

	// [Modified] 如果原始缓存未过期，使用 UserReturnTTL；否则使用 FastResponseTTL
	var userTTL uint32 = uint32(cfg.Cache.FastResponseTTL)

	if !raw.IsExpired() {
		elapsedRaw := time.Since(raw.AcquisitionTime)
		remainingRaw := int(raw.UpstreamTTL) - int(elapsedRaw.Seconds())

		if remainingRaw > 0 {
			if cfg.Cache.UserReturnTTL > 0 {
				cycleOffset := int(elapsedRaw.Seconds()) % cfg.Cache.UserReturnTTL
				cappedTTL := cfg.Cache.UserReturnTTL - cycleOffset
				if remainingRaw < cappedTTL {
					userTTL = uint32(remainingRaw)
				} else {
					userTTL = uint32(cappedTTL)
				}
			} else {
				userTTL = uint32(remainingRaw)
			}
		}
	}

	// 使用历史数据进行兜底排序 (Fallback Rank)
	// [Fix] 如果存在 CNAME，使用最终目标域名获取排序权重，因为 stats 是记在 target 上的
	rankDomain := domain
	if len(raw.CNAMEs) > 0 {
		rankDomain = strings.TrimRight(raw.CNAMEs[len(raw.CNAMEs)-1], ".")
	}
	fallbackIPs := s.prefetcher.GetFallbackRank(rankDomain, raw.IPs)

	msg := new(dns.Msg)
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
