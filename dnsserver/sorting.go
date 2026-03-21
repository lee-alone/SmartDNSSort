package dnsserver

import (
	"context"
	"fmt"
	"net"
	"smartdnssort/cache"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"sort"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// performPingSort 执行 ping 排序操作
// 第三阶段改造：从 IPPool 实时获取 RTT 数据进行排序，实现"解耦"
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

	// 第三阶段改造：优先从 IPPool 获取 RTT 数据（真理化改造）
	// 这样可以避免每次都进行实时探测，提高响应速度
	ipPool := pinger.GetIPPool()
	if ipPool != nil {
		// 从 IPPool 批量获取 RTT 数据
		rttMap := ipPool.GetAllIPRTTs(ips)

		// 如果所有 IP 都有 RTT 数据，直接使用 IPPool 的数据进行排序
		if len(rttMap) == len(ips) {
			logger.Debugf("[performPingSort] 使用 IPPool RTT 数据进行排序: %s", domain)
			return s.sortIPsByRTT(ips, rttMap, sortDomain)
		}

		// 部分或全部 IP 没有 RTT 数据，需要探测
		// 对于新 IP（IPPool 中没有的），不等待测速，直接返回
		// 将新 IP 扔进异步测速队列
		var newIPs []string
		for _, ip := range ips {
			if _, exists := rttMap[ip]; !exists {
				newIPs = append(newIPs, ip)
			}
		}

		if len(newIPs) > 0 {
			logger.Debugf("[performPingSort] 发现 %d 个新 IP，将进行异步测速: %v", len(newIPs), newIPs)
			// 异步测速新 IP
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.Ping.TimeoutMs)*time.Millisecond)
				defer cancel()
				pinger.PingAndSort(ctx, newIPs, sortDomain)
			}()
		}

		// 如果至少有一个 IP 有 RTT 数据，使用现有数据排序
		if len(rttMap) > 0 {
			logger.Debugf("[performPingSort] 使用部分 IPPool RTT 数据进行排序: %s", domain)
			return s.sortIPsByRTT(ips, rttMap, sortDomain)
		}
	}

	// 兜底方案：使用现有的 Pinger 进行 ping 测试和排序
	// We use sortDomain here so that stats are keyed by the canonical name
	pingResults := pinger.PingAndSort(ctx, ips, sortDomain)

	if len(pingResults) == 0 {
		// 断网且无缓存时，返回原始 IP 列表（尽力而为）
		// 这样系统能够继续提供有限的解析服务，而不是返回 SERVFAIL
		logger.Debugf("[performPingSort] 断网且无缓存，返回原始 IP 列表: %s", domain)
		return ips, nil, nil
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

// sortIPsByRTT 根据 RTT 数据对 IP 进行排序
// 第三阶段新增：使用 IPPool 中的 RTT 数据进行排序
func (s *Server) sortIPsByRTT(ips []string, rttMap map[string]int, domain string) ([]string, []int, error) {
	// 创建 IP-RTT 对列表
	type ipRTT struct {
		ip  string
		rtt int
	}

	ipRTTs := make([]ipRTT, 0, len(ips))
	for _, ip := range ips {
		rtt := ping.LogicDeadRTT // 默认值，表示不可达
		if r, exists := rttMap[ip]; exists {
			rtt = r
		}
		ipRTTs = append(ipRTTs, ipRTT{ip: ip, rtt: rtt})
	}

	// 按 RTT 从小到大排序
	// 选择 sort.Slice 因为其底层实现针对中等规模数组（DNS 响应 IP 数量通常 < 100）
	// 比冒泡排序 O(n²) 性能更优 (O(n log n))
	sort.Slice(ipRTTs, func(i, j int) bool {
		// 首先按 RTT 从小到大排序
		if ipRTTs[i].rtt != ipRTTs[j].rtt {
			return ipRTTs[i].rtt < ipRTTs[j].rtt
		}
		// RTT 相等时，按 IP 字符串字典序排序（保证排序稳定性，便于测试和结果一致性）
		return ipRTTs[i].ip < ipRTTs[j].ip
	})

	// 提取排序后的 IP 和 RTT
	sortedIPs := make([]string, len(ipRTTs))
	rtts := make([]int, len(ipRTTs))
	for i, ipRtt := range ipRTTs {
		sortedIPs[i] = ipRtt.ip
		rtts[i] = ipRtt.rtt
		if ipRtt.rtt < ping.LogicDeadRTT {
			s.stats.IncPingSuccesses()
		}
	}

	logger.Debugf("[sortIPsByRTT] 排序完成: %s -> %v (RTT: %v)", domain, sortedIPs, rtts)
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
		logger.Warnf("[sortIPsAsync] 并发排序任务已达上限 (%d)，跳过排序: %s (type=%s)",
			MaxConcurrentSorts, domain, dns.TypeToString[qtype])
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
