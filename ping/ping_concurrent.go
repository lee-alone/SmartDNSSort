package ping

import (
	"context"
	"sort"
	"sync"
)

// concurrentPing 并发测试多个 IP
// 使用信号量控制并发数量，避免资源耗尽
func (p *Pinger) concurrentPing(ctx context.Context, ips []string, domain string) []Result {
	if len(ips) == 0 {
		return nil
	}

	sem := make(chan struct{}, p.concurrency)
	resultCh := make(chan Result, len(ips))
	var wg sync.WaitGroup

	for _, ip := range ips {
		wg.Add(1)
		go func(ipAddr string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := p.pingIP(ctx, ipAddr, domain)
			resultCh <- *res
		}(ip)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	results := make([]Result, 0, len(ips))
	for r := range resultCh {
		results = append(results, r)
	}
	return results
}

// sortResults 综合得分排序（推荐）
// 排序规则：RTT + Loss*权重 + 探测方法权重 + IP失效权重
// 权重为 30，表示 1% 丢包相当于 30ms 延迟（从 18 提高到 30，加强对不稳定 IP 的惩罚）
// 探测方法权重：ICMP(0) < TCP(100) < HTTP(300) < UDP(500)
func (p *Pinger) sortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		// 计算实际失效次数（从百分比还原，用于阶梯式惩罚）
		failCountI := int(results[i].Loss*float64(p.count)/100.0 + 0.5)
		failCountJ := int(results[j].Loss*float64(p.count)/100.0 + 0.5)

		// 1. 基础得分：真实 RTT + 强力失效率惩罚（每次失败加 2000ms）
		// 这样 1 次丢包（即使 RTT 只有 10ms）也会排在 0 丢包（即使 RTT 是 1000ms）的后面
		scoreI := results[i].RTT + failCountI*2000
		scoreJ := results[j].RTT + failCountJ*2000

		// 2. 根据探测方法增加权重惩罚
		scoreI += p.getProbeMethodPenalty(results[i].ProbeMethod)
		scoreJ += p.getProbeMethodPenalty(results[j].ProbeMethod)

		// 3. 加入历史 IP 失效权重（带有衰减）
		if p.failureWeightMgr != nil {
			scoreI += p.failureWeightMgr.GetWeight(results[i].IP)
			scoreJ += p.failureWeightMgr.GetWeight(results[j].IP)
		}

		// 4. 5ms 分箱（Binning）去噪：消除 1ms 级别的随机波动
		roundedScoreI := (scoreI / 5) * 5
		roundedScoreJ := (scoreJ / 5) * 5

		if roundedScoreI != roundedScoreJ {
			return roundedScoreI < roundedScoreJ
		}

		// 5. 最终稳定性保障：如果分箱得分相同，按 IP 字符串字典序排
		// 这保证了只要网络质量微差，结果绝对不会上下跳变
		return results[i].IP < results[j].IP
	})
}

// getProbeMethodPenalty 根据探测方法返回权重惩罚
// ICMP 最优（权重 0），TCP 次优（权重 100），HTTP 备选（权重 300），UDP 最差（权重 500）
func (p *Pinger) getProbeMethodPenalty(method string) int {
	switch method {
	case "icmp":
		return 0 // 无惩罚，最优
	case "tls", "tcp443":
		return 100 // TCP 增加 100ms
	case "tcp80":
		return 300 // HTTP 增加 300ms
	case "udp53":
		return 500 // UDP 增加 500ms
	case "none":
		return 999999 // 完全失败
	default:
		return 0
	}
}
