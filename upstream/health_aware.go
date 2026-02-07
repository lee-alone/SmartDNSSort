package upstream

import (
	"context"
	"time"

	"github.com/miekg/dns"
)

// HealthAwareUpstream 带健康检查的上游服务器包装器
type HealthAwareUpstream struct {
	// 底层上游服务器
	upstream Upstream

	// 健康状态管理器
	health *ServerHealth
}

// NewHealthAwareUpstream 创建带健康检查的上游服务器
// statsConfig: 统计配置，用于动态计算上游统计的桶数量
func NewHealthAwareUpstream(upstream Upstream, healthConfig *HealthCheckConfig, statsConfig *StatsConfig) *HealthAwareUpstream {
	return &HealthAwareUpstream{
		upstream: upstream,
		health:   NewServerHealth(upstream.Address(), healthConfig, statsConfig),
	}
}

// Exchange 执行 DNS 查询，并记录延迟
func (h *HealthAwareUpstream) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	startTime := time.Now()
	reply, err := h.upstream.Exchange(ctx, msg)
	latency := time.Since(startTime)

	// 记录延迟用于动态参数优化
	// 注意：成功/失败的标记由 manager 中的 RecordSuccess/RecordError 调用处理
	// 以避免重复计数
	if err == nil && reply != nil {
		if reply.Rcode == dns.RcodeSuccess || reply.Rcode == dns.RcodeNameError {
			// 查询成功，记录延迟
			h.health.RecordLatency(latency)
		}
	}

	return reply, err
}

// Address 返回服务器地址
func (h *HealthAwareUpstream) Address() string {
	return h.upstream.Address()
}

// Protocol 返回协议类型
func (h *HealthAwareUpstream) Protocol() string {
	return h.upstream.Protocol()
}

// ShouldSkipTemporarily 判断是否应该临时跳过此服务器
func (h *HealthAwareUpstream) ShouldSkipTemporarily() bool {
	return h.health.ShouldSkipTemporarily()
}

// GetHealth 获取健康状态管理器（用于统计）
func (h *HealthAwareUpstream) GetHealth() *ServerHealth {
	return h.health
}

// MarkSuccess 手动标记成功（用于特殊情况）
func (h *HealthAwareUpstream) MarkSuccess() {
	h.health.MarkSuccess()
}

// MarkFailure 手动标记失败（用于特殊情况）
func (h *HealthAwareUpstream) MarkFailure() {
	h.health.MarkFailure()
}

// RecordSuccess 记录一次成功的查询
func (h *HealthAwareUpstream) RecordSuccess() {
	h.health.MarkSuccess()
}

// RecordError 记录一次通用错误
func (h *HealthAwareUpstream) RecordError() {
	h.health.MarkFailure()
}

// RecordTimeout 记录一次超时
func (h *HealthAwareUpstream) RecordTimeout() {
	h.health.MarkTimeout(0)
}

// Name 返回服务器名称（地址）
func (h *HealthAwareUpstream) Name() string {
	return h.upstream.Address()
}

// Query 执行 DNS 查询并返回结果
func (h *HealthAwareUpstream) Query(ctx context.Context) (interface{}, error) {
	// 这个方法用于 sequential 和 racing 策略
	// 返回 *dns.Msg 作为查询结果
	return h.upstream.Exchange(ctx, &dns.Msg{})
}
