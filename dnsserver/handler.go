package dnsserver

import (
	"context"
	"fmt"
	"net"
	"smartdnssort/adblock"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/stats"
	"smartdnssort/upstream"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// handleAdBlockCheck 执行 AdBlock 过滤检查
// 返回 (shouldReturn, responseWritten) - 如果 shouldReturn 为 true，表示请求已处理
func (s *Server) handleAdBlockCheck(w dns.ResponseWriter, r *dns.Msg, domain string, cfg *config.Config, adblockMgr *adblock.AdBlockManager) bool {
	if adblockMgr == nil || !cfg.AdBlock.Enable {
		return false
	}

	// 1. 检查拦截缓存 (快速路径)
	if entry, hit := s.cache.GetBlocked(domain); hit {
		logger.Debugf("[AdBlock] Cache Hit (Blocked): %s (rule: %s)", domain, entry.Rule)
		adblockMgr.RecordBlock(domain, entry.Rule)

		// 根据配置返回拦截响应
		switch cfg.AdBlock.BlockMode {
		case "nxdomain":
			buildNXDomainResponse(w, r)
		case "zero_ip":
			buildZeroIPResponse(w, r, cfg.AdBlock.BlockedResponseIP, cfg.AdBlock.BlockedTTL)
		case "refuse":
			buildRefuseResponse(w, r)
		default:
			buildNXDomainResponse(w, r)
		}
		return true
	}

	// 2. 检查白名单缓存 (快速路径)
	// 如果在白名单缓存中，直接跳过 AdBlock 检查
	if s.cache.GetAllowed(domain) {
		return false // 继续执行后续 DNS 逻辑
	}

	// 3. 执行完整的规则匹配
	if blocked, rule := adblockMgr.CheckHost(domain); blocked {
		logger.Debugf("[AdBlock] Blocked: %s (rule: %s)", domain, rule)

		// 记录统计
		adblockMgr.RecordBlock(domain, rule)

		// 写入拦截缓存
		s.cache.SetBlocked(domain, &cache.BlockedCacheEntry{
			BlockType: cfg.AdBlock.BlockMode,
			Rule:      rule,
			ExpiredAt: time.Now().Add(time.Duration(cfg.AdBlock.BlockedTTL) * time.Second),
		})

		// 根据配置返回拦截响应
		switch cfg.AdBlock.BlockMode {
		case "nxdomain":
			buildNXDomainResponse(w, r)
		case "zero_ip":
			buildZeroIPResponse(w, r, cfg.AdBlock.BlockedResponseIP, cfg.AdBlock.BlockedTTL)
		case "refuse":
			buildRefuseResponse(w, r)
		default:
			buildNXDomainResponse(w, r)
		}
		return true
	}

	// 写入白名单缓存
	// 缓存 10 分钟 (600秒)，避免频繁检查热门白名单域名
	s.cache.SetAllowed(domain, &cache.AllowedCacheEntry{
		ExpiredAt: time.Now().Add(600 * time.Second),
	})

	return false // 未被拦截，继续处理
}

// handleCustomResponse 处理自定义回复规则
// 返回 true 表示请求已处理
func (s *Server) handleCustomResponse(w dns.ResponseWriter, r *dns.Msg, domain string, qtype uint16) bool {
	if s.customRespManager == nil {
		return false
	}

	rules, matched := s.customRespManager.Match(domain, qtype)
	if !matched {
		return false
	}

	logger.Debugf("[CustomResponse] Matched: %s (type=%s), rules=%d", domain, dns.TypeToString[qtype], len(rules))

	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.RecursionAvailable = true
	msg.Compress = false

	// Check for CNAME
	var cnameRule *CustomRule
	var aRules []CustomRule

	for _, rule := range rules {
		if rule.Type == dns.TypeCNAME {
			cnameRule = &rule
			break // CNAME priority
		}
		if rule.Type == qtype {
			aRules = append(aRules, rule)
		}
	}

	if cnameRule != nil {
		// CNAME Response
		rr := new(dns.CNAME)
		rr.Hdr = dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: cnameRule.TTL}
		rr.Target = dns.Fqdn(cnameRule.Value)
		msg.Answer = append(msg.Answer, rr)
		w.WriteMsg(msg)
		return true
	} else if len(aRules) > 0 {
		// A/AAAA Response
		for _, rule := range aRules {
			var rr dns.RR
			header := dns.RR_Header{Name: r.Question[0].Name, Rrtype: rule.Type, Class: dns.ClassINET, Ttl: rule.TTL}
			switch rule.Type {
			case dns.TypeA:
				rr = &dns.A{Hdr: header, A: net.ParseIP(rule.Value)}
			case dns.TypeAAAA:
				rr = &dns.AAAA{Hdr: header, AAAA: net.ParseIP(rule.Value)}
			}
			if rr != nil {
				msg.Answer = append(msg.Answer, rr)
			}
		}
		w.WriteMsg(msg)
		return true
	}

	return false
}

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
	if remaining < 0 {
		remaining = 0
	}

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
		if calculatedUserTTL > fastTTL {
			userTTL = fastTTL
		} else {
			userTTL = calculatedUserTTL
		}
		logger.Debugf("[handleQuery] 排序缓存命中 (Stale): %s (type=%s) -> %v (TTL=%d, Force FastTTL)",
			domain, dns.TypeToString[qtype], sorted.IPs, userTTL)

		// 触发异步刷新，无论原始缓存状态如何
		go func() {
			sfKey := fmt.Sprintf("refresh:%s:%d", domain, qtype)
			s.requestGroup.Do(sfKey, func() (interface{}, error) {
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

// handleCNAMEChainValidation 对 CNAME 链进行 AdBlock 检查
// 返回 true 表示请求被拦截
func (s *Server) handleCNAMEChainValidation(w dns.ResponseWriter, r *dns.Msg, domain string, cnames []string, cfg *config.Config, adblockMgr *adblock.AdBlockManager) bool {
	if adblockMgr == nil || !cfg.AdBlock.Enable || len(cnames) == 0 {
		return false
	}

	for _, cnameToCheck := range cnames {
		cnameDomain := strings.TrimRight(cnameToCheck, ".")
		if blocked, rule := adblockMgr.CheckHost(cnameDomain); blocked {
			logger.Debugf("[AdBlock] CNAME Blocked: %s found in chain for %s (rule: %s)", cnameDomain, domain, rule)
			adblockMgr.RecordBlock(domain, rule) // 记录主域名被拦截

			// 写入拦截缓存 (针对主域名)
			s.cache.SetBlocked(domain, &cache.BlockedCacheEntry{
				BlockType: cfg.AdBlock.BlockMode,
				Rule:      rule,
				ExpiredAt: time.Now().Add(time.Duration(cfg.AdBlock.BlockedTTL) * time.Second),
			})

			// 返回拦截响应
			switch cfg.AdBlock.BlockMode {
			case "nxdomain":
				buildNXDomainResponse(w, r)
			case "zero_ip":
				buildZeroIPResponse(w, r, cfg.AdBlock.BlockedResponseIP, cfg.AdBlock.BlockedTTL)
			case "refuse":
				buildRefuseResponse(w, r)
			default:
				buildNXDomainResponse(w, r)
			}
			return true
		}
	}
	return false
}

// handleCacheMiss 处理缓存未命中的情况（首次查询）
func (s *Server) handleCacheMiss(w dns.ResponseWriter, r *dns.Msg, domain string, question dns.Question, ctx context.Context, currentUpstream *upstream.Manager, currentCfg *config.Config, currentStats *stats.Stats, adblockMgr *adblock.AdBlockManager) {
	qtype := question.Qtype

	currentStats.IncCacheMisses()

	// ========== IPv6 开关检查 ==========
	if qtype == dns.TypeAAAA && !currentCfg.DNS.EnableIPv6 {
		logger.Debugf("[handleQuery] IPv6 已禁用，直接返回空响应: %s", domain)
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.RecursionAvailable = true
		msg.Compress = false
		msg.SetRcode(r, dns.RcodeSuccess)
		msg.Answer = nil
		w.WriteMsg(msg)
		return
	}

	// ========== 阶段一：首次查询（无缓存）==========
	logger.Debugf("[handleQuery] 首次查询，无缓存: %s (type=%s)", domain, dns.TypeToString[qtype])

	// 计算动态超时时间: timeout_ms × 健康服务器数
	healthyServerCount := currentUpstream.GetHealthyServerCount()
	if healthyServerCount == 0 {
		healthyServerCount = 1
	}

	// 设置最大总超时上限 (30秒),避免服务器太多时超时过长
	maxTotalTimeout := 30 * time.Second
	totalTimeout := time.Duration(currentCfg.Upstream.TimeoutMs*healthyServerCount) * time.Millisecond
	if totalTimeout > maxTotalTimeout {
		totalTimeout = maxTotalTimeout
	}

	logger.Debugf("[handleQuery] 动态超时计算: 健康服务器=%d, 单次超时=%dms, 总超时=%v",
		healthyServerCount, currentCfg.Upstream.TimeoutMs, totalTimeout)

	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	// 使用 singleflight 合并相同的并发请求
	sfKey := fmt.Sprintf("query:%s:%d", domain, qtype)

	v, err, shared := s.requestGroup.Do(sfKey, func() (interface{}, error) {
		return currentUpstream.Query(ctx, r, currentCfg.Upstream.Dnssec)
	})

	if shared {
		logger.Debugf("[handleQuery] 合并并发请求: %s (type=%s)", domain, dns.TypeToString[qtype])
	}

	var result *upstream.QueryResultWithTTL
	if err == nil {
		result = v.(*upstream.QueryResultWithTTL)
	}

	if err != nil {
		logger.Warnf("[handleQuery] 上游查询失败: %v", err)
		originalRcode := parseRcodeFromError(err)
		if originalRcode != dns.RcodeNameError {
			currentStats.IncUpstreamFailures()
		}

		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.RecursionAvailable = true
		msg.Compress = false

		if originalRcode == dns.RcodeNameError {
			s.cache.SetError(domain, qtype, originalRcode, currentCfg.Cache.ErrorCacheTTL)
			logger.Debugf("[handleQuery] NXDOMAIN 错误，缓存并返回: %s", domain)
			msg.SetRcode(r, dns.RcodeNameError)
			w.WriteMsg(msg)
		} else {
			logger.Debugf("[handleQuery] SERVFAIL/超时错误，返回空响应（不缓存）: %s, Rcode=%d", domain, originalRcode)
			msg.SetRcode(r, dns.RcodeSuccess)
			msg.Answer = nil
			w.WriteMsg(msg)
		}
		return
	}

	// --- 统一处理入口 ---

	var finalIPs []string
	var fullCNAMEs []string
	var finalTTL uint32

	if len(result.IPs) == 0 && len(result.CNAMEs) > 0 {
		// 场景1: 只有 CNAME，需要递归解析
		lastCNAME := result.CNAMEs[len(result.CNAMEs)-1]
		logger.Debugf("[handleQuery] 上游查询返回 CNAMEs=%v，开始递归解析最后一个: %s -> %s", result.CNAMEs, domain, lastCNAME)

		finalResult, resolveErr := s.resolveCNAME(ctx, lastCNAME, qtype, r, currentCfg.Upstream.Dnssec)
		if resolveErr != nil {
			logger.Warnf("[handleQuery] CNAME 递归解析失败: %v", resolveErr)
			msg := new(dns.Msg)
			msg.SetReply(r)
			msg.RecursionAvailable = true
			msg.Compress = false
			msg.SetRcode(r, dns.RcodeServerFailure)
			w.WriteMsg(msg)
			return
		}

		finalIPs = finalResult.IPs
		// 完整链 = 初始链 + 递归解析出的链
		fullCNAMEs = append(result.CNAMEs, finalResult.CNAMEs...)
		finalTTL = finalResult.TTL
	} else {
		// 场景2: 直接获得了 IP (可能也带了 CNAME) 或 空结果
		finalIPs = result.IPs
		fullCNAMEs = result.CNAMEs
		finalTTL = result.TTL
	}

	// [AdBlock] 对最终的完整 CNAME 链进行检查
	if s.handleCNAMEChainValidation(w, r, domain, fullCNAMEs, currentCfg, adblockMgr) {
		return // 请求被拦截
	}

	// 如果最终没有IP也没有CNAME，那就是 NODATA
	if len(finalIPs) == 0 && len(fullCNAMEs) == 0 {
		logger.Debugf("[handleQuery] 上游查询返回空结果 (NODATA): %s", domain)
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.RecursionAvailable = true
		msg.Compress = false
		msg.SetRcode(r, dns.RcodeSuccess)
		msg.Answer = nil
		w.WriteMsg(msg)
		return
	}

	// --- 缓存与排序 ---
	currentStats.RecordDomainQuery(domain)
	logger.Debugf("[handleQuery] 最终解析结果: %s (type=%s) 获得 %d 个IP, 完整 CNAMEs=%v (TTL=%d秒): %v",
		domain, dns.TypeToString[qtype], len(finalIPs), fullCNAMEs, finalTTL, finalIPs)

	// [Fix] 为CNAME链中的每个域名都创建缓存和排序任务
	// 总是保存实际的 AuthenticatedData 值到缓存，响应时根据配置决定是否转发
	s.cache.SetRawWithDNSSEC(domain, qtype, finalIPs, fullCNAMEs, finalTTL, result.AuthenticatedData)
	if len(finalIPs) > 0 {
		go s.sortIPsAsync(domain, qtype, finalIPs, finalTTL, time.Now())
	}

	for i, cname := range fullCNAMEs {
		cnameDomain := strings.TrimRight(cname, ".")
		var subCNAMEs []string
		if i < len(fullCNAMEs)-1 {
			subCNAMEs = fullCNAMEs[i+1:]
		}
		s.cache.SetRaw(cnameDomain, qtype, finalIPs, subCNAMEs, finalTTL)
		if len(finalIPs) > 0 {
			go s.sortIPsAsync(cnameDomain, qtype, finalIPs, finalTTL, time.Now())
		}
	}

	// --- 快速响应 ---
	// 使用历史数据进行兜底排序 (Fallback Rank)
	rankDomain := domain
	if len(fullCNAMEs) > 0 {
		rankDomain = strings.TrimRight(fullCNAMEs[len(fullCNAMEs)-1], ".")
	}
	fallbackIPs := s.prefetcher.GetFallbackRank(rankDomain, finalIPs)
	fastTTL := uint32(currentCfg.Cache.FastResponseTTL)

	msg := new(dns.Msg)
	msg.RecursionAvailable = true
	msg.SetReply(r)
	msg.Compress = false
	// 仅当启用 DNSSEC 时才转发验证标记
	authData := result.AuthenticatedData && currentCfg.Upstream.Dnssec
	if len(fullCNAMEs) > 0 {
		s.buildDNSResponseWithCNAMEAndDNSSEC(msg, domain, fullCNAMEs, fallbackIPs, qtype, fastTTL, authData)
	} else {
		s.buildDNSResponseWithDNSSEC(msg, domain, fallbackIPs, qtype, fastTTL, authData)
	}
	w.WriteMsg(msg)
}

func (s *Server) handleQuery(w dns.ResponseWriter, r *dns.Msg) {
	s.mu.RLock()
	// Copy pointers and values needed for the query under the read lock
	currentUpstream := s.upstream
	currentCfg := s.cfg
	currentStats := s.stats
	adblockMgr := s.adblockManager
	s.mu.RUnlock() // Release the lock early

	currentStats.IncQueries()

	if len(r.Question) == 0 {
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.RecursionAvailable = true
		w.WriteMsg(msg)
		return
	}

	question := r.Question[0]
	domain := strings.TrimRight(question.Name, ".")
	qtype := question.Qtype

	// ========== 第 1 阶段: AdBlock 过滤检查 ==========
	if s.handleAdBlockCheck(w, r, domain, currentCfg, adblockMgr) {
		return // 请求被拦截
	}

	// ========== 第 2 阶段: 自定义回复规则检查 ==========
	if s.handleCustomResponse(w, r, domain, qtype) {
		return // 请求已被自定义规则处理
	}

	// ========== 第 3 阶段: 本地规则检查 & 基础验证 ==========
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.RecursionAvailable = true
	msg.Compress = false

	if s.handleLocalRules(w, r, msg, domain, question) {
		return // 请求已被本地规则处理
	}

	// 仅处理 A 和 AAAA 查询
	if qtype != dns.TypeA && qtype != dns.TypeAAAA {
		msg.SetRcode(r, dns.RcodeNotImplemented)
		w.WriteMsg(msg)
		return
	}

	s.RecordRecentQuery(domain)
	logger.Debugf("[handleQuery] 查询: %s (type=%s)", domain, dns.TypeToString[qtype])

	// ========== 第 4 阶段: 缓存查询 ==========
	// 优先级：错误缓存 -> 排序缓存 -> 原始缓存 -> 缓存未命中

	if s.handleErrorCacheHit(w, r, domain, qtype, currentStats) {
		return
	}

	if s.handleSortedCacheHit(w, r, domain, qtype, currentCfg, currentStats) {
		return
	}

	if s.handleRawCacheHit(w, r, domain, qtype, currentCfg, currentStats) {
		return
	}

	// ========== 第 5 阶段: 缓存未命中，执行首次查询 ==========
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.handleCacheMiss(w, r, domain, question, ctx, currentUpstream, currentCfg, currentStats, adblockMgr)
}

// handleLocalRules applies a set of hardcoded rules to block or redirect common bogus queries.
// It returns true if the query was handled, meaning the caller should stop processing.
func (s *Server) handleLocalRules(w dns.ResponseWriter, r *dns.Msg, msg *dns.Msg, domain string, question dns.Question) bool {
	// Rule: Single-label domain (no dots)
	if !strings.Contains(domain, ".") {
		logger.Debugf("[QueryFilter] REFUSED: single-label domain query for '%s'", domain)
		msg.SetRcode(r, dns.RcodeRefused)
		w.WriteMsg(msg)
		return true
	}

	// Rule: localhost
	if domain == "localhost" {
		logger.Debugf("[QueryFilter] STATIC: localhost query for '%s'", domain)
		var ips []string
		switch question.Qtype {
		case dns.TypeA:
			ips = []string{"127.0.0.1"}
		case dns.TypeAAAA:
			ips = []string{"::1"}
		}
		s.buildDNSResponse(msg, domain, ips, question.Qtype, 3600) // 1 hour TTL
		w.WriteMsg(msg)
		return true
	}

	// Rule: Reverse DNS queries
	if strings.HasSuffix(domain, ".in-addr.arpa") || strings.HasSuffix(domain, ".ip6.arpa") {
		logger.Debugf("[QueryFilter] REFUSED: reverse DNS query for '%s'", domain)
		msg.SetRcode(r, dns.RcodeRefused)
		w.WriteMsg(msg)
		return true
	}

	// Rule: Blocklist for specific domains and suffixes
	// Using a map for exact matches is efficient.
	blockedDomains := map[string]int{
		"local":                     dns.RcodeRefused,
		"corp":                      dns.RcodeRefused,
		"home":                      dns.RcodeRefused,
		"lan":                       dns.RcodeRefused,
		"internal":                  dns.RcodeRefused,
		"intranet":                  dns.RcodeRefused,
		"private":                   dns.RcodeRefused,
		"home.arpa":                 dns.RcodeRefused,
		"wpad":                      dns.RcodeNameError, // NXDOMAIN is better for wpad
		"isatap":                    dns.RcodeRefused,
		"teredo.ipv6.microsoft.com": dns.RcodeNameError,
	}

	if rcode, ok := blockedDomains[domain]; ok {
		logger.Debugf("[QueryFilter] Rule match for '%s', responding with %s", domain, dns.RcodeToString[rcode])
		msg.SetRcode(r, rcode)
		w.WriteMsg(msg)
		return true
	}

	return false // Not handled by filter
}

// resolveCNAME 递归解析 CNAME，直到找到 IP 地址.
// 它返回最终的 IP 和在解析过程中发现的 *所有* CNAME。
func (s *Server) resolveCNAME(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*upstream.QueryResultWithTTL, error) {
	const maxRedirects = 10
	currentDomain := domain
	var accumulatedCNAMEs []string

	var finalResult *upstream.QueryResultWithTTL

	for i := 0; i < maxRedirects; i++ {
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

		// 累加发现的 CNAME
		if len(result.CNAMEs) > 0 {
			accumulatedCNAMEs = append(accumulatedCNAMEs, result.CNAMEs...)
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
		// 在这种情况下，我们仍认为解析是“成功”的，但返回空 IP 列表
		finalResult = result
		break
	}

	if finalResult == nil {
		return nil, fmt.Errorf("cname resolution failed: exceeded max redirects for %s", domain)
	}

	// 确保返回的 CNAME 链是完整的
	finalResult.CNAMEs = accumulatedCNAMEs
	return finalResult, nil
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
