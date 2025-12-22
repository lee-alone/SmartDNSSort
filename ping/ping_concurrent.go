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
// 排序规则：RTT + Loss*权重 + IP失效权重
// 权重为 18，表示 1% 丢包相当于 18ms 延迟
func (p *Pinger) sortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		scoreI := results[i].RTT + int(results[i].Loss*18) // 权重可调
		scoreJ := results[j].RTT + int(results[j].Loss*18)

		// 加入IP失效权重
		if p.failureWeightMgr != nil {
			scoreI += p.failureWeightMgr.GetWeight(results[i].IP)
			scoreJ += p.failureWeightMgr.GetWeight(results[j].IP)
		}

		if scoreI != scoreJ {
			return scoreI < scoreJ
		}
		return results[i].IP < results[j].IP
	})
}
