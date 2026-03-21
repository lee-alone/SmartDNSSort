package ping

import (
	"context"
	"math"
	"sort"
	"sync"
)

// concurrentPing 并发测试多个 IP（纯 ICMP 模式）
// 使用 Worker Pool 模式替代 goroutine-per-IP，减少大批量 IP 时的 goroutine 开销
// 使用 SingleFlight 合并对同一 IP 的重复探测请求
func (p *Pinger) concurrentPing(ctx context.Context, ips []string, _ string) []Result {
	if len(ips) == 0 {
		return nil
	}

	resultCh := make(chan Result, len(ips))
	ipCh := make(chan string, len(ips))

	// 启动固定数量的 worker goroutine
	// 而不是为每个 IP 创建一个 goroutine，减少 goroutine 创建销毁的开销
	var wg sync.WaitGroup
	for i := 0; i < p.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 每个 worker 从 ipCh 中获取任务，直到 channel 关闭
			for ipAddr := range ipCh {
				// 使用 SingleFlight 合并对同一 IP 的探测请求
				// domain 参数保留用于未来可能的扩展，但当前纯 ICMP 模式下不使用
				// 如果多个 goroutine 同时对同一 IP 发起探测，只有第一个会执行真正的探测
				// 其他的会等待第一个的结果
				key := ipAddr
				v, err, _ := p.probeFlight.Do(key, func() (interface{}, error) {
					res := p.pingIP(ctx, ipAddr, "")
					return res, nil
				})

				if err == nil && v != nil {
					resultCh <- *(v.(*Result))
				}
			}
		}()
	}

	// 分发任务到 ipCh
	go func() {
		for _, ip := range ips {
			ipCh <- ip
		}
		close(ipCh)
	}()

	// 等待所有 worker 完成
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 收集结果
	results := make([]Result, 0, len(ips))
	for r := range resultCh {
		results = append(results, r)
	}
	return results
}

// sortResults 综合得分排序（纯 ICMP 模式）
// 排序规则：RTT + Loss*权重 + IP失效权重
// 权重为 30，表示 1% 丢包相当于 30ms 延迟（从 18 提高到 30，加强对不稳定 IP 的惩罚）
//
// 纯 ICMP 模式优化：
// - 移除探测方法权重（所有 IP 都使用 ICMP 探测）
// - 保持 2000ms 强力惩罚：每次失败加 2000ms，确保丢包 IP 排在后面
// - 建议增加测试次数（Count=3-5）来替代多协议兜底
// - 绝对 RTT 竞争：直接基于积分原始值排序，无分箱，实现最灵敏的速度优先
func (p *Pinger) sortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		// 计算实际失效次数（从百分比还原，用于阶梯式惩罚）
		// 修复 #3：使用 math.Round 替代手动四舍五入，避免浮点精度问题
		failCountI := int(math.Round(results[i].Loss * float64(p.count) / 100.0))
		failCountJ := int(math.Round(results[j].Loss * float64(p.count) / 100.0))

		// 1. 基础得分：真实 RTT + 强力失效率惩罚（每次失败加 2000ms）
		// 这样 1 次丢包（即使 RTT 只有 10ms）也会排在 0 丢包（即使 RTT 是 1000ms）的后面
		scoreI := results[i].RTT + failCountI*2000
		scoreJ := results[j].RTT + failCountJ*2000

		// 2. 加入历史 IP 失效权重（带有衰减）
		if p.failureWeightMgr != nil {
			scoreI += p.failureWeightMgr.GetWeight(results[i].IP)
			scoreJ += p.failureWeightMgr.GetWeight(results[j].IP)
		}

		// 3. 绝对 RTT 竞争：直接比对积分原始值，无分箱
		if scoreI != scoreJ {
			return scoreI < scoreJ
		}

		// 4. 平局决胜：只有 RTT 完全相等时，才使用 IP 字符串字典序兜底
		// 确保响应的确定性，防止顺序在多次请求间跳变
		return results[i].IP < results[j].IP
	})
}
