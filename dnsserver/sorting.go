package dnsserver

import (
	"context"
	"fmt"
	"smartdnssort/cache"
	"smartdnssort/logger"
	"time"

	"github.com/miekg/dns"
)

// performPingSort 执行 ping 排序操作
func (s *Server) performPingSort(ctx context.Context, domain string, ips []string) ([]string, []int, error) {
	logger.Debugf("[performPingSort] 对 %d 个 IP 进行 ping 排序", len(ips))

	s.mu.RLock()
	pinger := s.pinger
	s.mu.RUnlock()

	// 使用现有的 Pinger 进行 ping 测试和排序
	pingResults := pinger.PingAndSort(ctx, ips)

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
	s.prefetcher.ReportPingResultWithDomain(domain, pingResults)

	return sortedIPs, rtts, nil
}

// calculateRemainingTTL 计算剩余 TTL
// 基于上游 TTL 和获取时间，减去已过去的时间，并应用 min/max 限制
// 特殊语义：
//   - min 和 max 都为 0: 不修改上游 TTL
//   - 仅 min 为 0: 只限制最大值
//   - 仅 max 为 0: 只限制最小值
func (s *Server) calculateRemainingTTL(upstreamTTL uint32, acquisitionTime time.Time) int {
	elapsed := time.Since(acquisitionTime).Seconds()
	remaining := int(upstreamTTL) - int(elapsed)

	minTTL := s.cfg.Cache.MinTTLSeconds
	maxTTL := s.cfg.Cache.MaxTTLSeconds

	// 如果 min 和 max 都为 0，不修改上游 TTL
	if minTTL == 0 && maxTTL == 0 {
		return remaining
	}

	// 应用最小值限制（如果 min > 0）
	if minTTL > 0 && remaining < minTTL {
		remaining = minTTL
	}

	// 应用最大值限制（如果 max > 0）
	if maxTTL > 0 && remaining > maxTTL {
		remaining = maxTTL
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
