package dnsserver

import (
	"time"

	"smartdnssort/logger"
	"smartdnssort/upstream"

	"github.com/miekg/dns"
)

// setupUpstreamCallback 设置上游管理器的缓存更新回调
func (s *Server) setupUpstreamCallback(u *upstream.Manager) {
	u.SetCacheUpdateCallback(func(domain string, qtype uint16, records []dns.RR, cnames []string, ttl uint32, queryVersion int64) {
		logger.Debugf("[CacheUpdateCallback] 后台补全完成: %s (type=%s), 记录数量=%d, CNAMEs=%v, TTL=%d秒, version=%d",
			domain, dns.TypeToString[qtype], len(records), cnames, ttl, queryVersion)

		// 获取当前原始缓存中的 IP 信息和版本号
		var oldIPs []string
		var currentVersion int64
		if oldEntry, exists := s.cache.GetRaw(domain, qtype); exists {
			oldIPs = oldEntry.IPs
			currentVersion = oldEntry.QueryVersion
		}

		// ========== 关键修复：版本号检查 ==========
		// 只有更新的版本号才能更新缓存
		// 这防止了旧的后台补全覆盖新的缓存
		if queryVersion < currentVersion {
			logger.Debugf("[CacheUpdateCallback] ⏭️  跳过过期的查询结果: %s (version=%d, current=%d)",
				domain, queryVersion, currentVersion)
			return
		}

		// 从新记录中提取 IP（与 SetRawRecords 逻辑一致）
		newIPSet := make(map[string]bool)
		var newIPs []string
		for _, r := range records {
			switch rec := r.(type) {
			case *dns.A:
				ipStr := rec.A.String()
				if !newIPSet[ipStr] {
					newIPSet[ipStr] = true
					newIPs = append(newIPs, ipStr)
				}
			case *dns.AAAA:
				ipStr := rec.AAAA.String()
				if !newIPSet[ipStr] {
					newIPSet[ipStr] = true
					newIPs = append(newIPs, ipStr)
				}
			}
		}

		// ========== IP池变化检测 ==========
		// 检测是否存在"实质性"的IP池变化（优化为 O(n) 算法）
		// 预先构建集合，避免循环内重复创建
		oldIPSet := make(map[string]bool, len(oldIPs))
		for _, ip := range oldIPs {
			oldIPSet[ip] = true
		}

		newIPSet = make(map[string]bool, len(newIPs))
		for _, ip := range newIPs {
			newIPSet[ip] = true
		}

		// 检测是否有新增 IP
		hasNewIPs := false
		for ip := range newIPSet {
			if !oldIPSet[ip] {
				hasNewIPs = true
				break
			}
		}

		// 检测是否有删除 IP
		hasRemovedIPs := false
		for ip := range oldIPSet {
			if !newIPSet[ip] {
				hasRemovedIPs = true
				break
			}
		}

		oldIPCount := len(oldIPs)
		newIPCount := len(newIPs)

		// 记录IP变化信息
		if oldIPCount > 0 {
			logger.Debugf("[CacheUpdateCallback] IP池分析: 旧=%d, 新=%d, 新增=%v, 删除=%v",
				oldIPCount, newIPCount, hasNewIPs, hasRemovedIPs)
		}

		// ========== 决策：是否更新缓存 ==========
		shouldUpdate := false
		reason := ""

		if oldIPCount == 0 {
			shouldUpdate = true
			reason = "首次查询"
		} else if hasNewIPs {
			shouldUpdate = true
			reason = "发现新增IP"
		} else if hasRemovedIPs {
			shouldUpdate = true
			reason = "检测到IP删除"
		} else if newIPCount > oldIPCount && float64(newIPCount-oldIPCount)/float64(oldIPCount) > 0.5 {
			shouldUpdate = true
			reason = "IP数量显著增加(>50%)"
		}

		if !shouldUpdate {
			logger.Debugf("[CacheUpdateCallback] ⏭️  跳过缓存更新: %s (原因: IP池无实质性变化, 保持现有排序)",
				domain)
			return
		}

		logger.Debugf("[CacheUpdateCallback] ✅ 更新缓存: %s (原因: %s, version=%d)", domain, reason, queryVersion)

		// 更新原始缓存中的记录列表，带版本号
		s.cache.SetRawRecordsWithVersion(domain, qtype, records, cnames, ttl, queryVersion)

		// 如果是A/AAAA记录且IP池有变化，需要重新排序
		if (qtype == dns.TypeA || qtype == dns.TypeAAAA) && (hasNewIPs || hasRemovedIPs) {
			logger.Debugf("[CacheUpdateCallback] 🔄 IP池变化，清除旧排序状态并重新排序: %s",
				domain)

			// 清除旧的排序状态，允许重新排序
			s.cache.CancelSort(domain, qtype)

			// 获取新的 IPs 用于排序
			if newEntry, exists := s.cache.GetRaw(domain, qtype); exists {
				// 触发异步排序，更新排序缓存
				go s.sortIPsAsync(domain, qtype, newEntry.IPs, ttl, time.Now())
			}
		}
	})
}
