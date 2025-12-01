package ping

import (
	"context"
	"net"
	"smartdnssort/logger"
	"sort"
	"sync"
	"time"
)

// Result ping 结果
type Result struct {
	IP   string
	RTT  int // 毫秒
	Loss float64
}

// rttCacheEntry RTT 缓存条目
type rttCacheEntry struct {
	rtt       int
	expiresAt time.Time
}

// Pinger IP ping 测试模块
type Pinger struct {
	count              int
	timeoutMs          int
	concurrency        int
	strategy           string // min, avg
	maxTestIPs         int
	rttCacheTtlSeconds int
	rttCache           map[string]*rttCacheEntry
	rttCacheMu         sync.RWMutex
	stopChan           chan struct{}
}

// NewPinger 创建新的 pinger
func NewPinger(count, timeoutMs, concurrency, maxTestIPs, rttCacheTtlSeconds int, strategy string) *Pinger {
	if count <= 0 {
		count = 3
	}
	if timeoutMs <= 0 {
		timeoutMs = 500
	}
	if concurrency <= 0 {
		concurrency = 16
	}
	if strategy == "" {
		strategy = "min"
	}

	p := &Pinger{
		count:              count,
		timeoutMs:          timeoutMs,
		concurrency:        concurrency,
		strategy:           strategy,
		maxTestIPs:         maxTestIPs,
		rttCacheTtlSeconds: rttCacheTtlSeconds,
		rttCache:           make(map[string]*rttCacheEntry),
		stopChan:           make(chan struct{}),
	}

	if p.rttCacheTtlSeconds > 0 {
		go p.startRttCacheCleaner()
	}

	return p
}

func (p *Pinger) startRttCacheCleaner() {
	ticker := time.NewTicker(time.Duration(p.rttCacheTtlSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.rttCacheMu.Lock()
			for ip, entry := range p.rttCache {
				if time.Now().After(entry.expiresAt) {
					delete(p.rttCache, ip)
				}
			}
			p.rttCacheMu.Unlock()
		case <-p.stopChan:
			return
		}
	}
}

// Stop 停止 Pinger 的后台任务
func (p *Pinger) Stop() {
	close(p.stopChan)
}

// PingAndSort 对 IP 列表进行 ping 测试并排序，返回完整结果（包括 RTT）
//
// 该函数实现了两个核心优化:
// 1. 智能探测 (Intelligent Probing): 如果设置了 maxTestIPs > 0，则只对列表中的前 N 个 IP 进行测试。
// 2. RTT 缓存 (RTT Caching): 在 ping 之前检查缓存。如果 IP 的延迟数据在缓存中且未过期，则直接使用，避免重复 ping。
func (p *Pinger) PingAndSort(ctx context.Context, ips []string) []Result {
	if len(ips) == 0 {
		return []Result{}
	}

	// 1. 智能探测：如果设置了 maxTestIPs，则截取部分 IP 进行测试
	var ipsToTest []string
	if p.maxTestIPs > 0 && len(ips) > p.maxTestIPs {
		ipsToTest = ips[:p.maxTestIPs]
		logger.Debugf("[PingAndSort] IP 数量超过 max_test_ips (%d)，只测试前 %d 个 IP", p.maxTestIPs, p.maxTestIPs)
	} else {
		ipsToTest = ips
	}

	// 2. RTT 缓存检查
	var ipsToPing []string
	var cachedResults []Result
	if p.rttCacheTtlSeconds > 0 {
		p.rttCacheMu.RLock()
		for _, ip := range ipsToTest {
			if entry, exists := p.rttCache[ip]; exists && time.Now().Before(entry.expiresAt) {
				// 缓存命中且未过期
				cachedResults = append(cachedResults, Result{IP: ip, RTT: entry.rtt, Loss: 0})
			} else {
				// 缓存未命中或已过期
				ipsToPing = append(ipsToPing, ip)
			}
		}
		p.rttCacheMu.RUnlock()
		logger.Debugf("[PingAndSort] RTT 缓存检查完成: %d 个命中, %d 个需要 ping", len(cachedResults), len(ipsToPing))
	} else {
		// 未启用 RTT 缓存
		ipsToPing = ipsToTest
	}

	// 3. 对需要 ping 的 IP 进行并发测试
	var pingedResults []Result
	if len(ipsToPing) > 0 {
		pingedResults = p.performConcurrentPing(ctx, ipsToPing)
	}

	// 4. 更新 RTT 缓存
	if p.rttCacheTtlSeconds > 0 && len(pingedResults) > 0 {
		p.rttCacheMu.Lock()
		for _, res := range pingedResults {
			if res.Loss == 0 { // 只缓存成功的 ping 结果
				p.rttCache[res.IP] = &rttCacheEntry{
					rtt:       res.RTT,
					expiresAt: time.Now().Add(time.Duration(p.rttCacheTtlSeconds) * time.Second),
				}
			}
		}
		p.rttCacheMu.Unlock()
	}

	// 5. 合并缓存结果和新 ping 的结果
	finalResults := append(cachedResults, pingedResults...)

	// 6. 对最终结果进行排序
	p.sortResults(finalResults)

	logger.Debugf("[PingAndSort] 排序完成，最终结果: %v", finalResults)

	return finalResults
}

// performConcurrentPing 并发 ping 多个 IP，返回未排序的结果
func (p *Pinger) performConcurrentPing(ctx context.Context, ips []string) []Result {
	logger.Debugf("[performConcurrentPing] 开始对 %d 个 IP 进行并发ping测试，并发数:%d, 每个IP测试次数:%d", len(ips), p.concurrency, p.count)

	sem := make(chan struct{}, p.concurrency)
	resultCh := make(chan Result, len(ips))
	var wg sync.WaitGroup

	for _, ip := range ips {
		wg.Add(1)
		go func(ipAddr string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := p.pingIP(ctx, ipAddr)
			resultCh <- *result
		}(ip)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	results := make([]Result, 0, len(ips))
	for result := range resultCh {
		results = append(results, result)
	}

	logger.Debugf("[performConcurrentPing] 所有 ping 测试完成，收集了 %d 个结果", len(results))
	return results
}

// pingIP 单个 IP 的 ping 测试（使用 TCP Ping）
func (p *Pinger) pingIP(ctx context.Context, ip string) *Result {
	var totalRTT int64
	var minRTT int = 999999
	successCount := 0

	for i := 0; i < p.count; i++ {
		rtt := p.tcpPing(ctx, ip)
		if rtt >= 0 {
			totalRTT += int64(rtt)
			successCount++
			if rtt < minRTT {
				minRTT = rtt
			}
		}
	}

	var finalRTT int
	// 在 pingIP 中计算综合 RTT（关键！）
	if successCount == 0 {
		finalRTT = 999999
	} else {
		avgRTT := int(totalRTT / int64(successCount))
		// 每丢一个包惩罚 150~200ms（推荐 180，效果最佳）
		penalty := (p.count - successCount) * 180
		finalRTT = avgRTT + penalty
		// 可选：设置上限，防止惩罚过大
		if finalRTT > 5000 {
			finalRTT = 5000
		}
	}

	lossRate := float64(p.count-successCount) / float64(p.count) * 100

	return &Result{
		IP:   ip,
		RTT:  finalRTT,
		Loss: lossRate,
	}
}

// tcpPing TCP Ping 测试（模拟真实网络环境）
func (p *Pinger) tcpPing(ctx context.Context, ip string) int {
	// 尝试连接常见的 HTTP/HTTPS 端口
	ports := []string{"80", "443"}
	for _, port := range ports {
		addr := net.JoinHostPort(ip, port)
		dialer := &net.Dialer{Timeout: time.Duration(p.timeoutMs) * time.Millisecond}

		start := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", addr)

		if err == nil {
			conn.Close()
			return int(time.Since(start).Milliseconds())
		}
	}
	return -1 // 失败返回 -1
}

// sortResults 按 RTT 排序结果
// 排序规则：
// 1. 首先按丢包率排序（丢包率低的优先）
// 2. 然后按 RTT 排序（RTT 低的优先）
// 排序函数：极简 + 最优
func (p *Pinger) sortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		a, b := results[i], results[j]

		// 1. 完全不通的永远靠后（核心修复点）
		if (a.Loss == 100.0) != (b.Loss == 100.0) {
			return b.Loss == 100.0 // 只有 b 是 100% 时返回 true → b 排后面
		}

		// 2. 同类节点比综合 RTT
		if a.RTT != b.RTT {
			return a.RTT < b.RTT
		}

		// 3. 完全相同时按 IP 排序（保证稳定）
		return a.IP < b.IP
	})
}
