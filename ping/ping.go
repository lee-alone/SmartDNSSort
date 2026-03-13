package ping

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Result 表示单个 IP 的 ping 结果
type Result struct {
	IP          string
	RTT         int // 毫秒，999999 表示不可达
	Loss        float64
	ProbeMethod string // 探测方法：icmp, tcp443, tls, udp53, tcp80, none
	FastFail    bool   // 标记是否触发了快速失败（避免两重记录）
}

// rttCacheEntry 缓存条目
type rttCacheEntry struct {
	rtt       int
	loss      float64   // 丢包率，用于负向缓存
	staleAt   time.Time // 软过期时间：超过此时间，缓存变为陈旧，返回旧数据并异步刷新
	expiresAt time.Time // 硬过期时间：超过此时间，缓存完全失效并被清理
}

// Pinger DNS IP 延迟测量和排序工具
// 提供智能混合探测、缓存和并发测试功能
type Pinger struct {
	count              int
	timeoutMs          int
	concurrency        int
	strategy           string // 已弃用：保留用于向后兼容。详见 PING_NOTES.md
	maxTestIPs         int
	rttCacheTtlSeconds int
	enableHttpFallback bool // 是否对纯 HTTP(80) 做补充探测，默认关闭

	rttCache         *shardedRttCache // 改为分片缓存，减少锁竞争
	stopChan         chan struct{}
	bufferPool       *sync.Pool // 新增: 复用 UDP 读取 buffer
	failureWeightMgr *IPFailureWeightManager
	probeFlight      *singleflight.Group // 新增: 请求合并，避免重复探测同一 IP
	ipPool           *IPPool             // 全局 IP 资源管理器，用于 SNI 绑定

	// Stale-While-Revalidate 相关
	staleRevalidateMu sync.Mutex
	staleRevalidating map[string]bool // 记录正在异步更新的 IP，避免重复触发
	staleGracePeriod  time.Duration   // 软过期容忍期（默认 30 秒）
}

// PingAndSort 执行并发 ping 测试并返回排序后的结果
// 支持缓存、智能探测和并发控制
//
// 第三阶段优化：测速逻辑切换
// - 首选路径：直接读取 IPMonitor 维护的 RTT 缓存数据
// - 兜底方案：当缓存不存在或过期时，才触发实时探测
// - 这样可以显著降低用户请求触发的探测频率，减少 ICMP 流量
func (p *Pinger) PingAndSort(ctx context.Context, ips []string, domain string) []Result {
	if len(ips) == 0 {
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
					cached = append(cached, Result{IP: ip, RTT: e.rtt, Loss: e.loss, ProbeMethod: "cached"})
					p.RecordIPSuccess(ip)
				} else if now.Before(e.expiresAt) {
					// 缓存处于软过期期间（Stale）：返回旧数据，异步更新
					// IPMonitor 可能正在刷新，返回旧数据即可
					cached = append(cached, Result{IP: ip, RTT: e.rtt, Loss: e.loss, ProbeMethod: "stale"})
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

// RecordIPFailure 记录IP失效（应用层调用）
func (p *Pinger) RecordIPFailure(ip string) {
	if p.failureWeightMgr != nil {
		p.failureWeightMgr.RecordFailure(ip)
	}
}

// RecordIPSuccess 记录IP成功（应用层调用）
func (p *Pinger) RecordIPSuccess(ip string) {
	if p.failureWeightMgr != nil {
		p.failureWeightMgr.RecordSuccess(ip)
	}
}

// RecordIPFastFail 记录IP快速失败（第一次探测就超时）
func (p *Pinger) RecordIPFastFail(ip string) {
	if p.failureWeightMgr != nil {
		p.failureWeightMgr.RecordFastFail(ip)
	}
}

// SaveIPFailureWeights 保存IP失效权重到磁盘
func (p *Pinger) SaveIPFailureWeights() error {
	if p.failureWeightMgr != nil {
		return p.failureWeightMgr.SaveToDisk()
	}
	return nil
}

// GetIPFailureRecord 获取IP的失效记录
func (p *Pinger) GetIPFailureRecord(ip string) *IPFailureRecord {
	if p.failureWeightMgr != nil {
		return p.failureWeightMgr.GetRecord(ip)
	}
	return &IPFailureRecord{IP: ip}
}

// GetAllIPFailureRecords 获取所有IP的失效记录
func (p *Pinger) GetAllIPFailureRecords() []*IPFailureRecord {
	if p.failureWeightMgr != nil {
		return p.failureWeightMgr.GetAllRecords()
	}
	return nil
}

// GetIPPool 获取全局 IP 资源管理器
func (p *Pinger) GetIPPool() *IPPool {
	return p.ipPool
}

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
// 第三阶段优化：供 IPMonitor 在并发探测完成后调用，将结果同步到全局 RTT 缓存
// 参数：
//   - ip: IP 地址
//   - rtt: RTT 值（毫秒），-1 表示不可达
//   - loss: 丢包率（0-100）
//   - probeMethod: 探测方法（icmp, tls, udp53, tcp80）
//
// 第三阶段修复：内部调用 RecordIPSuccess/Failure，保证权重系统同步更新
func (p *Pinger) UpdateIPCache(ip string, rtt int, loss float64, probeMethod string) {
	if p.rttCacheTtlSeconds <= 0 {
		return
	}

	// 第三阶段修复：更新失败权重系统
	// 这样无论是后台监控还是用户请求，只要更新了缓存，权重系统就会同步更新
	if loss >= 100 {
		p.RecordIPFailure(ip)
	} else {
		p.RecordIPSuccess(ip)
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
		if r.RTT < 50 {
			// 极优 IP（RTT < 50ms）：10 倍基础 TTL
			ratio = 10.0
		} else if r.RTT < 100 {
			// 优质 IP（RTT 50-100ms）：5 倍基础 TTL
			ratio = 5.0
		} else {
			// 一般 IP（RTT >= 100ms）：2 倍基础 TTL
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
func (p *Pinger) triggerStaleRevalidate(ip, domain string) {
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

		// 记录失效权重
		if result.FastFail {
			// 已经在 pingIP 中记录过了
		} else if result.Loss == 100 {
			p.RecordIPFailure(ip)
		} else {
			p.RecordIPSuccess(ip)
		}

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
