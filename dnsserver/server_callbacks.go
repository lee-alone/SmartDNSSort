package dnsserver

import (
	"time"

	"smartdnssort/logger"
	"smartdnssort/upstream"

	"github.com/miekg/dns"
)

// setupUpstreamCallback 设置上游管理器的缓存更新回调
func (s *Server) setupUpstreamCallback(u *upstream.Manager) {
	u.SetCacheUpdateCallback(func(domain string, qtype uint16, records []dns.RR, cnames []string, ttl uint32) {
		logger.Debugf("[CacheUpdateCallback] 更新缓存: %s (type=%s), 记录数量=%d, CNAMEs=%v, TTL=%d秒",
			domain, dns.TypeToString[qtype], len(records), cnames, ttl)

		// 获取当前原始缓存中的 IP 数量
		var oldIPCount int
		if oldEntry, exists := s.cache.GetRaw(domain, qtype); exists {
			oldIPCount = len(oldEntry.IPs)
		}

		// 更新原始缓存中的记录列表
		// 注意：SetRawRecords 会自动从 records 中派生 IPs
		s.cache.SetRawRecords(domain, qtype, records, cnames, ttl)

		// 获取新的 IP 数量
		var newIPCount int
		if newEntry, exists := s.cache.GetRaw(domain, qtype); exists {
			newIPCount = len(newEntry.IPs)
		}

		// 如果后台收集的 IP 数量比之前多，需要重新排序
		if newIPCount > oldIPCount && qtype == dns.TypeA || qtype == dns.TypeAAAA {
			logger.Debugf("[CacheUpdateCallback] 后台收集到更多IP (%d -> %d)，清除旧排序状态并重新排序",
				oldIPCount, newIPCount)

			// 清除旧的排序状态，允许重新排序
			s.cache.CancelSort(domain, qtype)

			// 获取新的 IPs 用于排序
			if newEntry, exists := s.cache.GetRaw(domain, qtype); exists {
				// 触发异步排序，更新排序缓存
				go s.sortIPsAsync(domain, qtype, newEntry.IPs, ttl, time.Now())
			}
		} else {
			logger.Debugf("[CacheUpdateCallback] IP数量未增加 (%d)，保持现有排序", newIPCount)
		}
	})
}
