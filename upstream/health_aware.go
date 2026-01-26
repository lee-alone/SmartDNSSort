package upstream

import (
	"context"
	"errors"
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
func NewHealthAwareUpstream(upstream Upstream, healthConfig *HealthCheckConfig) *HealthAwareUpstream {
	return &HealthAwareUpstream{
		upstream: upstream,
		health:   NewServerHealth(upstream.Address(), healthConfig),
	}
}

// Exchange 执行 DNS 查询，并记录健康状态
func (h *HealthAwareUpstream) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	startTime := time.Now()
	reply, err := h.upstream.Exchange(ctx, msg)
	latency := time.Since(startTime)

	// 根据查询结果更新健康状态
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			h.health.MarkTimeout(latency)
		} else {
			h.health.MarkFailure()
		}
		return nil, err
	}

	// 检查 DNS 响应码
	if reply.Rcode != dns.RcodeSuccess && reply.Rcode != dns.RcodeNameError {
		// SERVFAIL, REFUSED 等错误码视为失败
		h.health.MarkFailure()
	} else {
		// RcodeSuccess 和 RcodeNameError (NXDOMAIN) 都视为成功
		// 查询成功，记录延迟
		h.health.RecordLatency(latency)
		h.health.MarkSuccess()
	}

	return reply, nil
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
