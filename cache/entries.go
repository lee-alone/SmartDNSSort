package cache

import (
	"time"

	"github.com/miekg/dns"
)

// RawCacheEntry 原始缓存项（上游 DNS 的原始响应）
type RawCacheEntry struct {
	IPs               []string  // 原始 IP 列表
	CNAMEs            []string  // CNAME 记录列表（支持多级 CNAME）
	UpstreamTTL       uint32    // 上游 DNS 返回的原始 TTL（秒）
	AcquisitionTime   time.Time // 从上游获取的时间
	AuthenticatedData bool      // DNSSEC 验证标记 (AD flag)
}

// IsExpired 检查原始缓存是否过期
func (e *RawCacheEntry) IsExpired() bool {
	elapsed := time.Since(e.AcquisitionTime).Seconds()
	return elapsed > float64(e.UpstreamTTL)
}

// SortedCacheEntry 排序后的缓存项
type SortedCacheEntry struct {
	IPs       []string  // 排序后的 IP 列表
	RTTs      []int     // 对应的 RTT（毫秒）
	Timestamp time.Time // 排序完成时间
	TTL       int       // TTL（秒）
	IsValid   bool      // 排序是否有效
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
