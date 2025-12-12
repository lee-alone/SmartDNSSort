package dnsserver

import (
	"context"
	"fmt"
	"net"
	"smartdnssort/cache"
	"smartdnssort/logger"
	"smartdnssort/upstream"
	"strings"
	"time"

	"github.com/miekg/dns"
)

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
		w.WriteMsg(msg)
		return
	}

	question := r.Question[0]
	domain := strings.TrimRight(question.Name, ".")
	qtype := question.Qtype

	// AdBlock 过滤检查
	if adblockMgr != nil && currentCfg.AdBlock.Enable {
		// 1. 检查拦截缓存 (快速路径)
		if entry, hit := s.cache.GetBlocked(domain); hit {
			logger.Debugf("[AdBlock] Cache Hit (Blocked): %s (rule: %s)", domain, entry.Rule)
			adblockMgr.RecordBlock(domain, entry.Rule)

			// 使用缓存中的 BlockType 或当前配置
			// 这里我们使用当前配置以保持一致性，但缓存的存在意味着它被拦截了
			switch currentCfg.AdBlock.BlockMode {
			case "nxdomain":
				buildNXDomainResponse(w, r)
			case "zero_ip":
				buildZeroIPResponse(w, r, currentCfg.AdBlock.BlockedResponseIP, currentCfg.AdBlock.BlockedTTL)
			case "refuse":
				buildRefuseResponse(w, r)
			default:
				buildNXDomainResponse(w, r)
			}
			return
		}

		// 2. 检查白名单缓存 (快速路径)
		// 如果在白名单缓存中，直接跳过 AdBlock 检查
		if s.cache.GetAllowed(domain) {
			// log.Printf("[AdBlock] Cache Hit (Allowed): %s", domain)
			// 继续执行后续 DNS 逻辑
		} else {
			// 3. 执行完整的规则匹配
			if blocked, rule := adblockMgr.CheckHost(domain); blocked {
				logger.Debugf("[AdBlock] Blocked: %s (rule: %s)", domain, rule)

				// 记录统计
				adblockMgr.RecordBlock(domain, rule)

				// 写入拦截缓存
				s.cache.SetBlocked(domain, &cache.BlockedCacheEntry{
					BlockType: currentCfg.AdBlock.BlockMode,
					Rule:      rule,
					ExpiredAt: time.Now().Add(time.Duration(currentCfg.AdBlock.BlockedTTL) * time.Second),
				})

				// 根据配置返回响应
				switch currentCfg.AdBlock.BlockMode {
				case "nxdomain":
					buildNXDomainResponse(w, r)
				case "zero_ip":
					buildZeroIPResponse(w, r, currentCfg.AdBlock.BlockedResponseIP, currentCfg.AdBlock.BlockedTTL)
				case "refuse":
					buildRefuseResponse(w, r)
				default:
					buildNXDomainResponse(w, r)
				}
				return
			} else {
				// 写入白名单缓存
				// 缓存 10 分钟 (600秒)，避免频繁检查热门白名单域名
				s.cache.SetAllowed(domain, &cache.AllowedCacheEntry{
					ExpiredAt: time.Now().Add(600 * time.Second),
				})
			}
		}
	}

	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Compress = false

	// ========== 自定义回复规则检查 ==========
	if s.customRespManager != nil {
		if rules, matched := s.customRespManager.Match(domain, qtype); matched {
			logger.Debugf("[CustomResponse] Matched: %s (type=%s), rules=%d", domain, dns.TypeToString[qtype], len(rules))

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
				// Custom response rules typicaly imply a single CNAME target
				rr := new(dns.CNAME)
				rr.Hdr = dns.RR_Header{Name: question.Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: cnameRule.TTL}
				rr.Target = dns.Fqdn(cnameRule.Value)
				msg.Answer = append(msg.Answer, rr)
				w.WriteMsg(msg)
				return
			} else if len(aRules) > 0 {
				// A/AAAA Response
				for _, rule := range aRules {
					var rr dns.RR
					header := dns.RR_Header{Name: question.Name, Rrtype: rule.Type, Class: dns.ClassINET, Ttl: rule.TTL}
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
				return
			}
		}
	}

	// ========== 规则过滤 ==========
	// 在处理任何逻辑之前，首先应用本地规则
	if s.handleLocalRules(w, r, msg, domain, question) {
		return // 如果规则已处理该请求，则直接返回
	}

	// 仅处理 A 和 AAAA 查询
	if question.Qtype != dns.TypeA && question.Qtype != dns.TypeAAAA {
		msg.SetRcode(r, dns.RcodeNotImplemented)
		w.WriteMsg(msg)
		return
	}

	// ✅ 记录域名查询逻辑已移动到解析成功后
	// 这样只统计有效域名（能解析出IP的域名）
	s.RecordRecentQuery(domain)

	logger.Debugf("[handleQuery] 查询: %s (type=%s)", domain, dns.TypeToString[question.Qtype])

	// ========== 优先检查错误缓存 ==========
	// 只缓存 NXDOMAIN（域名不存在）错误，不缓存 SERVFAIL 等临时错误
	if _, ok := s.cache.GetError(domain, question.Qtype); ok {
		currentStats.IncCacheHits()
		logger.Debugf("[handleQuery] NXDOMAIN 缓存命中: %s (type=%s)",
			domain, dns.TypeToString[question.Qtype])
		msg.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(msg)
		return
	}

	// ========== 阶段二：排序完成后缓存命中 ==========
	// 优先检查排序缓存（排序完成后的结果）
	if sorted, ok := s.cache.GetSorted(domain, question.Qtype); ok {
		s.cache.RecordAccess(domain, question.Qtype)          // 记录访问
		s.prefetcher.RecordAccess(domain, uint32(sorted.TTL)) // Prefetcher Math Model Update
		currentStats.IncCacheHits()
		currentStats.RecordDomainQuery(domain) // ✅ 统计有效域名查询

		// [优化] Stale-While-Revalidate 模式
		// 使用 s.cfg.Ping.RttCacheTtlSeconds 作为 "新鲜度" 阈值
		// 如果上次排序时间在阈值内，说明数据还很新鲜，无需刷新，返回正常 TTL
		// 否则，说明数据稍微旧了，返回快速 TTL，并触发后台刷新

		elapsed := time.Since(sorted.Timestamp)
		isFresh := elapsed.Seconds() < float64(s.cfg.Ping.RttCacheTtlSeconds)

		var userTTL uint32

		if isFresh {
			// === 场景 1: 数据新鲜 ===
			// 计算剩余 TTL (复用原有的逻辑)
			remaining := int(sorted.TTL) - int(elapsed.Seconds())
			if remaining < 0 {
				remaining = 0
			}

			// 应用 UserReturnTTL 配置
			if currentCfg.Cache.UserReturnTTL > 0 {
				cycleOffset := int(elapsed.Seconds()) % currentCfg.Cache.UserReturnTTL
				cappedTTL := currentCfg.Cache.UserReturnTTL - cycleOffset
				if remaining < cappedTTL {
					userTTL = uint32(remaining)
				} else {
					userTTL = uint32(cappedTTL)
				}
			} else {
				userTTL = uint32(remaining)
			}

			logger.Debugf("[handleQuery] 排序缓存命中 (Fresh): %s (type=%s) -> %v (TTL=%d)",
				domain, dns.TypeToString[question.Qtype], sorted.IPs, userTTL)
		} else {
			// === 场景 2: 数据陈旧 (SWR) ===
			// 返回 FastResponseTTL 促使客户端尽快回来
			userTTL = uint32(currentCfg.Cache.FastResponseTTL)

			logger.Debugf("[handleQuery] 排序缓存命中 (Stale): %s (type=%s) -> %v (强制TTL=%d)",
				domain, dns.TypeToString[question.Qtype], sorted.IPs, userTTL)

			// 尝试触发后台刷新
			// 获取原始缓存以支持刷新逻辑
			raw, rawExists := s.cache.GetRaw(domain, question.Qtype)
			if rawExists && !raw.IsExpired() {
				go func() {
					sfKey := fmt.Sprintf("refresh:%s:%d", domain, question.Qtype)
					s.requestGroup.Do(sfKey, func() (interface{}, error) {
						// 双重检查防抖: 10秒内不重复刷新
						if latest, ok := s.cache.GetSorted(domain, question.Qtype); ok {
							if time.Since(latest.Timestamp) < 10*time.Second {
								return nil, nil
							}
						}
						s.sortIPsAsync(domain, question.Qtype, raw.IPs, raw.UpstreamTTL, raw.AcquisitionTime)
						return nil, nil
					})
				}()
			}
		}

		// 检查是否有 CNAME（从原始缓存获取）
		var cnames []string
		if raw, ok := s.cache.GetRaw(domain, question.Qtype); ok && len(raw.CNAMEs) > 0 {
			cnames = raw.CNAMEs
		}

		// 构造响应
		if len(cnames) > 0 {
			s.buildDNSResponseWithCNAME(msg, domain, cnames, sorted.IPs, question.Qtype, userTTL)
		} else {
			s.buildDNSResponse(msg, domain, sorted.IPs, question.Qtype, userTTL)
		}
		w.WriteMsg(msg)
		return
	}

	// ========== 阶段三:缓存过期后再次访问 ==========
	// 检查原始缓存(上游 DNS 响应缓存)
	if raw, ok := s.cache.GetRaw(domain, question.Qtype); ok {
		s.cache.RecordAccess(domain, question.Qtype)       // 记录访问
		s.prefetcher.RecordAccess(domain, raw.UpstreamTTL) // Prefetcher Math Model Update
		currentStats.IncCacheHits()
		currentStats.RecordDomainQuery(domain) // ✅ 统计有效域名查询
		logger.Debugf("[handleQuery] 原始缓存命中: %s (type=%s) -> %v, CNAMEs=%v (过期:%v)",
			domain, dns.TypeToString[question.Qtype], raw.IPs, raw.CNAMEs, raw.IsExpired())

		fastTTL := uint32(currentCfg.Cache.FastResponseTTL)

		// 使用历史数据进行兜底排序 (Fallback Rank)
		fallbackIPs := s.prefetcher.GetFallbackRank(domain, raw.IPs)

		if len(raw.CNAMEs) > 0 {
			s.buildDNSResponseWithCNAME(msg, domain, raw.CNAMEs, fallbackIPs, question.Qtype, fastTTL)
		} else {
			s.buildDNSResponse(msg, domain, fallbackIPs, question.Qtype, fastTTL)
		}
		w.WriteMsg(msg)

		if raw.IsExpired() {
			logger.Debugf("[handleQuery] 原始缓存已过期,触发异步刷新: %s (type=%s)",
				domain, dns.TypeToString[question.Qtype])
			task := RefreshTask{Domain: domain, Qtype: question.Qtype}
			s.refreshQueue.Submit(task)
		} else {
			go s.sortIPsAsync(domain, question.Qtype, raw.IPs, raw.UpstreamTTL, raw.AcquisitionTime)
		}
		return
	}

	currentStats.IncCacheMisses()

	// ========== IPv6 开关检查 ==========
	if question.Qtype == dns.TypeAAAA && !currentCfg.DNS.EnableIPv6 {
		logger.Debugf("[handleQuery] IPv6 已禁用，直接返回空响应: %s", domain)
		msg.SetRcode(r, dns.RcodeSuccess)
		msg.Answer = nil
		w.WriteMsg(msg)
		return
	}

	// ========== 阶段一：首次查询（无缓存）==========
	logger.Debugf("[handleQuery] 首次查询，无缓存: %s (type=%s)", domain, dns.TypeToString[question.Qtype])

	// 计算动态超时时间: timeout_ms × 健康服务器数
	// 这样可以确保每台服务器都有完整的超时时间进行尝试
	healthyServerCount := currentUpstream.GetHealthyServerCount()
	if healthyServerCount == 0 {
		// 如果所有服务器都不健康,至少给一次机会
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

	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	// 使用 singleflight 合并相同的并发请求
	// 这可以防止在高并发下对同一域名发起大量重复的上游查询，避免资源竞争和缓存覆盖问题
	sfKey := fmt.Sprintf("query:%s:%d", domain, question.Qtype)

	v, err, shared := s.requestGroup.Do(sfKey, func() (interface{}, error) {
		return currentUpstream.Query(ctx, domain, question.Qtype)
	})

	if shared {
		logger.Debugf("[handleQuery] 合并并发请求: %s (type=%s)", domain, dns.TypeToString[question.Qtype])
	}

	var result *upstream.QueryResultWithTTL
	if err == nil {
		result = v.(*upstream.QueryResultWithTTL)
	}

	var ips []string
	var cnames []string
	var upstreamTTL uint32 = uint32(currentCfg.Cache.MaxTTLSeconds)

	if err != nil {
		logger.Warnf("[handleQuery] 上游查询失败: %v", err)
		originalRcode := parseRcodeFromError(err)
		if originalRcode != dns.RcodeNameError {
			currentStats.IncUpstreamFailures()
		}

		if originalRcode == dns.RcodeNameError {
			s.cache.SetError(domain, question.Qtype, originalRcode, currentCfg.Cache.ErrorCacheTTL)
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

	if result != nil {
		ips = result.IPs
		cnames = result.CNAMEs
		upstreamTTL = result.TTL
	}

	// [AdBlock] CNAME 链路检查
	// 无论结果来自缓存还是上游，都需要确保整个 CNAME 链通过 AdBlock 检查
	if adblockMgr != nil && currentCfg.AdBlock.Enable && len(cnames) > 0 {
		for _, cnameToCheck := range cnames {
			cnameDomain := strings.TrimRight(cnameToCheck, ".")
			if blocked, rule := adblockMgr.CheckHost(cnameDomain); blocked {
				logger.Debugf("[AdBlock] CNAME Blocked: %s found in chain for %s (rule: %s)", cnameDomain, domain, rule)
				adblockMgr.RecordBlock(domain, rule) // 记录主域名被拦截

				// 写入拦截缓存 (针对主域名)
				s.cache.SetBlocked(domain, &cache.BlockedCacheEntry{
					BlockType: currentCfg.AdBlock.BlockMode,
					Rule:      rule,
					ExpiredAt: time.Now().Add(time.Duration(currentCfg.AdBlock.BlockedTTL) * time.Second),
				})

				// 返回拦截响应
				switch currentCfg.AdBlock.BlockMode {
				case "nxdomain":
					buildNXDomainResponse(w, r)
				case "zero_ip":
					buildZeroIPResponse(w, r, currentCfg.AdBlock.BlockedResponseIP, currentCfg.AdBlock.BlockedTTL)
				case "refuse":
					buildRefuseResponse(w, r)
				default:
					buildNXDomainResponse(w, r)
				}
				return
			}
		}
	}

	if len(ips) > 0 {
		currentStats.RecordDomainQuery(domain)
		logger.Debugf("[handleQuery] 上游查询完成: %s (type=%s) 获得 %d 个IP, CNAMEs=%v (TTL=%d秒): %v",
			domain, dns.TypeToString[question.Qtype], len(ips), cnames, upstreamTTL, ips)

		s.cache.SetRaw(domain, question.Qtype, ips, cnames, upstreamTTL)
		go s.sortIPsAsync(domain, question.Qtype, ips, upstreamTTL, time.Now())

		// [Fix] 若存在 CNAME，同时也缓存 CNAME 目标域名的结果
		// 这样可以确保 CNAME 链中的中间域名也被缓存，加速后续查询
		// TODO: 对于多级 CNAME，最好是能递归缓存。当前简单处理：将最后一个 CNAME 指向 IPs。
		if len(cnames) > 0 {
			// 通常最后一个 CNAME 是直接指向 IPs 的别名
			lastCNAME := cnames[len(cnames)-1]
			cnameTargetDomain := strings.TrimRight(dns.Fqdn(lastCNAME), ".")
			// 注意：这里我们只缓存最后一个 CNAME -> IPs 的关系。
			// 如果需要完整的链路缓存，需要更复杂的逻辑，但这能解决最常见的 "www.a.com -> cdn.a.com -> IP" 中 cdn.a.com 的缓存问题。
			s.cache.SetRaw(cnameTargetDomain, question.Qtype, ips, nil, upstreamTTL)
			go s.sortIPsAsync(cnameTargetDomain, question.Qtype, ips, upstreamTTL, time.Now())
		}

		// 使用历史数据进行兜底排序 (Fallback Rank)
		fallbackIPs := s.prefetcher.GetFallbackRank(domain, ips)

		fastTTL := uint32(currentCfg.Cache.FastResponseTTL)
		if len(cnames) > 0 {
			logger.Debugf("[handleQuery] 构造 CNAME 响应链: %s -> %v -> IPs", domain, cnames)
			s.buildDNSResponseWithCNAME(msg, domain, cnames, fallbackIPs, question.Qtype, fastTTL)
		} else {
			s.buildDNSResponse(msg, domain, fallbackIPs, question.Qtype, fastTTL)
		}
		w.WriteMsg(msg)
		return
	}

	if len(cnames) > 0 {
		// 只有 CNAME 没有 IP (可能是 CNAME 到另一个还没解析的域名，或者 CNAME loop，或者上游只返回了 CNAME)
		// 如果只有一个 CNAME 且没有 IP，我们尝试递归解析它（复用旧逻辑，但要注意多 CNAME 情况）
		// 如果上游返回了多个 CNAME，说明链条已经部分解析了？

		// 简单起见，如果上游返回了 CNAME 但没 IP，我们取出最后一个 CNAME 进行递归
		lastCNAME := cnames[len(cnames)-1]

		logger.Debugf("[handleQuery] 上游查询返回 CNAMEs=%v，开始递归解析最后一个: %s -> %s", cnames, domain, lastCNAME)

		finalResult, err := s.resolveCNAME(ctx, lastCNAME, question.Qtype)
		if err != nil {
			logger.Warnf("[handleQuery] CNAME 递归解析失败: %v", err)
			msg.SetRcode(r, dns.RcodeServerFailure)
			w.WriteMsg(msg)
			return
		}

		// 合并结果
		fullCNAMEs := append(cnames, finalResult.CNAMEs...)
		finalIPs := finalResult.IPs
		finalTTL := finalResult.TTL

		// Update Cache
		s.cache.SetRaw(domain, qtype, nil, cnames, upstreamTTL) // Cache partial chain? or full?
		// Actually, we should cache the full resolution for the original domain
		s.cache.SetRaw(domain, question.Qtype, finalIPs, fullCNAMEs, finalTTL)

		// Cache target
		cnameTargetDomain := strings.TrimRight(dns.Fqdn(lastCNAME), ".")
		s.cache.SetRaw(cnameTargetDomain, question.Qtype, finalIPs, finalResult.CNAMEs, finalTTL)

		fastTTL := uint32(currentCfg.Cache.FastResponseTTL)
		currentStats.RecordDomainQuery(domain)

		// Fallback Rank
		fallbackIPs := s.prefetcher.GetFallbackRank(cnameTargetDomain, finalIPs)

		s.buildDNSResponseWithCNAME(msg, domain, fullCNAMEs, fallbackIPs, question.Qtype, fastTTL)
		w.WriteMsg(msg)

		go s.sortIPsAsync(cnameTargetDomain, question.Qtype, finalIPs, finalTTL, time.Now())
		return
	}

	logger.Debugf("[handleQuery] 上游查询返回空结果 (NODATA): %s", domain)
	msg.SetRcode(r, dns.RcodeSuccess)
	msg.Answer = nil
	w.WriteMsg(msg)
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

// resolveCNAME 递归解析 CNAME，直到找到 IP 地址
func (s *Server) resolveCNAME(ctx context.Context, domain string, qtype uint16) (*upstream.QueryResultWithTTL, error) {
	const maxRedirects = 10
	currentDomain := domain

	for i := 0; i < maxRedirects; i++ {
		logger.Debugf("[resolveCNAME] 递归查询 #%d: %s (type=%s)", i+1, currentDomain, dns.TypeToString[qtype])

		// 检查上下文是否已取消
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// 去掉末尾的点, 以符合内部查询习惯
		queryDomain := strings.TrimRight(currentDomain, ".")

		result, err := s.upstream.Query(ctx, queryDomain, qtype)
		if err != nil {
			return nil, fmt.Errorf("cname resolution failed for %s: %v", queryDomain, err)
		}

		// 如果找到了 IP，解析结束
		if len(result.IPs) > 0 {
			logger.Debugf("[resolveCNAME] 成功解析到 IP: %v for domain %s", result.IPs, queryDomain)
			// CNAME链的最终结果的CNAME字段应为空
			result.CNAMEs = nil
			return result, nil
		}

		// 如果没有 IP 但有 CNAME，继续重定向
		if len(result.CNAMEs) > 0 {
			lastCNAME := result.CNAMEs[len(result.CNAMEs)-1]
			logger.Debugf("[resolveCNAME] 发现下一跳 CNAME: %s -> %s", queryDomain, lastCNAME)
			currentDomain = lastCNAME
			continue
		}

		// 如果既没有 IP 也没有 CNAME，说明解析中断
		return nil, fmt.Errorf("cname resolution failed: no IPs or further CNAME found for %s", queryDomain)
	}

	return nil, fmt.Errorf("cname resolution failed: exceeded max redirects for %s", domain)
}

// buildDNSResponse 构造 DNS 响应
func (s *Server) buildDNSResponse(msg *dns.Msg, domain string, ips []string, qtype uint16, ttl uint32) {
	fqdn := dns.Fqdn(domain)
	logger.Debugf("[buildDNSResponse] 构造响应: %s (type=%s) 包含 %d 个IP, TTL=%d",
		domain, dns.TypeToString[qtype], len(ips), ttl)

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
	if len(cnames) == 0 {
		return
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
