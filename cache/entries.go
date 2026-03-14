package cache

import (
	"time"

	"github.com/miekg/dns"
)

// CacheEntryState 缓存条目状态（三段式）
type CacheEntryState int

const (
	// Fresh 新鲜状态：now < AcquisitionTime + EffectiveTTL
	FRESH CacheEntryState = iota
	// Stale 陈旧状态：now >= AcquisitionTime + EffectiveTTL 但 now < AcquisitionTime + EffectiveTTL + GracePeriod
	STALE
	// Expired 彻底过期：now >= AcquisitionTime + EffectiveTTL + GracePeriod
	EXPIRED
)

// RawCacheEntry 原始缓存项（上游 DNS 的原始响应）
type RawCacheEntry struct {
	Records           []dns.RR  // 通用记录列表（所有类型的 DNS 记录）
	IPs               []string  // 原始 IP 列表（Records 中 A/AAAA 记录的物化视图）
	CNAMEs            []string  // CNAME 记录列表（支持多级 CNAME）
	UpstreamTTL       uint32    // 上游 DNS 返回的原始 TTL（秒）
	EffectiveTTL      uint32    // 实际内部缓存使用的 TTL（秒），应用了本地 min/max 策略
	AcquisitionTime   time.Time // 从上游获取的时间
	AuthenticatedData bool      // DNSSEC 验证标记 (AD flag)
	QueryVersion      int64     // 查询版本号，用于防止旧的后台补全覆盖新的缓存

	// 第二阶段改造：Stale-While-Revalidate 支持
	gracePeriod uint32 // 软过期容忍期（秒），用于 Stale-While-Revalidate
}

// IsExpired 检查原始缓存是否过期（使用 EffectiveTTL）
// 向后兼容方法
func (e *RawCacheEntry) IsExpired() bool {
	elapsed := time.Since(e.AcquisitionTime).Seconds()
	return elapsed > float64(e.EffectiveTTL)
}

// GetState 获取缓存条目的当前状态（三段式判定）
// - Fresh: now < AcquisitionTime + EffectiveTTL
// - Stale: now >= AcquisitionTime + EffectiveTTL 但 now < AcquisitionTime + EffectiveTTL + GracePeriod
// - Expired: now >= AcquisitionTime + EffectiveTTL + GracePeriod
func (e *RawCacheEntry) GetState(gracePeriodSeconds uint32) CacheEntryState {
	now := time.Now()
	expiresAt := e.AcquisitionTime.Add(time.Duration(e.EffectiveTTL) * time.Second)
	graceExpiresAt := expiresAt.Add(time.Duration(gracePeriodSeconds) * time.Second)

	if now.Before(expiresAt) {
		// Fresh: 未过期，可以直接返回
		return FRESH
	} else if now.Before(graceExpiresAt) {
		// Stale: 处于软过期期间，返回旧数据并异步刷新
		return STALE
	} else {
		// Expired: 彻底过期，需要重新查询上游
		return EXPIRED
	}
}

// GetEffectiveGracePeriod 获取有效的软过期容忍期
// 如果配置了 KeepExpiredEntries，则使用配置的 GracePeriod；否则直接返回 0
func (e *RawCacheEntry) GetEffectiveGracePeriod(keepExpired bool, gracePeriodSeconds uint32) uint32 {
	if !keepExpired {
		return 0
	}
	return gracePeriodSeconds
}

// GetRemainingTTL 获取剩余 TTL（秒）
func (e *RawCacheEntry) GetRemainingTTL() int32 {
	elapsed := time.Since(e.AcquisitionTime).Seconds()
	remaining := float64(e.EffectiveTTL) - elapsed
	if remaining < 0 {
		return 0
	}
	return int32(remaining)
}

// GetStateWithConfig 根据配置获取缓存状态
func (e *RawCacheEntry) GetStateWithConfig(keepExpired bool, gracePeriodSeconds uint32) CacheEntryState {
	if !keepExpired {
		// 不允许保留过期条目时，直接使用 IsExpired 判断
		if e.IsExpired() {
			return EXPIRED
		}
		return FRESH
	}
	return e.GetState(gracePeriodSeconds)
}

// SortedCacheEntry 排序后的缓存项
type SortedCacheEntry struct {
	IPs          []string  // 排序后的 IP 列表
	RTTs         []int     // 对应的 RTT（毫秒）
	Timestamp    time.Time // 排序完成时间
	TTL          int       // TTL（秒）
	IsValid      bool      // 排序是否有效
	QueryVersion int64     // 查询版本号，用于防止旧的排序覆盖新的排序
}

// IsExpired 检查排序缓存是否过期
func (e *SortedCacheEntry) IsExpired() bool {
	if !e.IsValid {
		return true
	}
	return time.Since(e.Timestamp).Seconds() > float64(e.TTL)
}

// SortingState 表示某个域名的排序状态
type SortingState struct {
	InProgress bool              // 是否正在排序
	Done       chan struct{}     // 排序完成信号
	Result     *SortedCacheEntry // 排序结果
	Error      error             // 排序错误
}

// ErrorCacheEntry 错误响应缓存项（用于缓存 DNS 错误响应）
type ErrorCacheEntry struct {
	Rcode    int       // DNS 错误码（SERVFAIL, REFUSED 等）
	CachedAt time.Time // 缓存时间
	TTL      int       // 缓存 TTL（秒）
}

// IsExpired 检查错误缓存是否过期
func (e *ErrorCacheEntry) IsExpired() bool {
	return time.Since(e.CachedAt).Seconds() > float64(e.TTL)
}

// DNSSECCacheEntry 代表一个 DNSSEC 缓存条目
// 存储完整的 DNS 消息及其过期时间
type DNSSECCacheEntry struct {
	Message         *dns.Msg  // 完整的 DNS 响应消息
	AcquisitionTime time.Time // 获取时间
	TTL             uint32    // 消息 TTL（秒）
}

// IsExpired 检查缓存条目是否已过期
func (e *DNSSECCacheEntry) IsExpired() bool {
	elapsed := time.Since(e.AcquisitionTime).Seconds()
	return elapsed > float64(e.TTL)
}

// PersistentCacheEntry 用于持久化的缓存项
type PersistentCacheEntry struct {
	Domain string   `json:"domain"`
	QType  uint16   `json:"qtype"`
	IPs    []string `json:"ips"`
	CNAME  string   `json:"cname,omitempty"`  // 旧版本兼容
	CNAMEs []string `json:"cnames,omitempty"` // 新版本字段
}
