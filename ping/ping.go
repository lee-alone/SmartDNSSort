package ping

import (
	"context"
	"sync"
	"time"
)

// Result 表示单个 IP 的 ping 结果
type Result struct {
	IP   string
	RTT  int // 毫秒，999999 表示不可达
	Loss float64
}

// rttCacheEntry 缓存条目
type rttCacheEntry struct {
	rtt       int
	expiresAt time.Time
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

	rttCache         map[string]*rttCacheEntry
	rttCacheMu       sync.RWMutex
	stopChan         chan struct{}
	bufferPool       *sync.Pool // 新增: 复用 UDP 读取 buffer
	failureWeightMgr *IPFailureWeightManager
}

// PingAndSort 执行并发 ping 测试并返回排序后的结果
// 支持缓存、智能探测和并发控制
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

	// 缓存检查
	if p.rttCacheTtlSeconds > 0 {
		now := time.Now() // 在循环外调用一次，避免重复系统调用
		p.rttCacheMu.RLock()
		for _, ip := range testIPs {
			if e, ok := p.rttCache[ip]; ok && now.Before(e.expiresAt) {
				cached = append(cached, Result{IP: ip, RTT: e.rtt, Loss: 0})
				// 缓存命中也视为一次成功，维持活跃状态
				p.RecordIPSuccess(ip)
			} else {
				toPing = append(toPing, ip)
			}
		}
		p.rttCacheMu.RUnlock()
	} else {
		toPing = testIPs
	}

	// 并发测
	results := p.concurrentPing(ctx, toPing, domain)

	// 记录失效权重
	for _, r := range results {
		if r.Loss == 100 {
			p.RecordIPFailure(r.IP)
		} else {
			p.RecordIPSuccess(r.IP)
		}
	}

	// 更新缓存（只缓存完全成功的）
	if p.rttCacheTtlSeconds > 0 {
		p.rttCacheMu.Lock()
		for _, r := range results {
			if r.Loss == 0 {
				p.rttCache[r.IP] = &rttCacheEntry{
					rtt:       r.RTT,
					expiresAt: time.Now().Add(time.Duration(p.rttCacheTtlSeconds) * time.Second),
				}
			}
		}
		p.rttCacheMu.Unlock()
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
