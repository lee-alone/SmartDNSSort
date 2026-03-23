package webapi

import (
	"encoding/json"
	"net/http"
	"smartdnssort/config"
	"smartdnssort/connectivity"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
	"gopkg.in/yaml.v3"
)

// handleQuery 处理 DNS 查询请求
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	queryType := r.URL.Query().Get("type")

	if domain == "" {
		s.writeJSONError(w, "Missing domain parameter", http.StatusBadRequest)
		return
	}
	if queryType == "" {
		queryType = "A"
	}

	var qtype uint16
	switch strings.ToUpper(queryType) {
	case "A":
		qtype = dns.TypeA
	case "AAAA":
		qtype = dns.TypeAAAA
	default:
		s.writeJSONError(w, "Invalid query type (must be A or AAAA)", http.StatusBadRequest)
		return
	}

	var ipsResult []IPResult
	var status string

	if sortedEntry, ok := s.dnsCache.GetSorted(domain, qtype); ok {
		status = "cached_sorted"
		for i, ip := range sortedEntry.IPs {
			rtt := 0
			if i < len(sortedEntry.RTTs) {
				rtt = sortedEntry.RTTs[i]
			}
			ipsResult = append(ipsResult, IPResult{IP: ip, RTT: rtt})
		}
	} else if rawEntry, ok := s.dnsCache.GetRaw(domain, qtype); ok {
		status = "cached_raw"
		for _, ip := range rawEntry.IPs {
			ipsResult = append(ipsResult, IPResult{IP: ip, RTT: 0})
		}
	}

	if len(ipsResult) == 0 {
		s.writeJSONError(w, "Domain not found in cache", http.StatusNotFound)
		return
	}

	result := QueryResult{
		Domain: domain,
		Type:   queryType,
		IPs:    ipsResult,
		Status: status,
	}

	w.Header().Set("Content-Type", "application/json")
	response := APIResponse{
		Success: true,
		Message: "Query result",
		Data:    result,
	}
	json.NewEncoder(w).Encode(response)
}

// handleStats 处理统计信息请求
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 1. 获取基础统计信息（包含系统状态和全量域名统计）
	stats := s.dnsServer.GetStats()

	// 2. 获取时间范围参数并覆盖计数型数据
	daysStr := r.URL.Query().Get("days")
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil {
			// 参数验证：只允许 1, 7, 30
			if d == 1 || d == 7 || d == 30 {
				rangeStats := s.dnsServer.GetStatsWithTimeRange(d)
				// 使用时间范围内的计数覆盖实时累计计数
				for k, v := range rangeStats {
					stats[k] = v
				}
			}
		}
	}

	// 3. 计算缓存内存统计（实时数据，不受时间范围影响）
	cacheCfg := s.dnsServer.GetConfig().Cache
	currentEntries := s.dnsCache.GetCurrentEntries()
	expiredEntries := s.dnsCache.GetExpiredEntries()
	maxEntries := cacheCfg.CalculateMaxEntries()

	// 计算采样的平均字节数
	avgBytesPerEntry := s.calculateAvgBytesPerEntry()

	// 计算驱逐率
	evictionsPerMin := s.calculateEvictionsPerMinute()

	var memoryPercent float64
	if maxEntries > 0 {
		memoryPercent = (float64(currentEntries) / float64(maxEntries)) * 100
	}

	var expiredPercent float64
	if currentEntries > 0 {
		expiredPercent = (float64(expiredEntries) / float64(currentEntries)) * 100
	}

	stats["cache_memory_stats"] = map[string]interface{}{
		"max_memory_mb":     cacheCfg.MaxMemoryMB,
		"max_entries":       maxEntries,
		"current_entries":   currentEntries,
		"current_memory_mb": int(float64(currentEntries) * float64(avgBytesPerEntry) / (1024 * 1024)),
		"memory_percent":    memoryPercent,
		"expired_entries":   expiredEntries,
		"expired_percent":   expiredPercent,
		"protected_entries": s.dnsCache.GetProtectedEntries(),
		"evictions_per_min": evictionsPerMin,
	}

	// 添加网络在线状态
	stats["network_online"] = connectivity.GetGlobalNetworkChecker().IsNetworkHealthy()

	// 使用统一的 API 响应格式
	s.writeJSONSuccess(w, "Statistics retrieved successfully", stats)
}

// handleCacheMemoryStats 处理缓存内存统计请求
func (s *Server) handleCacheMemoryStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	cacheCfg := s.dnsServer.GetConfig().Cache
	currentEntries := s.dnsCache.GetCurrentEntries()
	maxEntries := cacheCfg.CalculateMaxEntries()
	expiredEntries := s.dnsCache.GetExpiredEntries()
	protectedEntries := s.dnsCache.GetProtectedEntries()

	// 计算采样的平均字节数
	avgBytesPerEntry := s.calculateAvgBytesPerEntry()

	// 计算驱逐率
	evictionsPerMin := s.calculateEvictionsPerMinute()

	var memoryPercent float64
	if maxEntries > 0 {
		memoryPercent = (float64(currentEntries) / float64(maxEntries)) * 100
	}

	var expiredPercent float64
	if currentEntries > 0 {
		expiredPercent = (float64(expiredEntries) / float64(currentEntries)) * 100
	}

	stats := map[string]interface{}{
		"max_memory_mb":     cacheCfg.MaxMemoryMB,
		"max_entries":       maxEntries,
		"current_entries":   currentEntries,
		"current_memory_mb": int(float64(currentEntries) * float64(avgBytesPerEntry) / (1024 * 1024)),
		"memory_percent":    memoryPercent,
		"expired_entries":   expiredEntries,
		"expired_percent":   expiredPercent,
		"protected_entries": protectedEntries,
		"evictions_per_min": evictionsPerMin,
	}

	s.writeJSONSuccess(w, "Cache memory stats retrieved successfully", stats)
}

// handleHealth 处理健康检查请求
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSONSuccess(w, "Service is healthy", map[string]string{"status": "healthy"})
}

// handleClearCache 处理清空缓存请求
func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 清空内存缓存
	s.dnsCache.Clear()
	logger.Debug("DNS cache (memory) cleared via API request.")

	// 删除磁盘缓存文件
	cacheFile := "dns_cache.json"
	if err := s.deleteCacheFile(cacheFile); err != nil {
		logger.Errorf("Failed to delete cache file during API clear request: %v", err)
		s.writeJSONError(w, "Failed to clear disk cache: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSONSuccess(w, "Cache cleared successfully (memory and disk)", nil)
}

// handleClearStats 处理清空统计信息请求
func (s *Server) handleClearStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	s.dnsServer.ClearStats()
	logger.Debug("Statistics cleared via API request.")
	s.writeJSONSuccess(w, "All stats cleared successfully", nil)
}

// handleRecentQueries 处理最近查询请求
func (s *Server) handleRecentQueries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 获取时间范围参数
	daysStr := r.URL.Query().Get("days")
	days := 7 // 默认 7 天
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil {
			if d == 1 || d == 7 || d == 30 {
				days = d
			}
		}
	}

	// 获取指定时间范围内的最近查询
	queries := s.dnsServer.GetRecentQueriesWithTimeRange(days)
	if queries == nil {
		queries = []string{}
	}
	s.writeJSONSuccess(w, "Recent queries retrieved successfully", queries)
}

// handleHotDomains 处理热点域名请求
func (s *Server) handleHotDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	stats := s.dnsServer.GetStats()
	topDomainsList, ok := stats["top_domains"]
	if !ok || topDomainsList == nil {
		topDomainsList = []interface{}{}
	}
	s.writeJSONSuccess(w, "Hot domains retrieved successfully", topDomainsList)
}

// handleBlockedDomains 处理被拦截域名请求
func (s *Server) handleBlockedDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 获取时间范围参数
	daysStr := r.URL.Query().Get("days")
	days := 7 // 默认 7 天
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil {
			if d == 1 || d == 7 || d == 30 {
				days = d
			}
		}
	}

	// 获取指定时间范围内的被拦截域名
	stats := s.dnsServer.GetStatsWithTimeRange(days)
	topBlockedDomainsList, ok := stats["top_blocked_domains"]
	if !ok || topBlockedDomainsList == nil {
		topBlockedDomainsList = []interface{}{}
	}
	s.writeJSONSuccess(w, "Blocked domains retrieved successfully", topBlockedDomainsList)
}

// handleRestart 处理重启请求
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 加锁检查和设置重启状态
	s.restartMutex.Lock()
	if s.isRestarting {
		s.restartMutex.Unlock()
		s.writeJSONError(w, "Service restart is already in progress", http.StatusConflict)
		return
	}
	s.isRestarting = true
	s.restartMutex.Unlock()

	logger.Debug("Service restart requested via API.")
	s.writeJSONSuccess(w, "Service restart initiated", nil)

	if s.restartFunc != nil {
		go func() {
			defer func() {
				// 重启完成后重置标志
				s.restartMutex.Lock()
				s.isRestarting = false
				s.restartMutex.Unlock()
			}()
			logger.Debug("Executing restart function...")
			s.restartFunc()
		}()
	} else {
		// 没有重启函数时立即重置标志
		s.restartMutex.Lock()
		s.isRestarting = false
		s.restartMutex.Unlock()
		logger.Warn("No restart function configured. Please restart manually.")
	}
}

// handleRecentlyBlocked 处理最近被拦截的域名请求
func (s *Server) handleRecentlyBlocked(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 获取时间范围参数
	daysStr := r.URL.Query().Get("days")
	days := 7 // 默认 7 天
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil {
			if d == 1 || d == 7 || d == 30 {
				days = d
			}
		}
	}

	// 获取指定时间范围内的最近被拦截域名
	domains := s.dnsCache.GetRecentlyBlocked().GetAllWithTimeRange(days)
	if domains == nil {
		domains = []string{}
	}

	s.writeJSONSuccess(w, "Recently blocked domains retrieved successfully", domains)
}

// IPPoolResult IP 池结果
type IPPoolResult struct {
	IP         string `json:"ip"`
	RepDomain  string `json:"rep_domain"`
	RefCount   int    `json:"ref_count"`
	AccessHeat int64  `json:"access_heat"`
	RTT        int    `json:"rtt"`
	LastAccess string `json:"last_access"`
}

// IPPoolStatusResponse IP 池状态响应
type IPPoolStatusResponse struct {
	TotalIPs      int                    `json:"total_ips"`
	TotalRefCount int                    `json:"total_ref_count"`
	TotalHeat     int64                  `json:"total_heat"`
	LastUpdated   string                 `json:"last_updated"`
	MonitorStats  map[string]interface{} `json:"monitor_stats"`
	TopIPs        []IPPoolResult         `json:"top_ips"`
}

// handleIPPoolStatus 处理 IP 池状态请求
func (s *Server) handleIPPoolStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	response := IPPoolStatusResponse{
		TopIPs: []IPPoolResult{},
		// 初始化默认值，确保即使 ipMonitor 为 nil 也能返回有效数据
		MonitorStats: map[string]interface{}{
			"total_refreshes":     int64(0),
			"total_planned_pings": int64(0),
			"total_actual_pings":  int64(0),
			"total_skipped_pings": int64(0),
			"last_refresh_time":   time.Time{},
			"t0_pool_size":        0,
			"t1_pool_size":        0,
			"t2_pool_size":        0,
			"downgraded_ips":      0,
			"hourly_quota_used":   0,
			"hourly_quota_limit":  5000,
		},
	}

	// 获取 IP 池信息
	ipMonitor := s.dnsServer.GetIPMonitor()
	if ipMonitor != nil {
		// 获取 IPMonitor 统计信息
		stats := ipMonitor.GetStats()
		response.MonitorStats = map[string]interface{}{
			"total_refreshes":     stats.TotalRefreshes,
			"total_planned_pings": stats.TotalPlannedPings,
			"total_actual_pings":  stats.TotalActualPings,
			"total_skipped_pings": stats.TotalSkippedPings,
			"last_refresh_time":   stats.LastRefreshTime,
			"t0_pool_size":        stats.T0PoolSize,
			"t1_pool_size":        stats.T1PoolSize,
			"t2_pool_size":        stats.T2PoolSize,
			"downgraded_ips":      stats.DowngradedIPs,
			"hourly_quota_used":   stats.HourlyQuotaUsed,
			"hourly_quota_limit":  stats.HourlyQuotaLimit,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	s.writeJSONSuccess(w, "IP pool status retrieved successfully", response)
}

// handleIPPoolTop 处理失效 IP 列表请求
// 返回失效 IP 列表（默认）或全量 IP 列表
func (s *Server) handleIPPoolTop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	view := r.URL.Query().Get("view") // 'dead' (默认) 或 'all'/'top'

	response := map[string]interface{}{
		"total_ips":       0,
		"total_ref_count": 0,
		"total_heat":      0,
		"last_updated":    "",
		"top_ips":         []IPPoolResult{},
		"monitor_stats":   make(map[string]interface{}),
		"monitor_enabled": false,
	}

	// 获取 IP 池信息
	ipMonitor := s.dnsServer.GetIPMonitor()
	if ipMonitor != nil {
		stats := ipMonitor.GetStats()
		response["monitor_stats"] = map[string]interface{}{
			"total_refreshes":     stats.TotalRefreshes,
			"total_planned_pings": stats.TotalPlannedPings,
			"total_actual_pings":  stats.TotalActualPings,
			"total_skipped_pings": stats.TotalSkippedPings,
			"last_refresh_time":   stats.LastRefreshTime.Format(time.RFC3339),
			"t0_pool_size":        stats.T0PoolSize,
			"t1_pool_size":        stats.T1PoolSize,
			"t2_pool_size":        stats.T2PoolSize,
			"downgraded_ips":      stats.DowngradedIPs,
			"hourly_quota_used":   stats.HourlyQuotaUsed,
			"hourly_quota_limit":  stats.HourlyQuotaLimit,
		}
		// 获取配置中的 Enabled 状态 (需要加锁或者通过方法获取)
		// 这里暂且从 dnsServer 配置中读，更准确
		response["monitor_enabled"] = s.dnsServer.GetConfig().IPMonitor.Enabled

		pool := ipMonitor.GetIPPool()
		if pool != nil {
			poolStats := pool.GetStats()
			response["total_ips"] = poolStats.TotalIPs
			response["total_ref_count"] = poolStats.TotalRefCount
			response["total_heat"] = poolStats.TotalHeat
			response["last_updated"] = poolStats.LastUpdated.Format(time.RFC3339)

			// 获取所有 IP
			allIPs := pool.GetAllIPs()
			pinger := ipMonitor.GetPinger()
			topIPs := []IPPoolResult{}

			for _, info := range allIPs {
				// 优先使用 IPPool 内部维护的 RTT（真理化数据）
				rtt := -1
				if !info.RTTUpdated.IsZero() {
					rtt = info.RTT
				} else if pinger != nil {
					// 只有当 IPPool 还没测过速时，才去查 Pinger 的实时缓存作为补充
					rttVal, _, exists, _ := pinger.GetIPRTT(info.IP)
					if exists {
						rtt = rttVal
					}
				}

				// 筛选逻辑：如果是 'dead' 视图，则只显示 RTT >= LogicDeadRTT 的 IP
				if view == "all" || view == "top" || rtt >= ping.LogicDeadRTT {
					result := IPPoolResult{
						IP:         info.IP,
						RepDomain:  info.RepDomain,
						RefCount:   info.RefCount,
						AccessHeat: info.AccessHeat,
						RTT:        rtt,
						LastAccess: info.LastAccess.Format(time.RFC3339),
					}
					topIPs = append(topIPs, result)
				}
			}

			// 排序
			if view == "all" || view == "top" {
				// 按热度排序
				sort.Slice(topIPs, func(i, j int) bool {
					if topIPs[i].AccessHeat != topIPs[j].AccessHeat {
						return topIPs[i].AccessHeat > topIPs[j].AccessHeat
					}
					return topIPs[i].RefCount > topIPs[j].RefCount
				})
			} else {
				// 按引用计数排序 (Dead IPs)
				sort.Slice(topIPs, func(i, j int) bool {
					return topIPs[i].RefCount > topIPs[j].RefCount
				})
			}

			// 限制返回数量 (如果是 Top IPs 视角)
			if (view == "all" || view == "top") && len(topIPs) > 100 {
				topIPs = topIPs[:100]
			}

			response["top_ips"] = topIPs
		}
	} else {
		// 默认 MonitorStats
		response["monitor_stats"] = map[string]interface{}{
			"total_refreshes":     0,
			"total_ips_refreshed": 0,
			"last_refresh_time":   "",
			"t0_pool_size":        0,
			"t1_pool_size":        0,
			"t2_pool_size":        0,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	s.writeJSONSuccess(w, "IP pool data retrieved successfully", response)
}

// handleIPPoolToggle 切换 IP 池监控启用状态
func (s *Server) handleIPPoolToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	enabledStr := r.URL.Query().Get("enabled")
	enabled := enabledStr == "true"

	// 1. 获取写锁，保护配置更新
	s.cfgMutex.Lock()
	defer s.cfgMutex.Unlock()

	// 2. 加载现有配置
	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		s.writeJSONError(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. 更新配置
	cfg.IPMonitor.Enabled = enabled

	// 4. 序列化并写入文件
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		s.writeJSONError(w, "Failed to marshal config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.writeConfigFile(yamlData); err != nil {
		s.writeJSONError(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 5. 应用到运行中的服务器
	if err := s.dnsServer.ApplyConfig(cfg); err != nil {
		s.writeJSONError(w, "Failed to apply config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSONSuccess(w, "IP monitor status updated successfully", map[string]bool{"enabled": enabled})
}
