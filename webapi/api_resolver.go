package webapi

import (
	"encoding/json"
	"net/http"
	"time"

	"smartdnssort/logger"
)

// ResolverStatus 递归解析器状态
type ResolverStatus struct {
	Status       string `json:"status"`        // running, stopped
	Uptime       string `json:"uptime"`        // 运行时间
	UptimeS      int64  `json:"uptime_s"`      // 运行时间（秒）
	Enabled      bool   `json:"enabled"`       // 是否启用
	Port         int    `json:"port"`          // 监听端口
	QueryTimeout int    `json:"query_timeout"` // 查询超时
}

// ResolverStats 递归解析器统计
type ResolverStats struct {
	TotalQueries   int64   `json:"total_queries"`
	SuccessQueries int64   `json:"success_queries"`
	FailedQueries  int64   `json:"failed_queries"`
	SuccessRate    float64 `json:"success_rate"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	MinLatencyMs   float64 `json:"min_latency_ms"`
	MaxLatencyMs   float64 `json:"max_latency_ms"`
	CacheHits      int64   `json:"cache_hits"`
	CacheMisses    int64   `json:"cache_misses"`
	CacheHitRate   float64 `json:"cache_hit_rate"`
	Uptime         string  `json:"uptime"`
	UptimeSeconds  float64 `json:"uptime_seconds"`
}

// handleResolverStatus 获取递归解析器状态
func (s *Server) handleResolverStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查递归解析器是否启用
	enabled := s.cfg.Recursive.Enabled
	status := "stopped"
	if enabled && s.dnsServer != nil && s.dnsServer.IsResolverRunning() {
		status = "running"
	}

	// 获取统计信息
	var stats map[string]interface{}
	if s.dnsServer != nil && s.dnsServer.GetResolverStats != nil {
		stats = s.dnsServer.GetResolverStats()
	}

	uptime := ""
	uptimeS := int64(0)
	if stats != nil {
		if u, ok := stats["uptime"]; ok {
			uptime = u.(string)
		}
		if u, ok := stats["uptime_seconds"]; ok {
			uptimeS = int64(u.(float64))
		}
	}

	resp := ResolverStatus{
		Status:       status,
		Uptime:       uptime,
		UptimeS:      uptimeS,
		Enabled:      enabled,
		Port:         s.cfg.Recursive.Port,
		QueryTimeout: s.cfg.Recursive.QueryTimeout,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Data:    resp,
	})
}

// handleResolverControl 控制递归解析器
func (s *Server) handleResolverControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Action string `json:"action"` // start, stop, restart
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	switch req.Action {
	case "start":
		if s.dnsServer != nil && s.dnsServer.StartResolver != nil {
			if err := s.dnsServer.StartResolver(); err != nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(APIResponse{
					Success: false,
					Message: "Failed to start resolver: " + err.Error(),
				})
				return
			}
		}
		logger.Info("[API] Resolver started")

	case "stop":
		if s.dnsServer != nil && s.dnsServer.StopResolver != nil {
			if err := s.dnsServer.StopResolver(); err != nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(APIResponse{
					Success: false,
					Message: "Failed to stop resolver: " + err.Error(),
				})
				return
			}
		}
		logger.Info("[API] Resolver stopped")

	case "restart":
		if s.dnsServer != nil {
			if s.dnsServer.StopResolver != nil {
				s.dnsServer.StopResolver()
			}
			if s.dnsServer.StartResolver != nil {
				if err := s.dnsServer.StartResolver(); err != nil {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(APIResponse{
						Success: false,
						Message: "Failed to restart resolver: " + err.Error(),
					})
					return
				}
			}
		}
		logger.Info("[API] Resolver restarted")

	default:
		http.Error(w, "Invalid action", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Message: "Action completed",
	})
}

// handleResolverStats 获取递归解析器统计
func (s *Server) handleResolverStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var stats map[string]interface{}
	if s.dnsServer != nil && s.dnsServer.GetResolverStats != nil {
		stats = s.dnsServer.GetResolverStats()
	}

	if stats == nil {
		stats = map[string]interface{}{
			"total_queries":   0,
			"success_queries": 0,
			"failed_queries":  0,
			"success_rate":    0,
			"avg_latency_ms":  0,
			"min_latency_ms":  0,
			"max_latency_ms":  0,
			"cache_hits":      0,
			"cache_misses":    0,
			"cache_hit_rate":  0,
			"uptime":          "0s",
			"uptime_seconds":  0,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Data:    stats,
	})
}

// handleResolverStatsClear 清空递归解析器统计
func (s *Server) handleResolverStatsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.dnsServer != nil && s.dnsServer.ClearResolverStats != nil {
		s.dnsServer.ClearResolverStats()
	}

	logger.Info("[API] Resolver stats cleared")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Message: "Stats cleared",
	})
}

// handleResolverConfig 获取递归解析器配置
func (s *Server) handleResolverConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(APIResponse{
			Success: true,
			Data:    s.cfg.Recursive,
		})
	} else if r.Method == http.MethodPost {
		var cfg struct {
			Enabled              bool `json:"enabled"`
			Port                 int  `json:"port"`
			QueryTimeout         int  `json:"query_timeout"`
			MaxConcurrentQueries int  `json:"max_concurrent_queries"`
		}

		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// 更新配置
		s.cfg.Recursive.Enabled = cfg.Enabled
		s.cfg.Recursive.Port = cfg.Port
		s.cfg.Recursive.QueryTimeout = cfg.QueryTimeout
		s.cfg.Recursive.MaxConcurrentQueries = cfg.MaxConcurrentQueries

		// 保存配置到文件
		// 注意：这里需要实现配置保存逻辑
		logger.Info("[API] Resolver config updated")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(APIResponse{
			Success: true,
			Message: "Config updated",
		})
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleResolverTrace 获取迭代路径跟踪
func (s *Server) handleResolverTrace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Domain string `json:"domain"`
		Type   string `json:"type"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Type == "" {
		req.Type = "A"
	}

	// 调用递归解析器进行跟踪查询
	if s.dnsServer == nil || s.dnsServer.TraceResolve == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(APIResponse{
			Success: false,
			Message: "Resolver not available",
		})
		return
	}

	trace, err := s.dnsServer.TraceResolve(req.Domain, req.Type)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(APIResponse{
			Success: false,
			Message: "Trace failed: " + err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Data:    trace,
	})
}

// handleResolverDiagnose 诊断递归解析器
func (s *Server) handleResolverDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	diagnosis := map[string]interface{}{
		"enabled":       s.cfg.Recursive.Enabled,
		"running":       s.dnsServer != nil && s.dnsServer.IsResolverRunning != nil && s.dnsServer.IsResolverRunning(),
		"port":          s.cfg.Recursive.Port,
		"query_timeout": s.cfg.Recursive.QueryTimeout,
		"root_hints_ok": true, // 可以扩展为实际检查
		"timestamp":     time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Data:    diagnosis,
	})
}
