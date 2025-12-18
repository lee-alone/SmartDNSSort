package ping

import "context"

// pingIP 单个 IP 多次测试 + 惩罚机制
// 执行多次 smartPing 测试，计算平均 RTT 和丢包率
// 对丢包进行惩罚以降低不稳定节点的优先级
func (p *Pinger) pingIP(ctx context.Context, ip, domain string) *Result {
	var totalRTT int64 = 0
	minRTT := 999999
	successCount := 0

	for i := 0; i < p.count; i++ {
		rtt := p.smartPing(ctx, ip, domain)
		if rtt >= 0 {
			totalRTT += int64(rtt)
			successCount++
			if rtt < minRTT {
				minRTT = rtt
			}
		}
	}

	if successCount == 0 {
		return &Result{IP: ip, RTT: 999999, Loss: 100}
	}

	avgRTT := int(totalRTT / int64(successCount))
	penalty := (p.count - successCount) * 150 // 惩罚降低一点，防止误伤
	finalRTT := avgRTT + penalty
	if finalRTT > 5000 {
		finalRTT = 5000
	}

	return &Result{
		IP:   ip,
		RTT:  finalRTT,
		Loss: float64(p.count-successCount) / float64(p.count) * 100,
	}
}
