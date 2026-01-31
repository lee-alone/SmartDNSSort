package webapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// RecursorStatus 递归解析器状态
type RecursorStatus struct {
	Enabled         bool   `json:"enabled"`
	Port            int    `json:"port"`
	Address         string `json:"address"`
	Uptime          int64  `json:"uptime"`            // 秒
	LastHealthCheck int64  `json:"last_health_check"` // Unix 时间戳
}

// handleRecursorStatus 获取 Recursor 状态
func (s *Server) handleRecursorStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// 1. 检查 Server 实例
	if s.dnsServer == nil {
		json.NewEncoder(w).Encode(RecursorStatus{
			Enabled: false,
		})
		return
	}

	// 2. 获取 Manager 实例（通过 Getter）
	mgr := s.dnsServer.GetRecursorManager()
	if mgr == nil {
		// Manager 未初始化（说明配置未启用）
		json.NewEncoder(w).Encode(RecursorStatus{
			Enabled: false,
		})
		return
	}

	// 3. 构造真实状态
	status := RecursorStatus{
		Enabled:         mgr.IsEnabled(),
		Port:            mgr.GetPort(),
		Address:         mgr.GetAddress(),
		LastHealthCheck: mgr.GetLastHealthCheck().Unix(),
	}

	// 4. 计算运行时间
	// 基于实际的启动时间计算，而不是最后一次健康检查时间
	startTime := mgr.GetStartTime()
	if status.Enabled && !startTime.IsZero() {
		status.Uptime = int64(time.Since(startTime).Seconds())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}
