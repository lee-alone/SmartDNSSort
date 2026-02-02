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
	// 获取 Recursor Manager
	mgr := s.dnsServer.GetRecursorManager()

	// 检查递归是否启用：
	// 1. 配置中启用了递归
	// 2. 或者 Manager 存在且已启用
	recursorEnabled := s.cfg.Upstream.EnableRecursor || (mgr != nil && mgr.IsEnabled())

	if !recursorEnabled {
		// 递归未启用时，返回空内容而不是错误
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"content": "", "enabled": false})
		return
	}

	// Manager 必须存在才能继续
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
		s.handleUnboundConfigPost(w, r)

	default:
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleUnboundConfigGet 读取 Unbound 配置文件
func (s *Server) handleUnboundConfigGet(w http.ResponseWriter) {
	// 读取文件前加读锁
	s.unboundConfigMutex.RLock()
	defer s.unboundConfigMutex.RUnlock()

	// 获取配置文件路径
	configPath := s.getUnboundConfigPath()

	// 读取文件内容
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"content": "", "enabled": true})
			return
		}
		logger.Errorf("[Unbound] Failed to read config file %s: %v", configPath, err)
		s.writeJSONError(w, "Failed to read config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"content": string(content), "enabled": true})
}

// handleUnboundConfigPost 保存 Unbound 配置文件并重启
func (s *Server) handleUnboundConfigPost(w http.ResponseWriter, r *http.Request) {
	// 写入文件前加写锁
	s.unboundConfigMutex.Lock()
	defer s.unboundConfigMutex.Unlock()

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
		logger.Errorf("[Unbound] Failed to create directory %s: %v", dir, err)
		s.writeJSONError(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 写入配置文件
	if err := os.WriteFile(configPath, []byte(payload.Content), 0644); err != nil {
		logger.Errorf("[Unbound] Failed to write config file %s: %v", configPath, err)
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
	// 在 Linux 上，配置文件在 /etc/unbound/unbound.conf.d/smartdnssort.conf
	// 在 Windows 上，配置文件在嵌入式目录

	// 首先尝试 Linux 标准位置
	linuxPath := "/etc/unbound/unbound.conf.d/smartdnssort.conf"
	if _, err := os.Stat(linuxPath); err == nil {
		return linuxPath
	}

	// 备选：嵌入式路径
	unboundDir := "unbound"
	return filepath.Join(unboundDir, "unbound.conf")
}
