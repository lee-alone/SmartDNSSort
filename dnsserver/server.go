package dnsserver

import (
	"sync"

	"smartdnssort/adblock"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"smartdnssort/prefetch"
	"smartdnssort/recursor"
	"smartdnssort/stats"
	"smartdnssort/upstream"

	"github.com/miekg/dns"
)

// Server DNS 服务器
// Note: Fields are used across multiple files in this package (handler*.go, sorting.go, refresh.go, tasks.go)
// Linter warnings about unused fields are expected and can be safely ignored.
type Server struct {
	mu                 sync.RWMutex
	cfg                *config.Config
	stats              *stats.Stats
	cache              *cache.Cache
	msgPool            *cache.MsgPool       // Used in: handler_query.go, handler_cache.go, handler_response.go - DNS 消息对象池
	upstream           *upstream.Manager    // Used in: handler_query.go, handler_cname.go, refresh.go, server_config.go
	pinger             *ping.Pinger         // Used in: sorting.go, server_config.go
	sortQueue          *cache.SortQueue     // Used in: sorting.go, server_lifecycle.go, server_config.go
	prefetcher         *prefetch.Prefetcher // Used in: sorting.go, handler_cache.go, handler_query.go, server_lifecycle.go, server_config.go
	refreshQueue       *RefreshQueue        // Used in: handler_cache.go, refresh.go, server_lifecycle.go, server_config.go
	recentQueries      [20]string           // Circular buffer for recent queries
	recentQueriesIndex int
	recentQueriesMu    sync.Mutex
	udpServer          *dns.Server             // Used in: server_lifecycle.go
	tcpServer          *dns.Server             // Used in: server_lifecycle.go
	adblockManager     *adblock.AdBlockManager // 广告拦截管理器
	customRespManager  *CustomResponseManager  // 自定义回复管理器
	recursorMgr        *recursor.Manager       // 嵌入式递归解析器管理器
	stopCh             chan struct{}           // 用于优雅关闭后台 goroutine
	sortSemaphore      chan struct{}           // 限制并发排序任务数量（最多 50 个）
}

// GetCustomResponseManager returns the custom response manager instance
func (s *Server) GetCustomResponseManager() *CustomResponseManager {
	return s.customRespManager
}

// GetStats 获取统计信息
func (s *Server) GetStats() map[string]interface{} {
	s.mu.RLock()
	st := s.stats.GetStats()
	if s.upstream != nil {
		st["upstream_dynamic_params"] = s.upstream.GetDynamicParamStats()
	}
	s.mu.RUnlock()
	return st
}

// CalculateEvictionsPerMinute 计算每分钟的驱逐率
func (s *Server) CalculateEvictionsPerMinute(currentEvictionCount int64) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats.GetEvictionsPerMinute(currentEvictionCount)
}

// ClearStats clears all collected statistics.
func (s *Server) ClearStats() {
	s.mu.Lock()
	defer s.mu.Unlock()
	logger.Info("Clearing all statistics via API request.")
	s.stats.Reset()

	// 清除上游服务器的统计数据
	if s.upstream != nil {
		s.upstream.ClearStats()
	}
}

// RecordRecentQuery adds a domain to the recent queries list.
func (s *Server) RecordRecentQuery(domain string) {
	s.recentQueriesMu.Lock()
	defer s.recentQueriesMu.Unlock()

	s.recentQueries[s.recentQueriesIndex] = domain
	s.recentQueriesIndex = (s.recentQueriesIndex + 1) % len(s.recentQueries)
}

// GetRecentQueries returns a slice of the most recent queries.
func (s *Server) GetRecentQueries() []string {
	s.recentQueriesMu.Lock()
	defer s.recentQueriesMu.Unlock()

	// The buffer is circular, so we need to reconstruct the order.
	// The oldest element is at `s.recentQueriesIndex`.
	var orderedQueries []string
	for i := 0; i < len(s.recentQueries); i++ {
		idx := (s.recentQueriesIndex + i) % len(s.recentQueries)
		if s.recentQueries[idx] != "" {
			orderedQueries = append(orderedQueries, s.recentQueries[idx])
		}
	}
	// Reverse to get the most recent first
	for i, j := 0, len(orderedQueries)-1; i < j; i, j = i+1, j-1 {
		orderedQueries[i], orderedQueries[j] = orderedQueries[j], orderedQueries[i]
	}

	return orderedQueries
}

// GetCache 获取缓存实例（供 WebAPI 使用）
func (s *Server) GetCache() *cache.Cache {
	return s.cache
}

// GetConfig returns the current server configuration.
func (s *Server) GetConfig() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy to prevent race conditions if the caller modifies it
	cfgCopy := *s.cfg
	return &cfgCopy
}

// GetAdBlockManager returns the adblock manager instance.
func (s *Server) GetAdBlockManager() *adblock.AdBlockManager {
	return s.adblockManager
}

// SetAdBlockEnabled dynamically enables or disables AdBlock filtering
func (s *Server) SetAdBlockEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cfg.AdBlock.Enable = enabled
	logger.Infof("[AdBlock] Filtering status changed to: %v", enabled)
}

// GetRecursorManager returns the recursor manager instance
func (s *Server) GetRecursorManager() *recursor.Manager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.recursorMgr
}

// GetUpstreamManager returns the upstream manager instance
func (s *Server) GetUpstreamManager() *upstream.Manager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.upstream
}
