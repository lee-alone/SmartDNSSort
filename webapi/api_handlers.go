package webapi

import (
	"encoding/json"
	"net/http"
	"smartdnssort/config"
	"smartdnssort/logger"
	"strings"

	"github.com/miekg/dns"
)

// handleQuery 处理 DNS 查询请求
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	queryType := r.URL.Query().Get("type")

	if domain == "" {
		http.Error(w, "Missing domain parameter", http.StatusBadRequest)
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
		http.Error(w, "Invalid query type (must be A or AAAA)", http.StatusBadRequest)
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
		http.Error(w, "Domain not found in cache", http.StatusNotFound)
		return
	}

	result := QueryResult{
		Domain: domain,
		Type:   queryType,
		IPs:    ipsResult,
		Status: status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleStats 处理统计信息请求
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	stats := s.dnsServer.GetStats()
	cacheCfg := s.dnsServer.GetConfig().Cache
	currentEntries := s.dnsCache.GetCurrentEntries()
	expiredEntries := s.dnsCache.GetExpiredEntries()
	maxEntries := cacheCfg.CalculateMaxEntries()
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
		"current_memory_mb": int(float64(currentEntries) * config.AvgBytesPerDomain / (1024 * 1024)),
		"memory_percent":    memoryPercent,
		"expired_entries":   expiredEntries,
		"expired_percent":   expiredPercent,
		"protected_entries": s.dnsCache.GetProtectedEntries(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		logger.Errorf("Failed to encode stats: %v", err)
		s.writeJSONError(w, "Failed to encode stats", http.StatusInternalServerError)
	}
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
		"current_memory_mb": int(float64(currentEntries) * config.AvgBytesPerDomain / (1024 * 1024)),
		"memory_percent":    memoryPercent,
		"expired_entries":   expiredEntries,
		"expired_percent":   expiredPercent,
		"protected_entries": protectedEntries,
	}

	s.writeJSONSuccess(w, "Cache memory stats retrieved successfully", stats)
}

// handleHealth 处理健康检查请求
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"healthy"}`))
}

// handleClearCache 处理清空缓存请求
func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// 清空内存缓存
	s.dnsCache.Clear()
	logger.Info("DNS cache (memory) cleared via API request.")

	// 删除磁盘缓存文件
	cacheFile := "dns_cache.json"
	if err := s.deleteCacheFile(cacheFile); err != nil {
		s.writeJSONError(w, err.Error(), http.StatusInternalServerError)
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
	logger.Info("Statistics cleared via API request.")
	s.writeJSONSuccess(w, "All stats cleared successfully", nil)
}

// handleRecentQueries 处理最近查询请求
func (s *Server) handleRecentQueries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	queries := s.dnsServer.GetRecentQueries()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(queries); err != nil {
		logger.Errorf("Failed to encode recent queries: %v", err)
		s.writeJSONError(w, "Failed to encode recent queries", http.StatusInternalServerError)
	}
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
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(topDomainsList); err != nil {
		logger.Errorf("Failed to encode hot domains: %v", err)
		s.writeJSONError(w, "Failed to encode hot domains", http.StatusInternalServerError)
	}
}

// handleRestart 处理重启请求
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	logger.Info("Service restart requested via API.")
	s.writeJSONSuccess(w, "Service restart initiated", nil)
	if s.restartFunc != nil {
		go func() {
			logger.Info("Executing restart function...")
			s.restartFunc()
		}()
	} else {
		logger.Warn("No restart function configured. Please restart manually.")
	}
}
