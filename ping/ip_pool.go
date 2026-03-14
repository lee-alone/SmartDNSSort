package ping

import (
	"sync"
	"time"
)

// IPInfo IP 池中的 IP 信息
type IPInfo struct {
	IP            string    // IP 地址
	RefCount      int       // 引用计数（有多少个域名使用此 IP）
	AccessHeat    int64     // 访问热度（累计访问次数）
	LastAccess    time.Time // 最后访问时间
	RepDomain     string    // 代表性域名（用于 SNI 绑定）
	RepDomainHeat int64     // 代表性域名的热度

	// 第一阶段新增：RTT 数据（真理化改造）
	RTT        int       // 最新 RTT 值（毫秒），LogicDeadRTT 表示不可达
	RTTUpdated time.Time // RTT 更新时间
	RTTEWMA    int       // EWMA 平滑后的 RTT 值（用于排序）
	loss       float64   // 丢包率（0-100）
}

// IPPool 全局 IP 资源管理器
// 维护 IP -> {代表性域名, 引用计数, 访问热度} 的映射
type IPPool struct {
	mu    sync.RWMutex
	ips   map[string]*IPInfo // IP -> IPInfo
	stats IPPoolStats        // 统计信息
}

// IPPoolStats IP 池统计信息
type IPPoolStats struct {
	TotalIPs      int       // 总 IP 数
	TotalRefCount int       // 总引用计数
	TotalHeat     int64     // 总访问热度
	LastUpdated   time.Time // 最后更新时间
}

// NewIPPool 创建新的 IP 池
func NewIPPool() *IPPool {
	return &IPPool{
		ips: make(map[string]*IPInfo),
	}
}

// UpdateDomainIPs 更新域名的 IP 列表
// 当域名的 IP 列表发生变化时调用此方法
// oldIPs: 旧的 IP 列表（可能为空）
// newIPs: 新的 IP 列表
// domain: 域名
func (p *IPPool) UpdateDomainIPs(oldIPs, newIPs []string, domain string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()

	// 使用 map 来快速查找和去重
	oldIPSet := make(map[string]bool)
	for _, ip := range oldIPs {
		oldIPSet[ip] = true
	}

	newIPSet := make(map[string]bool)
	for _, ip := range newIPs {
		newIPSet[ip] = true
	}

	// 第一步：处理需要移除的 IP（在 oldIPs 中但不在 newIPs 中）
	for ip := range oldIPSet {
		if !newIPSet[ip] {
			// IP 被移除
			if info, exists := p.ips[ip]; exists {
				info.RefCount--
				if info.RefCount <= 0 {
					// 引用计数为 0，删除该 IP
					delete(p.ips, ip)
				} else {
					// 如果代表性域名是当前域名，需要重新选择
					if info.RepDomain == domain {
						p.selectRepDomain(info)
					}
				}
			}
		}
	}

	// 第二步：处理需要新增或保留的 IP（在 newIPs 中）
	for ip := range newIPSet {
		if info, exists := p.ips[ip]; exists {
			// IP 已存在，更新引用计数和访问时间
			if !oldIPSet[ip] {
				// 新增的 IP（不在 oldIPs 中）
				info.RefCount++
			}
			// 保留的 IP（在 oldIPs 和 newIPs 中都存在），引用计数不变
			info.LastAccess = now
			// 更新代表性域名
			p.updateRepDomain(info, domain)
		} else {
			// 新 IP，创建记录
			p.ips[ip] = &IPInfo{
				IP:            ip,
				RefCount:      1,
				AccessHeat:    0,
				LastAccess:    now,
				RepDomain:     domain,
				RepDomainHeat: 0,
			}
		}
	}

	p.stats.LastUpdated = now
	p.updateStats()
}

// RecordAccess 记录 IP 访问
// 当某个 IP 被访问时调用此方法，增加访问热度
func (p *IPPool) RecordAccess(ip, domain string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()

	if info, exists := p.ips[ip]; exists {
		info.AccessHeat++
		info.LastAccess = now
		// 更新代表性域名
		p.updateRepDomain(info, domain)
		p.stats.LastUpdated = now
		p.stats.TotalHeat++
	}
}

// GetIPInfo 获取 IP 信息
func (p *IPPool) GetIPInfo(ip string) (*IPInfo, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	info, exists := p.ips[ip]
	if !exists {
		return nil, false
	}

	// 返回副本，避免外部修改
	return &IPInfo{
		IP:            info.IP,
		RefCount:      info.RefCount,
		AccessHeat:    info.AccessHeat,
		LastAccess:    info.LastAccess,
		RepDomain:     info.RepDomain,
		RepDomainHeat: info.RepDomainHeat,
	}, true
}

// GetRepDomain 获取 IP 的代表性域名
// 用于 SNI 绑定
func (p *IPPool) GetRepDomain(ip string) (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if info, exists := p.ips[ip]; exists {
		return info.RepDomain, true
	}
	return "", false
}

// GetAllIPs 获取所有 IP 信息
func (p *IPPool) GetAllIPs() []*IPInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]*IPInfo, 0, len(p.ips))
	for _, info := range p.ips {
		result = append(result, &IPInfo{
			IP:            info.IP,
			RefCount:      info.RefCount,
			AccessHeat:    info.AccessHeat,
			LastAccess:    info.LastAccess,
			RepDomain:     info.RepDomain,
			RepDomainHeat: info.RepDomainHeat,
		})
	}
	return result
}

// GetStats 获取 IP 池统计信息
func (p *IPPool) GetStats() IPPoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.stats
}

// selectRepDomain 选择代表性域名
// 当当前代表性域名被移除时，需要重新选择
func (p *IPPool) selectRepDomain(info *IPInfo) {
	// 简单策略：选择引用计数最高的域名
	// 由于我们没有维护完整的域名->IP 映射，这里简化处理
	// 在实际使用中，可以通过其他方式获取该 IP 的所有域名
	// 这里暂时保持不变，等待下次访问时更新
}

// updateRepDomain 更新代表性域名（内部方法）
// 根据访问热度选择代表性域名
func (p *IPPool) updateRepDomain(info *IPInfo, domain string) {
	// 简单策略：如果当前域名的热度超过代表性域名的热度，则更新
	// 这里我们假设每次访问都会增加热度，所以直接更新
	// 在实际实现中，可能需要更复杂的策略
	info.RepDomain = domain
	info.RepDomainHeat++
}

// UpdateRepDomainOnSuccess 在探测成功时更新代表性域名
// 第三阶段优化：当用户访问的域名探测成功时，可以更新该 IP 的代表性域名
// 参数：
//   - ip: IP 地址
//   - domain: 探测成功的域名
//   - forceUpdate: 是否强制更新（当代表性域名探测失败时设为 true）
func (p *IPPool) UpdateRepDomainOnSuccess(ip, domain string, forceUpdate bool) {
	if domain == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if info, exists := p.ips[ip]; exists {
		if forceUpdate {
			// 强制更新：代表性域名探测失败，用户域名探测成功
			info.RepDomain = domain
			info.RepDomainHeat = 1 // 重置热度
		} else if info.RepDomain == "" {
			// 如果没有代表性域名，直接设置
			info.RepDomain = domain
			info.RepDomainHeat = 1
		} else if info.RepDomain == domain {
			// 如果是同一个域名，增加热度
			info.RepDomainHeat++
		}
		// 否则不更新，保持现有的代表性域名
	}
}

// CheckAndUpdateRepDomain 检查并更新代表性域名
// 第三阶段优化：当探测失败时，尝试切换到备用域名
// 参数：
//   - ip: IP 地址
//   - failedDomain: 探测失败的域名（可能是代表性域名）
//   - successDomain: 探测成功的域名（用户访问的域名）
func (p *IPPool) CheckAndUpdateRepDomain(ip, failedDomain, successDomain string) {
	if successDomain == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if info, exists := p.ips[ip]; exists {
		// 如果失败的域名是当前代表性域名，则切换到成功的域名
		if info.RepDomain == failedDomain && failedDomain != successDomain {
			info.RepDomain = successDomain
			info.RepDomainHeat = 1
		}
	}
}

// updateStats 更新统计信息
func (p *IPPool) updateStats() {
	p.stats.TotalIPs = len(p.ips)
	p.stats.TotalRefCount = 0
	p.stats.TotalHeat = 0

	for _, info := range p.ips {
		p.stats.TotalRefCount += info.RefCount
		p.stats.TotalHeat += info.AccessHeat
	}
}

// Clear 清空 IP 池
func (p *IPPool) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ips = make(map[string]*IPInfo)
	p.stats = IPPoolStats{}
}

// RemoveIP 移除指定的 IP
func (p *IPPool) RemoveIP(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.ips, ip)
	p.updateStats()
}

// GetTopIPsByRefCount 获取引用计数最高的 N 个 IP
func (p *IPPool) GetTopIPsByRefCount(n int) []*IPInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if n <= 0 {
		return nil
	}

	// 收集所有 IP
	ips := make([]*IPInfo, 0, len(p.ips))
	for _, info := range p.ips {
		ips = append(ips, &IPInfo{
			IP:            info.IP,
			RefCount:      info.RefCount,
			AccessHeat:    info.AccessHeat,
			LastAccess:    info.LastAccess,
			RepDomain:     info.RepDomain,
			RepDomainHeat: info.RepDomainHeat,
		})
	}

	// 按引用计数排序
	for i := 0; i < len(ips) && i < n; i++ {
		for j := i + 1; j < len(ips); j++ {
			if ips[j].RefCount > ips[i].RefCount {
				ips[i], ips[j] = ips[j], ips[i]
			}
		}
	}

	// 返回前 N 个
	if len(ips) > n {
		ips = ips[:n]
	}

	return ips
}

// GetTopIPsByAccessHeat 获取访问热度最高的 N 个 IP
func (p *IPPool) GetTopIPsByAccessHeat(n int) []*IPInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if n <= 0 {
		return nil
	}

	// 收集所有 IP
	ips := make([]*IPInfo, 0, len(p.ips))
	for _, info := range p.ips {
		ips = append(ips, &IPInfo{
			IP:            info.IP,
			RefCount:      info.RefCount,
			AccessHeat:    info.AccessHeat,
			LastAccess:    info.LastAccess,
			RepDomain:     info.RepDomain,
			RepDomainHeat: info.RepDomainHeat,
		})
	}

	// 按访问热度排序
	for i := 0; i < len(ips) && i < n; i++ {
		for j := i + 1; j < len(ips); j++ {
			if ips[j].AccessHeat > ips[i].AccessHeat {
				ips[i], ips[j] = ips[j], ips[i]
			}
		}
	}

	// 返回前 N 个
	if len(ips) > n {
		ips = ips[:n]
	}

	return ips
}

// UpdateIPRTT 更新 IP 的 RTT 数据（第一阶段：真理化改造）
// 参数：
// - ip: IP 地址
// - rtt: RTT 值（毫秒），LogicDeadRTT 表示不可达
// - loss: 丢包率（0-100）
// - alpha: EWMA 平滑系数（0.0-1.0），推荐 0.3
func (p *IPPool) UpdateIPRTT(ip string, rtt int, loss float64, alpha float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if info, exists := p.ips[ip]; exists {
		now := time.Now()

		// 更新原始 RTT 值
		info.RTT = rtt
		info.loss = loss
		info.RTTUpdated = now

		// 使用 EWMA 平滑 RTT 值
		if info.RTTEWMA == 0 {
			// 首次更新，直接使用当前值
			info.RTTEWMA = rtt
		} else {
			// EWMA = alpha * current + (1 - alpha) * previous
			info.RTTEWMA = int(float64(rtt)*alpha + (1-alpha)*float64(info.RTTEWMA))
		}
	}
}

// GetIPRTT 获取 IP 的 RTT 数据
// 返回值：
// - rtt: 最新 RTT 值（毫秒）
// - rttEWMA: EWMA 平滑后的 RTT 值
// - updated: RTT 是否存在
func (p *IPPool) GetIPRTT(ip string) (rtt int, rttEWMA int, updated bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if info, exists := p.ips[ip]; exists {
		return info.RTT, info.RTTEWMA, !info.RTTUpdated.IsZero()
	}
	return LogicDeadRTT, LogicDeadRTT, false
}

// GetIPRTTWithLoss 获取 IP 的 RTT 和丢包率
func (p *IPPool) GetIPRTTWithLoss(ip string) (rtt int, rttEWMA int, loss float64, updated bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if info, exists := p.ips[ip]; exists {
		return info.RTT, info.RTTEWMA, info.loss, !info.RTTUpdated.IsZero()
	}
	return LogicDeadRTT, LogicDeadRTT, 0, false
}

// GetAllIPRTTs 批量获取所有 IP 的 RTT 数据（用于排序）
// 返回 map[ip]rttEWMA，只返回有 RTT 数据的 IP
func (p *IPPool) GetAllIPRTTs(ips []string) map[string]int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]int)
	for _, ip := range ips {
		if info, exists := p.ips[ip]; exists && !info.RTTUpdated.IsZero() {
			result[ip] = info.RTTEWMA
		}
	}
	return result
}

// IsIPDead 判断 IP 是否为"死"状态（RTT >= LogicDeadRTT）
func (p *IPPool) IsIPDead(ip string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if info, exists := p.ips[ip]; exists {
		return info.RTT >= LogicDeadRTT
	}
	return false
}
