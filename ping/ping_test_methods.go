package ping

import (
	"context"
	"smartdnssort/logger"
)

// pingIP 单个 IP 多次测试 + 惩罚机制 + 快速失败（纯 ICMP 模式）
// 执行多次 ICMP ping 测试，计算平均 RTT 和丢包率
// 对丢包进行惩罚以降低不稳定节点的优先级
//
// 纯 ICMP 模式优化：
// - 区分"路不通"与"层不通"
// - 如果 ICMP 失败是因为"权限拒绝"或"协议不支持"，严禁触发 FastFail
// - 只有真正的网络超时才应该触发 FastFail
// - 建议增加测试次数（Count=3-5）来提高准确性
func (p *Pinger) pingIP(ctx context.Context, ip, domain string) *Result {
	var totalRTT int64 = 0
	minRTT := LogicDeadRTT
	successCount := 0
	probeMethod := ""
	icmpPermissionError := false // 标记 ICMP 是否因权限问题失败

	for i := 0; i < p.count; i++ {
		rtt, method, icmpErr := p.smartPingWithMethod(ctx, ip, domain)
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
		} else {
			// 检查 ICMP 是否因权限问题失败
			// 如果是权限问题，不应该触发 FastFail
			if i == 0 {
				if icmpErr != nil && (icmpErr.IsPermissionError || icmpErr.IsProtocolError) {
					icmpPermissionError = true
					logger.Debugf("[Pinger] ICMP failed for %s due to permission/protocol error, skipping FastFail: %v", ip, icmpErr.Err)
					// 不触发 FastFail，继续尝试后续探测
					continue
				}

				// 如果不是权限问题，触发快速失败机制
				p.RecordIPFastFail(ip)
				// 直接返回完全失败的结果，不再进行后续探测
				// FastFail=true 标记，避免在 PingAndSort 中重复记录
				return &Result{IP: ip, RTT: LogicDeadRTT, Loss: 100, ProbeMethod: "icmp", FastFail: true}
			}
		}
	}

	if successCount == 0 {
		// 如果所有探测都失败，但 ICMP 是因权限问题失败，不应该标记为 FastFail
		if icmpPermissionError {
			logger.Debugf("[Pinger] All probes failed for %s, but ICMP had permission error, not marking as FastFail", ip)
			return &Result{IP: ip, RTT: LogicDeadRTT, Loss: 100, ProbeMethod: "icmp", FastFail: false}
		}
		return &Result{IP: ip, RTT: LogicDeadRTT, Loss: 100, ProbeMethod: "icmp", FastFail: false}
	}

	avgRTT := int(totalRTT / int64(successCount))

	return &Result{
		IP:          ip,
		RTT:         avgRTT, // 返回真实平均 RTT，不再预加惩罚
		Loss:        float64(p.count-successCount) / float64(p.count) * 100,
		ProbeMethod: probeMethod,
		FastFail:    false,
	}
}
