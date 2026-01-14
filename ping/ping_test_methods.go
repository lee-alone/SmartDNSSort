package ping

import "context"

// pingIP 单个 IP 多次测试 + 惩罚机制
// 执行多次 smartPing 测试，计算平均 RTT 和丢包率
// 对丢包进行惩罚以降低不稳定节点的优先级
func (p *Pinger) pingIP(ctx context.Context, ip, domain string) *Result {
	var totalRTT int64 = 0
	minRTT := 999999
	successCount := 0
	probeMethod := ""

	for i := 0; i < p.count; i++ {
		rtt, method := p.smartPingWithMethod(ctx, ip, domain)
		if rtt >= 0 {
			totalRTT += int64(rtt)
			successCount++
			if rtt < minRTT {
				minRTT = rtt
			}
			// 记录第一次成功的探测方法
			if probeMethod == "" {
				probeMethod = method
			}
		}
	}

	if successCount == 0 {
		return &Result{IP: ip, RTT: 999999, Loss: 100, ProbeMethod: "none"}
	}

	avgRTT := int(totalRTT / int64(successCount))

	return &Result{
		IP:          ip,
		RTT:         avgRTT, // 返回真实平均 RTT，不再预加惩罚
		Loss:        float64(p.count-successCount) / float64(p.count) * 100,
		ProbeMethod: probeMethod,
	}
}
