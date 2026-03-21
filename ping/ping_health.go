package ping

import "smartdnssort/connectivity"

// RecordIPFailure 记录 IP 失效（应用层调用）
// 熔断：断网时不记录，避免权重污染
func (p *Pinger) RecordIPFailure(ip string) {
	// 断网时不记录失效权重，防止误判
	if p.healthChecker != nil && !p.healthChecker.IsNetworkHealthy() {
		return
	}
	if p.failureWeightMgr != nil {
		p.failureWeightMgr.RecordFailure(ip)
	}
}

// RecordIPSuccess 记录 IP 成功（应用层调用）
// 熔断：断网时不记录，避免权重污染
func (p *Pinger) RecordIPSuccess(ip string) {
	// 断网时不记录成功权重，保持一致性
	if p.healthChecker != nil && !p.healthChecker.IsNetworkHealthy() {
		return
	}
	if p.failureWeightMgr != nil {
		p.failureWeightMgr.RecordSuccess(ip)
	}
}

// RecordIPFastFail 记录 IP 快速失败（第一次探测就超时）
// 熔断：断网时不记录，避免权重污染
func (p *Pinger) RecordIPFastFail(ip string) {
	// 断网时不记录快速失败，防止绕过 RecordIPFailure 保护
	if p.healthChecker != nil && !p.healthChecker.IsNetworkHealthy() {
		return
	}
	if p.failureWeightMgr != nil {
		p.failureWeightMgr.RecordFastFail(ip)
	}
}

// SaveIPFailureWeights 保存 IP 失效权重到磁盘
func (p *Pinger) SaveIPFailureWeights() error {
	if p.failureWeightMgr != nil {
		return p.failureWeightMgr.SaveToDisk()
	}
	return nil
}

// GetIPFailureRecord 获取 IP 的失效记录
func (p *Pinger) GetIPFailureRecord(ip string) *IPFailureRecord {
	if p.failureWeightMgr != nil {
		return p.failureWeightMgr.GetRecord(ip)
	}
	return &IPFailureRecord{IP: ip}
}

// GetAllIPFailureRecords 获取所有 IP 的失效记录
func (p *Pinger) GetAllIPFailureRecords() []*IPFailureRecord {
	if p.failureWeightMgr != nil {
		return p.failureWeightMgr.GetAllRecords()
	}
	return nil
}

// SetHealthChecker 设置网络健康检查器
// 用于断网时防止缓存污染
func (p *Pinger) SetHealthChecker(checker connectivity.NetworkHealthChecker) {
	p.healthChecker = checker
}

// ClearIPFailureWeights 清空所有 IP 失效权重记录
// 用于测试场景，确保排序结果不受历史权重影响
func (p *Pinger) ClearIPFailureWeights() {
	if p.failureWeightMgr != nil {
		p.failureWeightMgr.Clear()
	}
}

// IsNetworkOnline 返回网络是否在线
// 供 IPMonitor 使用，用于判断是否跳过当前刷新周期
func (p *Pinger) IsNetworkOnline() bool {
	if p.healthChecker == nil {
		// 如果没有设置健康检查器，默认认为网络在线
		return true
	}
	return p.healthChecker.IsNetworkHealthy()
}
