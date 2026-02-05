package webapi

import (
	"encoding/json"
	"net/http"
	"smartdnssort/logger"
	"smartdnssort/upstream"
)

// UpstreamServerStats 上游服务器统计信息
type UpstreamServerStats struct {
	Address                        string  `json:"address"`
	Protocol                       string  `json:"protocol"`
	Success                        int64   `json:"success"`
	Failure                        int64   `json:"failure"`
	Total                          int64   `json:"total"`
	SuccessRate                    float64 `json:"success_rate"`
	Status                         string  `json:"status"`
	LatencyMs                      float64 `json:"latency_ms"`
	ConsecutiveFailures            int     `json:"consecutive_failures"`
	ConsecutiveSuccesses           int     `json:"consecutive_successes"`
	LastFailure                    *string `json:"last_failure"`
	SecondsSinceLastFailure        *int    `json:"seconds_since_last_failure"`
	CircuitBreakerRemainingSeconds int     `json:"circuit_breaker_remaining_seconds"`
	IsTemporarilySkipped           bool    `json:"is_temporarily_skipped"`
}

// UpstreamStatsResponse 上游统计响应
type UpstreamStatsResponse struct {
	Servers []UpstreamServerStats `json:"servers"`
}

// handleUpstreamStats 处理上游服务器统计请求
func (s *Server) handleUpstreamStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	upstreamMgr := s.dnsServer.GetUpstreamManager()
	if upstreamMgr == nil {
		s.writeJSONError(w, "Upstream manager not available", http.StatusInternalServerError)
		return
	}

	// 获取所有上游服务器
	servers := upstreamMgr.GetServers()
	if len(servers) == 0 {
		w.Header().Set("Content-Type", "application/json")
		response := APIResponse{
			Success: true,
			Message: "No upstream servers configured",
			Data: UpstreamStatsResponse{
				Servers: []UpstreamServerStats{},
			},
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	// 获取全局统计数据
	globalStats := s.dnsServer.GetStats()
	upstreamStatsMap := make(map[string]map[string]int64)
	if upstreamStats, ok := globalStats["upstream_stats"].(map[string]map[string]int64); ok {
		upstreamStatsMap = upstreamStats
	}

	var statsServers []UpstreamServerStats

	for _, srv := range servers {
		// 获取健康状态信息
		var healthAwareSrv *upstream.HealthAwareUpstream
		if haSrv, ok := srv.(*upstream.HealthAwareUpstream); ok {
			healthAwareSrv = haSrv
		} else {
			logger.Warnf("Server %s is not HealthAwareUpstream", srv.Address())
			continue
		}

		health := healthAwareSrv.GetHealth()
		if health == nil {
			continue
		}

		// 获取健康状态统计数据
		healthStats := health.GetStats()

		// 从全局统计中获取成功/失败计数
		// 注意：srv.Address() 返回的格式包含协议前缀（如 "udp://8.8.8.8:53"）
		serverAddress := srv.Address()
		success := int64(0)
		failure := int64(0)
		if serverStats, ok := upstreamStatsMap[serverAddress]; ok {
			success = serverStats["success"]
			failure = serverStats["failure"]
		} else {
			// 调试日志：确认地址格式匹配
			logger.Debugf("No stats found for server: %s (available keys: %v)", serverAddress, getMapKeys(upstreamStatsMap))
		}
		total := success + failure

		// 计算成功率
		successRate := 0.0
		if total > 0 {
			successRate = float64(success) / float64(total) * 100
		}

		// 获取状态字符串
		statusStr := "healthy"
		if statusVal, ok := healthStats["status"].(string); ok {
			statusStr = statusVal
		}

		// 获取延迟（毫秒）
		latencyMs := healthAwareSrv.GetHealth().GetLatency().Seconds() * 1000

		// 获取最后失败时间
		var lastFailure *string
		if lastFailureStr, ok := healthStats["last_failure"].(string); ok && lastFailureStr != "" {
			lastFailure = &lastFailureStr
		}

		// 获取距离最后失败的秒数
		var secondsSinceLastFailure *int
		if secondsSince, ok := healthStats["seconds_since_last_failure"].(int); ok && secondsSince > 0 {
			secondsSinceLastFailure = &secondsSince
		}

		// 获取熔断剩余秒数
		circuitBreakerRemaining := 0
		if cbRemaining, ok := healthStats["circuit_breaker_remaining_seconds"].(int); ok {
			circuitBreakerRemaining = cbRemaining
		}

		// 获取连续失败/成功次数
		consecutiveFailures := 0
		if cf, ok := healthStats["consecutive_failures"].(int); ok {
			consecutiveFailures = cf
		}

		consecutiveSuccesses := 0
		if cs, ok := healthStats["consecutive_successes"].(int); ok {
			consecutiveSuccesses = cs
		}

		serverStats := UpstreamServerStats{
			Address:                        srv.Address(),
			Protocol:                       srv.Protocol(),
			Success:                        success,
			Failure:                        failure,
			Total:                          total,
			SuccessRate:                    successRate,
			Status:                         statusStr,
			LatencyMs:                      latencyMs,
			ConsecutiveFailures:            consecutiveFailures,
			ConsecutiveSuccesses:           consecutiveSuccesses,
			LastFailure:                    lastFailure,
			SecondsSinceLastFailure:        secondsSinceLastFailure,
			CircuitBreakerRemainingSeconds: circuitBreakerRemaining,
			IsTemporarilySkipped:           healthAwareSrv.ShouldSkipTemporarily(),
		}

		statsServers = append(statsServers, serverStats)
	}

	w.Header().Set("Content-Type", "application/json")
	response := APIResponse{
		Success: true,
		Message: "Upstream servers statistics",
		Data: UpstreamStatsResponse{
			Servers: statsServers,
		},
	}
	json.NewEncoder(w).Encode(response)
}

// getMapKeys 获取 map 的所有 key（用于调试）
func getMapKeys(m map[string]map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
