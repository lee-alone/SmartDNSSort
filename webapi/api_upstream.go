package webapi

import (
	"encoding/json"
	"net/http"
	"smartdnssort/logger"
	"smartdnssort/upstream"
	"strconv"
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

	// 获取时间范围参数
	daysStr := r.URL.Query().Get("days")
	days := 7 // 默认7天

	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil {
			// 参数验证：只允许 1, 7, 30
			if d == 1 || d == 7 || d == 30 {
				days = d
			}
		}
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
		response := map[string]interface{}{
			"success": true,
			"message": "No upstream servers configured",
			"data": map[string]interface{}{
				"servers": []interface{}{},
			},
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	var statsServers []map[string]interface{}

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
			logger.Warnf("Server %s health is nil", srv.Address())
			continue
		}

		// 使用时间范围查询统计
		healthStats := health.GetStatsWithTimeRange(days)

		serverStats := map[string]interface{}{
			"address":      healthStats["address"],
			"protocol":     srv.Protocol(),
			"success":      healthStats["success"],
			"failure":      healthStats["failure"],
			"total":        healthStats["total"],
			"success_rate": healthStats["success_rate"],
			"status":       healthStats["status"],
			"latency_ms":   healthStats["latency_ms"],
		}

		statsServers = append(statsServers, serverStats)
	}

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"success": true,
		"message": "Upstream servers statistics for last " + strconv.Itoa(days) + " days",
		"data": map[string]interface{}{
			"servers": statsServers,
		},
	}
	json.NewEncoder(w).Encode(response)
}

// handleClearUpstreamStats 处理清除上游服务器统计请求
func (s *Server) handleClearUpstreamStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	upstreamMgr := s.dnsServer.GetUpstreamManager()
	if upstreamMgr == nil {
		s.writeJSONError(w, "Upstream manager not available", http.StatusInternalServerError)
		return
	}

	upstreamMgr.ClearStats()
	logger.Info("Upstream servers statistics cleared via API request.")
	s.writeJSONSuccess(w, "Upstream servers statistics cleared successfully", nil)
}
