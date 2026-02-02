package dnsserver

import (
	"context"
	"fmt"
	"net"
	"smartdnssort/cache"
	"smartdnssort/logger"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// performPingSort 执行 ping 排序操作
func (s *Server) performPingSort(ctx context.Context, domain string, ips []string) ([]string, []int, error) {
	// Add the check for Ping.Enabled
	if !s.cfg.Ping.Enabled {
		logger.Debugf("[performPingSort] Ping 功能已禁用，直接返回原始 IP 列表: %s, IP数量=%d", domain, len(ips))
		// If ping is disabled, return the original IPs without sorting or RTTs.
		// RTTs will be nil, which calling functions should handle (e.g., using 0 or ignoring).
		// No error is returned as this is an intended bypass.
		return ips, nil, nil
	}

	logger.Debugf("[performPingSort] 对 %d 个 IP 进行 ping 排序", len(ips))

	// Determine the domain name to use for sorting stats (handle CNAMEs)
	// If the domain has CNAMEs, we want to use the canonical name (target) for stats and sorting.
	// This ensures that 'img1.mydrivers.com' shares the same blacklist/stats as 'img1.mydrivers.com.ctdns.cn'.
	sortDomain := domain
	if len(ips) > 0 {
		var qtype uint16 = dns.TypeA
		if net.ParseIP(ips[0]).To4() == nil {
			qtype = dns.TypeAAAA
		}

		if raw, exists := s.cache.GetRaw(domain, qtype); exists && len(raw.CNAMEs) > 0 {
			lastCname := raw.CNAMEs[len(raw.CNAMEs)-1]
			cnameTarget := strings.TrimRight(lastCname, ".")
			if cnameTarget != domain {
				logger.Debugf("[performPingSort] Using CNAME target %s for sorting stats of %s", cnameTarget, domain)
				sortDomain = cnameTarget
			}
		}
	}

	s.mu.RLock()
	pinger := s.pinger
	s.mu.RUnlock()

	// 使用现有的 Pinger 进行 ping 测试和排序
	// We use sortDomain here so that stats are keyed by the canonical name
	pingResults := pinger.PingAndSort(ctx, ips, sortDomain)

	if len(pingResults) == 0 {
		return nil, nil, fmt.Errorf("ping sort returned no results")
	}

	// 提取排序后的 IP 和 RTT
	var sortedIPs []string
	var rtts []int
	for _, result := range pingResults {
		sortedIPs = append(sortedIPs, result.IP)
		rtts = append(rtts, result.RTT)
		s.stats.IncPingSuccesses()
	}

	// Report results to prefetcher for blacklist/stat updates
	// We also report against sortDomain to centralize the knowledge base
	s.prefetcher.ReportPingResultWithDomain(sortDomain, pingResults)

	return sortedIPs, rtts, nil
}

// calculateRemainingTTL 计算基于本地策略后的剩余生存时间
func (s *Server) calculateRemainingTTL(upstreamTTL uint32, acquisitionTime time.Time) int {
	elapsed := int(time.Since(acquisitionTime).Seconds())

	// 1. 首先基于上游 TTL 和本地配置，计算该记录在本地的总生存期 (Effective TTL)
	effTTL := upstreamTTL
	minTTL := uint32(s.cfg.Cache.MinTTLSeconds)
	maxTTL := uint32(s.cfg.Cache.MaxTTLSeconds)

	if minTTL > 0 && effTTL < minTTL {
		effTTL = minTTL
	}
	if maxTTL > 0 && effTTL > maxTTL {
		effTTL = maxTTL
	}

	// 2. 然后减去已经过去的时间，得到剩下的生存时间
	remaining := int(effTTL) - elapsed

	// 确保不返回负数
	if remaining < 0 {
		return 0
	}
	return remaining
}

// sortIPsAsync 异步排序 IP 地址
// 排序完成后会更新排序缓存
func (s *Server) sortIPsAsync(domain string, qtype uint16, ips []string, upstreamTTL uint32, acquisitionTime time.Time) {
	// 检查是否已有排序任务在进行
	state, isNew := s.cache.GetOrStartSort(domain, qtype)
	if !isNew {
		logger.Debugf("[sortIPsAsync] 排序任务已在进行: %s (type=%s)，跳过重复排序",
			domain, dns.TypeToString[qtype])
		return
	}

	// 优化：如果只有一个IP，则无需排序
	if len(ips) == 1 {
		logger.Debugf("[sortIPsAsync] 只有一个IP，跳过排序: %s (type=%s) -> %s",
			domain, dns.TypeToString[qtype], ips[0])

		// 直接创建排序结果
		result := &cache.SortedCacheEntry{
			IPs:       ips,
			RTTs:      []int{0}, // RTT 为 0，因为没有测试
			Timestamp: time.Now(),
			TTL:       int(upstreamTTL),
			IsValid:   true,
		}

		// 直接调用回调函数处理排序完成的逻辑
		s.handleSortComplete(domain, qtype, result, nil, state)
		return
	}

	logger.Debugf("[sortIPsAsync] 启动异步排序任务: %s (type=%s), IP数量=%d",
		domain, dns.TypeToString[qtype], len(ips))

	// 尝试获取信号量（限制并发排序任务）
	select {
	case s.sortSemaphore <- struct{}{}:
		// 成功获取信号量，启动排序 goroutine
		go func() {
			defer func() { <-s.sortSemaphore }() // 释放信号量

			// 创建排序任务
			task := &cache.SortTask{
				Domain: domain,
				Qtype:  qtype,
				IPs:    ips,
				TTL:    uint32(s.calculateRemainingTTL(upstreamTTL, acquisitionTime)),
				Callback: func(result *cache.SortedCacheEntry, err error) {
					s.handleSortComplete(domain, qtype, result, err, state)
				},
			}

			// 提交到排序队列
			// 如果队列已满，回退到同步排序（立即执行）
			if !s.sortQueue.Submit(task) {
				logger.Warnf("[sortIPsAsync] 排序队列已满，改用同步排序: %s (type=%s)",
					domain, dns.TypeToString[qtype])
				task.Callback(nil, fmt.Errorf("sort queue full"))
			}
		}()
	default:
		// 信号量已满，跳过此次排序
		logger.Warnf("[sortIPsAsync] 并发排序任务已达上限 (50)，跳过排序: %s (type=%s)",
			domain, dns.TypeToString[qtype])
		s.cache.FinishSort(domain, qtype, nil, fmt.Errorf("sort semaphore full"), state)
	}
}

// handleSortComplete 处理排序完成事件
func (s *Server) handleSortComplete(domain string, qtype uint16, result *cache.SortedCacheEntry, err error, state *cache.SortingState) {
	if err != nil {
		logger.Warnf("[handleSortComplete] 排序失败: %s (type=%s), 错误: %v",
			domain, dns.TypeToString[qtype], err)
		s.cache.FinishSort(domain, qtype, nil, err, state)
		return
	}

	if result == nil {
		logger.Warnf("[handleSortComplete] 排序结果为空: %s (type=%s)",
			domain, dns.TypeToString[qtype])
		s.cache.FinishSort(domain, qtype, nil, fmt.Errorf("sort result is nil"), state)
		return
	}

	logger.Debugf("[handleSortComplete] 排序完成: %s (type=%s) -> %v (RTT: %v)",
		domain, dns.TypeToString[qtype], result.IPs, result.RTTs)

	// 从原始缓存获取获取时间，计算剩余 TTL
	raw, exists := s.cache.GetRaw(domain, qtype)
	if exists && raw != nil {
		result.TTL = s.calculateRemainingTTL(raw.UpstreamTTL, raw.AcquisitionTime)
	} else {
		// 如果原始缓存不存在（极少发生），使用最小 TTL 作为兜底
		result.TTL = s.cfg.Cache.MinTTLSeconds
	}

	// 缓存排序结果
	s.cache.SetSorted(domain, qtype, result)

	// 完成排序任务
	s.cache.FinishSort(domain, qtype, result, nil, state)
}
