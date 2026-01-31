package webapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"smartdnssort/logger"
)

// UnboundConfigPayload Unbound 配置请求体
type UnboundConfigPayload struct {
	Content string `json:"content"`
}

// handleUnboundConfig 处理 Unbound 配置文件的读写
func (s *Server) handleUnboundConfig(w http.ResponseWriter, r *http.Request) {
	// 检查递归是否启用
	if !s.cfg.Upstream.EnableRecursor {
		// 递归未启用时，返回空内容而不是错误
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"content": "", "enabled": false})
		return
	}

	// 获取 Recursor Manager
	mgr := s.dnsServer.GetRecursorManager()
	if mgr == nil {
		// Manager 未初始化时，返回空内容
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"content": "", "enabled": false})
		return
	}

	switch r.Method {
	case http.MethodGet:
		// 读取 Unbound 配置文件
		s.handleUnboundConfigGet(w)

	case http.MethodPost:
		// 保存 Unbound 配置文件并重启
		s.handleUnboundConfigPost(w, r, mgr)

	default:
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleUnboundConfigGet 读取 Unbound 配置文件
func (s *Server) handleUnboundConfigGet(w http.ResponseWriter) {
	// 获取配置文件路径
	configPath := s.getUnboundConfigPath()

	// 读取文件内容
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"content": ""})
			return
		}
		logger.Errorf("[Unbound] Failed to read config file: %v", err)
		s.writeJSONError(w, "Failed to read config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"content": string(content)})
}

// handleUnboundConfigPost 保存 Unbound 配置文件并重启
func (s *Server) handleUnboundConfigPost(w http.ResponseWriter, r *http.Request, mgr interface{}) {
	// 解析请求体
	var payload UnboundConfigPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		logger.Errorf("[Unbound] Failed to decode request: %v", err)
		s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 获取配置文件路径
	configPath := s.getUnboundConfigPath()

	// 确保目录存在
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Errorf("[Unbound] Failed to create directory: %v", err)
		s.writeJSONError(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 写入配置文件
	if err := os.WriteFile(configPath, []byte(payload.Content), 0644); err != nil {
		logger.Errorf("[Unbound] Failed to write config file: %v", err)
		s.writeJSONError(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infof("[Unbound] Config file saved: %s", configPath)

	// 重启 Unbound 进程
	recursorMgr := s.dnsServer.GetRecursorManager()
	if recursorMgr != nil {
		// 停止当前进程
		if err := recursorMgr.Stop(); err != nil {
			logger.Warnf("[Unbound] Failed to stop recursor: %v", err)
		}

		// 启动新进程
		if err := recursorMgr.Start(); err != nil {
			logger.Errorf("[Unbound] Failed to restart recursor: %v", err)
			s.writeJSONError(w, "Config saved but failed to restart: "+err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Infof("[Unbound] Process restarted successfully")
	}

	s.writeJSONSuccess(w, "Unbound config saved and process restarted", nil)
}

// getUnboundConfigPath 获取 Unbound 配置文件路径
func (s *Server) getUnboundConfigPath() string {
	// 与 recursor/embedded.go 中的 GetUnboundConfigDir 保持一致
	unboundDir := "unbound"
	return filepath.Join(unboundDir, "unbound.conf")
}
