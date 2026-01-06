package resolver

import (
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"github.com/miekg/dns"
)

// StatsHandler 统计数据查询处理器
type StatsHandler struct {
	stats *Stats
}

// NewStatsHandler 创建新的统计查询处理器
func NewStatsHandler(stats *Stats) *StatsHandler {
	return &StatsHandler{
		stats: stats,
	}
}

// HandleStatsQuery 处理统计数据查询
// 特殊DNS查询: QNAME="stats.resolver.local", QTYPE=TXT
// 返回JSON格式的统计数据
func (h *StatsHandler) HandleStatsQuery(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Compress = false

	// 获取统计数据
	statsData := h.GetStatsJSON()

	// 创建TXT记录，将JSON数据分割成多个255字节的段
	// DNS TXT记录的限制是每个字符串最多255字节
	segments := h.splitJSON(statsData, 255)
	for _, segment := range segments {
		rr := &dns.TXT{
			Hdr: dns.RR_Header{
				Name:   r.Question[0].Name,
				Rrtype: dns.TypeTXT,
				Class:  dns.ClassINET,
				Ttl:    0,
			},
			Txt: []string{segment},
		}
		m.Answer = append(m.Answer, rr)
	}

	w.WriteMsg(m)
}

// GetStatsJSON 获取JSON格式的统计数据
func (h *StatsHandler) GetStatsJSON() string {
	totalQueries := h.stats.GetTotalQueries()
	successQueries := h.stats.GetSuccessQueries()
	failedQueries := h.stats.GetFailedQueries()
	cacheHits := h.stats.GetCacheHits()
	cacheMisses := h.stats.GetCacheMisses()

	// 计算缓存命中率
	var cacheHitRate float64
	totalCacheAccess := cacheHits + cacheMisses
	if totalCacheAccess > 0 {
		cacheHitRate = float64(cacheHits) / float64(totalCacheAccess) * 100
	}

	// 计算成功率
	var successRate float64
	if totalQueries > 0 {
		successRate = float64(successQueries) / float64(totalQueries) * 100
	}

	// 获取内存统计
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// 计算运行时间
	uptime := time.Since(h.stats.startTime)

	statsMap := map[string]interface{}{
		"total_queries":   totalQueries,
		"success_queries": successQueries,
		"failed_queries":  failedQueries,
		"success_rate":    fmt.Sprintf("%.2f", successRate),
		"cache_hits":      cacheHits,
		"cache_misses":    cacheMisses,
		"cache_hit_rate":  fmt.Sprintf("%.2f", cacheHitRate),
		"avg_latency_ms":  fmt.Sprintf("%.2f", h.stats.GetAverageLatency()),
		"uptime_seconds":  int64(uptime.Seconds()),
		"memory_alloc_mb": memStats.Alloc / 1024 / 1024,
		"memory_total_mb": memStats.TotalAlloc / 1024 / 1024,
		"goroutines":      runtime.NumGoroutine(),
	}

	data, err := json.Marshal(statsMap)
	if err != nil {
		return `{"error":"failed to marshal stats"}`
	}

	return string(data)
}

// splitJSON 将JSON字符串分割成多个段
func (h *StatsHandler) splitJSON(data string, maxLen int) []string {
	var segments []string
	for len(data) > maxLen {
		segments = append(segments, data[:maxLen])
		data = data[maxLen:]
	}
	if len(data) > 0 {
		segments = append(segments, data)
	}
	return segments
}

// IsStatsQuery 检查是否是统计查询
func IsStatsQuery(r *dns.Msg) bool {
	if len(r.Question) == 0 {
		return false
	}

	q := r.Question[0]
	// 检查QNAME是否为"stats.resolver.local."
	return q.Name == "stats.resolver.local." && q.Qtype == dns.TypeTXT
}
