package ping

import (
	"context"
	"log"
	"net"
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

// Pinger IP ping 测试模块
type Pinger struct {
	count       int
	timeoutMs   int
	concurrency int
	strategy    string // min, avg
}

// NewPinger 创建新的 pinger
func NewPinger(count, timeoutMs, concurrency int, strategy string) *Pinger {
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

	return &Pinger{
		count:       count,
		timeoutMs:   timeoutMs,
		concurrency: concurrency,
		strategy:    strategy,
	}
}

// PingIPs 并发 ping 多个 IP，返回排序后的结果
func (p *Pinger) PingIPs(ctx context.Context, ips []string) []Result {
	if len(ips) == 0 {
		return []Result{}
	}

	log.Printf("[PingIPs] 开始对 %d 个 IP 进行并发ping测试，并发数:%d, 每个IP测试次数:%d\n", len(ips), p.concurrency, p.count)

	// 使用 semaphore 控制并发
	sem := make(chan struct{}, p.concurrency)
	results := make([]Result, 0, len(ips))
	resultCh := make(chan Result, len(ips))
	var wg sync.WaitGroup
	var runningMu sync.Mutex
	currentlyRunning := 0

	for idx, ip := range ips {
		wg.Add(1)
		go func(index int, ipAddr string) {
			defer wg.Done()
			log.Printf("[PingIPs] IP #%d (%s) 等待信号量...", index+1, ipAddr)
			sem <- struct{}{} // 获取信号量

			runningMu.Lock()
			currentlyRunning++
			log.Printf("[PingIPs] IP #%d (%s) 开始ping (当前正在执行:%d/%d)", index+1, ipAddr, currentlyRunning, p.concurrency)
			runningMu.Unlock()

			defer func() {
				<-sem
				runningMu.Lock()
				currentlyRunning--
				runningMu.Unlock()
			}()

			result := p.pingIP(ctx, ipAddr)
			// 总是发送结果，包括失败的 IP
			resultCh <- *result
		}(idx, ip)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 收集结果
	for result := range resultCh {
		results = append(results, result)
	}

	log.Printf("[PingIPs] 所有ping测试完成，收集了 %d 个结果\n", len(results))

	// 排序
	p.sortResults(results)

	log.Printf("[PingIPs] 排序完成，最终结果: %v\n", results)

	return results
}

// pingIP 单个 IP 的 ping 测试（使用 TCP Ping）
func (p *Pinger) pingIP(ctx context.Context, ip string) *Result {
	var totalRTT int64
	var minRTT int = 999999
	successCount := 0

	log.Printf("[pingIP %s] 开始对IP进行 %d 次ping测试\n", ip, p.count)

	for i := 0; i < p.count; i++ {
		rtt := p.tcpPing(ctx, ip)
		if rtt >= 0 {
			totalRTT += int64(rtt)
			successCount++
			// 记录最小 RTT
			if rtt < minRTT {
				minRTT = rtt
			}
			log.Printf("[pingIP %s] 尝试 #%d: RTT=%dms\n", ip, i+1, rtt)
		} else {
			log.Printf("[pingIP %s] 尝试 #%d: 失败\n", ip, i+1)
		}
	}

	var avgRTT int
	if successCount == 0 {
		// Ping 失败：设置高的 RTT 值，排在后面
		avgRTT = 999999 // 设置为很高的值，确保排在最后
		log.Printf("[pingIP %s] 最终: 全部失败，RTT=999999\n", ip)
	} else if p.strategy == "avg" {
		avgRTT = int(totalRTT / int64(successCount))
		log.Printf("[pingIP %s] 最终: 成功%d/%d次，RTT=%dms (avg)，丢包率%.1f%%\n", ip, successCount, p.count, avgRTT, float64(p.count-successCount)/float64(p.count)*100)
	} else {
		// min strategy: 使用最小 RTT
		avgRTT = minRTT
		log.Printf("[pingIP %s] 最终: 成功%d/%d次，RTT=%dms (min)，丢包率%.1f%%\n", ip, successCount, p.count, avgRTT, float64(p.count-successCount)/float64(p.count)*100)
	}

	lossRate := float64(p.count-successCount) / float64(p.count) * 100

	// 总是返回结果，即使 Ping 失败也会返回（RTT 为 999999）
	return &Result{
		IP:   ip,
		RTT:  avgRTT,
		Loss: lossRate,
	}
}

// tcpPing TCP Ping 测试（模拟真实网络环境）
func (p *Pinger) tcpPing(ctx context.Context, ip string) int {
	start := time.Now()

	// 尝试连接常见的 HTTP/HTTPS 端口
	ports := []string{"80", "443"}
	for _, port := range ports {
	addr := net.JoinHostPort(ip, port)

		// 设置超时
		deadline := time.Now().Add(time.Duration(p.timeoutMs) * time.Millisecond)
		newCtx, cancel := context.WithDeadline(ctx, deadline)

		dialer := &net.Dialer{
			Timeout: time.Duration(p.timeoutMs) * time.Millisecond,
		}

		conn, err := dialer.DialContext(newCtx, "tcp", addr)
		cancel()

		if err == nil {
			conn.Close()
			elapsed := time.Since(start).Milliseconds()
			return int(elapsed)
		}
	}

	return -1 // 失败返回 -1
}

// sortResults 按 RTT 排序结果
// 排序规则：
// 1. 首先按成功率排序（成功率高的优先）
// 2. 然后按 RTT 排序（RTT 低的优先）
// 3. Ping 失败的 IP（RTT=999999）自动排在最后
func (p *Pinger) sortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		// 首先按成功率排序（成功率高的优先）
		if results[i].Loss != results[j].Loss {
			return results[i].Loss < results[j].Loss
		}
		// 然后按 RTT 排序（RTT 低的优先）
		// Ping 失败的 IP RTT 为 999999，会自动排到最后
		return results[i].RTT < results[j].RTT
	})
}

// SortIPs 将 IP 列表按 RTT 排序，返回排序后的 IP 列表
func (p *Pinger) SortIPs(ctx context.Context, ips []string) []string {
	results := p.PingIPs(ctx, ips)
	sortedIPs := make([]string, len(results))
	for i, result := range results {
		sortedIPs[i] = result.IP
	}
	return sortedIPs
}

// PingAndSort 对 IP 列表进行 ping 测试并排序，返回完整结果（包括 RTT）
func (p *Pinger) PingAndSort(ctx context.Context, ips []string) []Result {
	return p.PingIPs(ctx, ips)
}
