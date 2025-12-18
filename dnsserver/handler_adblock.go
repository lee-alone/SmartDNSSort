package dnsserver

import (
	"smartdnssort/adblock"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// handleAdBlockCheck 执行 AdBlock 过滤检查
// 返回 true 表示请求已处理
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
			buildNXDomainResponse(w, r, s.msgPool)
		case "zero_ip":
			buildZeroIPResponse(w, r, cfg.AdBlock.BlockedResponseIP, cfg.AdBlock.BlockedTTL, s.msgPool)
		case "refuse":
			buildRefuseResponse(w, r, s.msgPool)
		default:
			buildNXDomainResponse(w, r, s.msgPool)
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
			buildNXDomainResponse(w, r, s.msgPool)
		case "zero_ip":
			buildZeroIPResponse(w, r, cfg.AdBlock.BlockedResponseIP, cfg.AdBlock.BlockedTTL, s.msgPool)
		case "refuse":
			buildRefuseResponse(w, r, s.msgPool)
		default:
			buildNXDomainResponse(w, r, s.msgPool)
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
				buildNXDomainResponse(w, r, s.msgPool)
			case "zero_ip":
				buildZeroIPResponse(w, r, cfg.AdBlock.BlockedResponseIP, cfg.AdBlock.BlockedTTL, s.msgPool)
			case "refuse":
				buildRefuseResponse(w, r, s.msgPool)
			default:
				buildNXDomainResponse(w, r, s.msgPool)
			}
			return true
		}
	}
	return false
}
