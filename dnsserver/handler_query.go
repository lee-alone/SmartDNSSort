package dnsserver

import (
	"context"
	"fmt"
	"smartdnssort/adblock"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/stats"
	"smartdnssort/upstream"
	"strings"
	"time"

	"github.com/miekg/dns"
)

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
	totalTimeout = min(totalTimeout, maxTotalTimeout)

	logger.Debugf("[handleQuery] 动态超时计算: 健康服务器=%d, 单次超时=%dms, 总超时=%v",
		healthyServerCount, currentCfg.Upstream.TimeoutMs, totalTimeout)

	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	// 使用 singleflight 合并相同的并发请求
	sfKey := fmt.Sprintf("query:%s:%d", domain, qtype)

	v, err, shared := s.requestGroup.Do(sfKey, func() (any, error) {
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

	// DNSSEC msgCache: 如果请求带有 DO 标志且启用了 DNSSEC，存储完整消息
	if currentCfg.Upstream.Dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
		if result.DnsMsg != nil {
			logger.Debugf("[handleQuery] 将完整 DNSSEC 消息存储到 msgCache: %s (type=%s)", domain, dns.TypeToString[qtype])

			// Helper to set DNSSEC message to cache for a given domain/qtype
			setDNSSECMsgToCache := func(d string, qt uint16, msg *dns.Msg) {
				s.cache.SetDNSSECMsg(d, qt, msg)
			}

			// For direct A/AAAA records, use the requested domain
			setDNSSECMsgToCache(domain, qtype, result.DnsMsg)

			// For each domain in the CNAME chain, also write the same full message
			// This allows any domain in the chain to hit msgCache later
			for _, cname := range fullCNAMEs {
				cnameDomain := strings.TrimRight(cname, ".")
				setDNSSECMsgToCache(cnameDomain, qtype, result.DnsMsg)
			}

			logger.Debugf("[handleQuery] DNSSEC 完整消息已存储到 msgCache: %s 及其 CNAME 链", domain)
		}
	}

	if len(fullCNAMEs) > 0 {
		s.buildDNSResponseWithCNAMEAndDNSSEC(msg, domain, fullCNAMEs, fallbackIPs, qtype, fastTTL, authData)
	} else {
		s.buildDNSResponseWithDNSSEC(msg, domain, fallbackIPs, qtype, fastTTL, authData)
	}
	w.WriteMsg(msg)
}

// adjustTTL decrements the TTL of DNS resource records by the elapsed duration.
// It ensures TTL does not go below a minimum value (1 second).
func adjustTTL(rrs []dns.RR, elapsed time.Duration) {
	for _, rr := range rrs {
		header := rr.Header()
		if header.Ttl > 0 { // Only adjust if TTL is not already 0
			newTTL := int64(header.Ttl) - int64(elapsed.Seconds())
			if newTTL <= 0 {
				header.Ttl = 1 // Ensure TTL is at least 1
			} else {
				header.Ttl = uint32(newTTL)
			}
		}
	}
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
	// 优先级：DNSSEC msgCache -> 错误缓存 -> 排序缓存 -> 原始缓存 -> 缓存未命中

	// 检测是否为 DNSSEC 请求（DO 标志）
	isDNSSECQuery := r.IsEdns0() != nil && r.IsEdns0().Do()

	// DNSSEC 完整消息缓存（仅当启用 DNSSEC 且请求带有 DO 标志时）
	if isDNSSECQuery && currentCfg.Upstream.Dnssec {
		if entry, found := s.cache.GetDNSSECMsg(domain, qtype); found {
			logger.Debugf("[handleQuery] DNSSEC msgCache 命中: %s (type=%s)", domain, dns.TypeToString[qtype])
			currentStats.IncCacheHits()

			// Create a deep copy of the cached message to modify TTLs
			responseMsg := entry.Message.Copy()
			elapsed := time.Since(entry.AcquisitionTime)

			// Adjust TTLs for all records in the response
			adjustTTL(responseMsg.Answer, elapsed)
			adjustTTL(responseMsg.Ns, elapsed)
			adjustTTL(responseMsg.Extra, elapsed)

			responseMsg.RecursionAvailable = true
			responseMsg.Id = r.Id
			responseMsg.Compress = false
			w.WriteMsg(responseMsg)
			return
		}
	}

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
