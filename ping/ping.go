package ping

import (
	"context"
	"time"
)

// PingAndSort 执行并发 ping 测试并返回排序后的结果
// 纯 ICMP 探测模式：只使用 ICMP echo request/reply 测试 IP 可达性
// 支持缓存、并发控制和智能排序
//
// 优化路径：
// - 首选路径：直接读取 IPMonitor 维护的 RTT 缓存数据
// - 兜底方案：当缓存不存在或过期时，才触发实时 ICMP 探测
// - 这样可以显著降低用户请求触发的探测频率，减少 ICMP 流量
func (p *Pinger) PingAndSort(ctx context.Context, ips []string, domain string) []Result {
	if len(ips) == 0 {
		return nil
	}

	// 熔断：断网时只返回缓存数据，不进行实际探测
	// 这样可以避免无效的 ICMP/TCP 探测，减少 CPU 和 IO 开销
	if p.healthChecker != nil && !p.healthChecker.IsNetworkHealthy() {
		// 尝试从缓存获取数据
		if p.rttCacheTtlSeconds > 0 {
			cached := make([]Result, 0, len(ips))
			for _, ip := range ips {
				if e, ok := p.rttCache.get(ip); ok {
					rttToUse := e.rtt
					if p.ipPool != nil {
						if _, poolRTTEWMA, updated := p.ipPool.GetIPRTT(ip); updated {
							rttToUse = poolRTTEWMA
						}
					}
					cached = append(cached, Result{IP: ip, RTT: rttToUse, Loss: e.loss, ProbeMethod: "cached-offline"})
				}
			}
			if len(cached) > 0 {
				p.sortResults(cached)
				return cached
			}
		}
		// 无缓存数据，返回空结果
		return nil
	}

	// 智能探测
	testIPs := ips
	if p.maxTestIPs > 0 && len(ips) > p.maxTestIPs {
		testIPs = ips[:p.maxTestIPs]
	}

	var toPing []string
	var cached []Result

	// 预分配容量，避免多次扩容
	cached = make([]Result, 0, len(testIPs))
	toPing = make([]string, 0, len(testIPs))

	// 第三阶段优化：优先使用 IPMonitor 维护的 RTT 数据
	// 缓存检查逻辑保持不变，但增加了对 IPMonitor 的依赖
	if p.rttCacheTtlSeconds > 0 {
		now := time.Now() // 在循环外调用一次，避免重复系统调用
		for _, ip := range testIPs {
			if e, ok := p.rttCache.get(ip); ok {
				if now.Before(e.staleAt) {
					// 缓存未过期（Fresh）：直接返回
					// 这是首选路径：IPMonitor 已经维护好了 RTT 数据
					rttToUse := e.rtt
					// 优化：优先从 IPPool 获取经过 EWMA 平滑后的 RTT
					// 防止网络抖动导致的瞬时值误导排序
					if p.ipPool != nil {
						if _, poolRTTEWMA, updated := p.ipPool.GetIPRTT(ip); updated {
							// 使用 IPPool 中的 EWMA 平滑值，提供更稳定的排序依据
							rttToUse = poolRTTEWMA
						}
					}
					cached = append(cached, Result{IP: ip, RTT: rttToUse, Loss: e.loss, ProbeMethod: "cached"})
					p.RecordIPSuccess(ip)
				} else if now.Before(e.expiresAt) {
					// 缓存处于软过期期间（Stale）：返回旧数据，异步更新
					// IPMonitor 可能正在刷新，返回旧数据即可
					rttToUse := e.rtt
					// 优化：优先从 IPPool 获取经过 EWMA 平滑后的 RTT
					if p.ipPool != nil {
						if _, poolRTTEWMA, updated := p.ipPool.GetIPRTT(ip); updated {
							rttToUse = poolRTTEWMA
						}
					}
					cached = append(cached, Result{IP: ip, RTT: rttToUse, Loss: e.loss, ProbeMethod: "stale"})
					p.RecordIPSuccess(ip)
					// 异步触发更新（兜底方案，IPMonitor 可能已经在更新）
					p.triggerStaleRevalidate(ip, domain)
				} else {
					// 缓存完全过期（Expired）：需要重新探测
					// 这是兜底方案：IPMonitor 未能及时刷新，用户请求触发探测
					toPing = append(toPing, ip)
				}
			} else {
				// 无缓存：需要探测
				// 这是兜底方案：新 IP 或缓存被清理
				toPing = append(toPing, ip)
			}
		}
	} else {
		toPing = testIPs
	}

	// 并发测（兜底方案）
	// 只有当缓存不可用时才会执行这里
	results := p.concurrentPing(ctx, toPing, domain)

	// 记录失效权重（避免两重记录）
	for _, r := range results {
		if r.FastFail {
			// 已经在 pingIP 中通过 RecordIPFastFail 记录过了，跳过以避免重复
			continue
		}
		if r.Loss == 100 {
			p.RecordIPFailure(r.IP)
		} else {
			p.RecordIPSuccess(r.IP)
		}
	}

	// 更新缓存（缓存所有结果，包括失败）
	if p.rttCacheTtlSeconds > 0 {
		for _, r := range results {
			ttl := p.calculateDynamicTTL(r)
			staleAt := time.Now().Add(ttl)

			// 软过期容忍期（Grace Period）
			gracePeriod := p.staleGracePeriod
			if gracePeriod == 0 {
				gracePeriod = 30 * time.Second
			}
			// 确保容忍期不会超过 TTL 的 50%，避免陈旧数据存在太久
			if gracePeriod > ttl/2 {
				gracePeriod = ttl / 2
			}
			expiresAt := staleAt.Add(gracePeriod)

			p.rttCache.set(r.IP, &rttCacheEntry{
				rtt:       r.RTT,
				loss:      r.Loss,
				staleAt:   staleAt,
				expiresAt: expiresAt,
			})
		}
	}

	// 合并 + 排序
	all := append(cached, results...)
	p.sortResults(all)
	return all
}

// Stop 停止 Pinger 的后台任务
func (p *Pinger) Stop() {
	close(p.stopChan)
	p.SaveIPFailureWeights()
}

// GetIPPool 获取全局 IP 资源管理器
func (p *Pinger) GetIPPool() *IPPool {
	return p.ipPool
}
