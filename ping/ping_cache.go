package ping

import (
	"context"
	"smartdnssort/logger"
	"time"
)

// GetIPRTT 从缓存中获取 IP 的 RTT 数据
// 第三阶段优化：提供只读访问 RTT 缓存的能力
// 返回值：
// - rtt: RTT 值（毫秒），-1 表示不可达
// - loss: 丢包率
// - exists: 缓存是否存在
// - isStale: 缓存是否处于软过期状态
func (p *Pinger) GetIPRTT(ip string) (rtt int, loss float64, exists bool, isStale bool) {
	if p.rttCacheTtlSeconds <= 0 {
		return -1, 0, false, false
	}

	entry, ok := p.rttCache.get(ip)
	if !ok {
		return -1, 0, false, false
	}

	now := time.Now()
	isStale = now.After(entry.staleAt) && now.Before(entry.expiresAt)

	return entry.rtt, entry.loss, true, isStale
}

// GetCacheTTLRemaining 获取 IP 缓存的剩余 TTL（毫秒）
// 用于探测冷却时间判断：如果剩余 TTL 足够长，可以跳过本次探测
// 返回值：
// - remainingMs: 剩余 TTL（毫秒），-1 表示缓存不存在或已过期
// - isFresh: 缓存是否处于新鲜状态（未到 staleAt）
func (p *Pinger) GetCacheTTLRemaining(ip string) (remainingMs int64, isFresh bool) {
	if p.rttCacheTtlSeconds <= 0 {
		return -1, false
	}

	entry, ok := p.rttCache.get(ip)
	if !ok {
		return -1, false
	}

	now := time.Now()

	// 检查是否已完全过期
	if now.After(entry.expiresAt) {
		return -1, false
	}

	// 计算到 staleAt 的剩余时间
	remaining := entry.staleAt.Sub(now)
	if remaining < 0 {
		remaining = 0
	}

	// 判断是否新鲜
	isFresh = now.Before(entry.staleAt)

	return remaining.Milliseconds(), isFresh
}

// GetMultipleIPRTTs 批量获取多个 IP 的 RTT 数据
// 第三阶段优化：用于批量查询，减少锁竞争
// 返回值：map[ip]Result，只包含缓存中存在的 IP
func (p *Pinger) GetMultipleIPRTTs(ips []string) map[string]Result {
	result := make(map[string]Result)
	if p.rttCacheTtlSeconds <= 0 {
		return result
	}

	for _, ip := range ips {
		entry, ok := p.rttCache.get(ip)
		if !ok {
			continue
		}

		now := time.Now()
		// 只返回未完全过期的缓存
		if now.Before(entry.expiresAt) {
			probeMethod := "cached"
			if now.After(entry.staleAt) {
				probeMethod = "stale"
			}
			result[ip] = Result{
				IP:          ip,
				RTT:         entry.rtt,
				Loss:        entry.loss,
				ProbeMethod: probeMethod,
			}
		}
	}

	return result
}

// UpdateIPCache 更新 IP 的 RTT 缓存
// 第一阶段优化：供 IPMonitor 在并发探测完成后调用，将结果同步到全局 RTT 缓存和 IPPool
// 参数：
// - ip: IP 地址
// - rtt: RTT 值（毫秒），-1 表示不可达
// - loss: 丢包率（0-100）
// - probeMethod: 探测方法（icmp, tls, udp53, tcp80）
//
// 第一阶段改造：IP 池"真理化"改造
// - 探测结果不仅写入 RTT 缓存，还同步更新到全局 IPPool 的 RTT 字段
// - 使用 EWMA 平滑 RTT 值，防止抖动
//
// 静默隔离改造：如果网络探测器报告离线，则拒绝更新缓存，防止缓存污染
func (p *Pinger) UpdateIPCache(ip string, rtt int, loss float64, probeMethod string) {
	if p.rttCacheTtlSeconds <= 0 {
		return
	}

	// 静默隔离：如果网络探测器报告离线，则拒绝更新 RTT 缓存
	// 目的："缓存防毒"。如果是本地断网（由于拨号、网关故障等），
	// 探测结果必然是全部超时。如果不拦截，这些假性的"不可达"会瞬间刷掉之前缓存的所有优质 IP 数据。
	//
	// 修复 #1：断网时仍然更新 IPPool 的 EWMA 数据，因为：
	// 1. IPPool 使用 EWMA 平滑，单次断网探测不会剧烈影响历史数据
	// 2. 网络恢复后，IPPool 的数据可以帮助快速恢复排序
	// 3. 只阻止 RTT 缓存更新，防止"不可达"结果污染缓存
	isNetworkOffline := p.healthChecker != nil && !p.healthChecker.IsNetworkHealthy()
	if isNetworkOffline {
		logger.Warnf("[Pinger] Network is offline, skipping RTT cache update for %s to prevent poisoning", ip)
		// 仍然更新 IPPool 的 EWMA 数据（使用较小的平滑系数，减少断网数据的影响）
		// 第四阶段：使用配置化的 alphaOffline 参数
		if p.ipPool != nil {
			alpha := p.alphaOffline
			if alpha <= 0 {
				alpha = 0.1 // 默认值
			}
			p.ipPool.UpdateIPRTT(ip, rtt, loss, alpha)
		}
		return
	}

	// 第一阶段改造：同步更新 IPPool 中的 RTT 数据
	// 第四阶段：使用配置化的 alphaOnline 参数
	if p.ipPool != nil {
		alpha := p.alphaOnline
		if alpha <= 0 {
			alpha = 0.3 // 默认值
		}
		p.ipPool.UpdateIPRTT(ip, rtt, loss, alpha)
	}

	// 第三阶段修复：更新失败权重系统
	// 这样无论是后台监控还是用户请求，只要更新了缓存，权重系统就会同步更新
	// 第四阶段：使用配置化的 deadThresholdMs 参数
	deadThreshold := p.deadThresholdMs
	if deadThreshold <= 0 {
		deadThreshold = LogicDeadRTT // 默认值
	}
	if rtt < deadThreshold {
		p.RecordIPSuccess(ip)
	} else {
		p.RecordIPFailure(ip)
	}

	// 构造 Result 用于计算动态 TTL
	result := Result{
		IP:          ip,
		RTT:         rtt,
		Loss:        loss,
		ProbeMethod: probeMethod,
	}

	ttl := p.calculateDynamicTTL(result)
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

	p.rttCache.set(ip, &rttCacheEntry{
		rtt:       rtt,
		loss:      loss,
		staleAt:   staleAt,
		expiresAt: expiresAt,
	})
}

// calculateDynamicTTL 根据探测结果动态计算缓存 TTL
// 基于丢包率和 RTT 来决定缓存时间
// 完全成功的 IP 缓存更久，失败的 IP 缓存更短
// TTL 基于全局配置的权重比例计算，确保尊重用户配置
func (p *Pinger) calculateDynamicTTL(r Result) time.Duration {
	// 基础 TTL：使用全局配置值
	baseTTL := time.Duration(p.rttCacheTtlSeconds) * time.Second
	if baseTTL == 0 {
		// 如果未配置，使用默认值
		baseTTL = 60 * time.Second
	}

	// 根据 IP 质量计算权重比例（相对于基础 TTL）
	var ratio float64

	if r.Loss == 0 {
		// 完全成功（0% 丢包）
		// 修复 #7：使用可配置的 RTT 阈值，而非硬编码的 50ms/100ms
		excellentThreshold := p.rttThresholdExcellent
		if excellentThreshold <= 0 {
			excellentThreshold = 50 // 默认值
		}
		goodThreshold := p.rttThresholdGood
		if goodThreshold <= 0 {
			goodThreshold = 100 // 默认值
		}

		if r.RTT < excellentThreshold {
			// 极优 IP：10 倍基础 TTL
			ratio = 10.0
		} else if r.RTT < goodThreshold {
			// 优质 IP：5 倍基础 TTL
			ratio = 5.0
		} else {
			// 一般 IP：2 倍基础 TTL
			ratio = 2.0
		}
	} else if r.Loss < 20 {
		// 轻微丢包（< 20%）：1 倍基础 TTL
		ratio = 1.0
	} else if r.Loss < 50 {
		// 中等丢包（20-50%）：0.5 倍基础 TTL
		ratio = 0.5
	} else if r.Loss < 100 {
		// 严重丢包（50-100%）：0.3 倍基础 TTL (提高比例，避免清理太快)
		ratio = 0.3
	} else {
		// 完全失败（100% 丢包）：0.2 倍基础 TTL (提高比例，避免清理太快)
		ratio = 0.2
	}

	ttl := time.Duration(float64(baseTTL) * ratio)
	// 强制最小 TTL 为 15 秒，避免在低频次访问下缓存瞬间消失
	if ttl < 15*time.Second {
		ttl = 15 * time.Second
	}
	return ttl
}

// triggerStaleRevalidate 触发异步软过期更新
// 当缓存处于软过期期间时，返回旧数据给用户，同时在后台异步更新
// 使用 staleRevalidating 记录来避免重复触发
// 熔断：断网时不触发异步探测，避免无效的后台探测请求
func (p *Pinger) triggerStaleRevalidate(ip, domain string) {
	// 网络异常期，不触发异步探测
	// 避免断网时发起无效的后台探测请求
	if p.healthChecker != nil && !p.healthChecker.IsNetworkHealthy() {
		return
	}

	p.staleRevalidateMu.Lock()
	// 检查是否已经在更新中
	if p.staleRevalidating[ip] {
		p.staleRevalidateMu.Unlock()
		return
	}
	// 标记为正在更新
	p.staleRevalidating[ip] = true
	p.staleRevalidateMu.Unlock()

	// 在后台 goroutine 中执行异步更新
	go func() {
		defer func() {
			// 更新完成后，清除标记
			p.staleRevalidateMu.Lock()
			delete(p.staleRevalidating, ip)
			p.staleRevalidateMu.Unlock()
		}()

		// 执行探测
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.timeoutMs)*time.Millisecond)
		defer cancel()

		result := p.pingIP(ctx, ip, domain)
		if result == nil {
			return
		}

		// 记录失效权重（使用统一方法）
		// 修复 #8：使用统一的 recordProbeResult 方法
		p.recordProbeResult(ip, result.Loss, result.FastFail)

		// 更新缓存
		if p.rttCacheTtlSeconds > 0 {
			ttl := p.calculateDynamicTTL(*result)
			staleAt := time.Now().Add(ttl)

			gracePeriod := p.staleGracePeriod
			if gracePeriod == 0 {
				gracePeriod = 30 * time.Second
			}
			if gracePeriod > ttl/2 {
				gracePeriod = ttl / 2
			}
			expiresAt := staleAt.Add(gracePeriod)

			p.rttCache.set(ip, &rttCacheEntry{
				rtt:       result.RTT,
				loss:      result.Loss,
				staleAt:   staleAt,
				expiresAt: expiresAt,
			})
		}
	}()
}

// startRttCacheCleaner 启动 RTT 缓存清理器
// 定期清理过期的缓存条目
// 使用分片缓存，每个分片独立清理，减少锁竞争
func (p *Pinger) startRttCacheCleaner() {
	ticker := time.NewTicker(time.Duration(p.rttCacheTtlSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 分片缓存的清理操作会自动处理所有分片
			// 每个分片独立加锁，不会相互阻塞
			cleaned := p.rttCache.cleanupExpired()
			// 可选：记录清理统计信息
			_ = cleaned
		case <-p.stopChan:
			return
		}
	}
}
