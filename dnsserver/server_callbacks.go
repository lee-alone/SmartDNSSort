package dnsserver

import (
	"time"

	"smartdnssort/logger"
	"smartdnssort/upstream"

	"github.com/miekg/dns"
)

// setupUpstreamCallback 设置上游管理器的缓存更新回调
func (s *Server) setupUpstreamCallback(u *upstream.Manager) {
	u.SetCacheUpdateCallback(func(domain string, qtype uint16, ips []string, cnames []string, ttl uint32) {
		logger.Debugf("[CacheUpdateCallback] 更新缓存: %s (type=%s), IP数量=%d, CNAMEs=%v, TTL=%d秒",
			domain, dns.TypeToString[qtype], len(ips), cnames, ttl)

		// 获取当前原始缓存中的 IP 数量
		var oldIPCount int
		if oldEntry, exists := s.cache.GetRaw(domain, qtype); exists {
			oldIPCount = len(oldEntry.IPs)
		}

		// 更新原始缓存中的IP列表
		// 注意：这里使用 time.Now() 作为获取时间，因为这是后台收集完成的时间
		s.cache.SetRaw(domain, qtype, ips, cnames, ttl)

		// 如果后台收集的 IP 数量比之前多，需要重新排序
		if len(ips) > oldIPCount {
			logger.Debugf("[CacheUpdateCallback] 后台收集到更多IP (%d -> %d)，清除旧排序状态并重新排序",
				oldIPCount, len(ips))

			// 清除旧的排序状态，允许重新排序
			s.cache.CancelSort(domain, qtype)

			// 触发异步排序，更新排序缓存
			go s.sortIPsAsync(domain, qtype, ips, ttl, time.Now())
		} else {
			logger.Debugf("[CacheUpdateCallback] IP数量未增加 (%d)，保持现有排序", len(ips))
		}
	})
}
