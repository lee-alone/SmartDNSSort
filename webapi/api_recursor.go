package webapi

import (
	"encoding/json"
	"net/http"
	"os"
	"smartdnssort/logger"
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

// RecursorInstallStatus 安装状态
type RecursorInstallStatus struct {
	State    string `json:"state"`    // "not_installed", "installing", "installed", "error"
	Progress int    `json:"progress"` // 0-100
	Message  string `json:"message"`
	ErrorMsg string `json:"error_msg,omitempty"`
}

// RecursorSystemInfo 系统信息
type RecursorSystemInfo struct {
	OS          string  `json:"os"`
	Distro      string  `json:"distro"`
	CPUCores    int     `json:"cpu_cores"`
	MemoryGB    float64 `json:"memory_gb"`
	UnboundVer  string  `json:"unbound_version"`
	IsInstalled bool    `json:"is_installed"`
	IsRunning   bool    `json:"is_running"`
}

// handleRecursorInstallStatus 获取安装状态
func (s *Server) handleRecursorInstallStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if s.dnsServer == nil {
		json.NewEncoder(w).Encode(RecursorInstallStatus{
			State: "not_installed",
		})
		return
	}

	mgr := s.dnsServer.GetRecursorManager()
	if mgr == nil {
		json.NewEncoder(w).Encode(RecursorInstallStatus{
			State: "not_installed",
		})
		return
	}

	// 获取安装状态
	installState := mgr.GetInstallState()
	status := RecursorInstallStatus{
		Progress: 0,
	}

	switch installState {
	case 0: // StateNotInstalled
		status.State = "not_installed"
		status.Message = "Unbound is not installed"
		status.Progress = 0
	case 1: // StateInstalling
		status.State = "installing"
		status.Message = "Installing Unbound..."
		status.Progress = 50
	case 2: // StateInstalled
		status.State = "installed"
		status.Message = "Unbound is installed and ready"
		status.Progress = 100
	case 3: // StateError
		status.State = "error"
		status.Message = "Error during installation"
		status.Progress = 0
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// handleRecursorSystemInfo 获取系统信息
func (s *Server) handleRecursorSystemInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if s.dnsServer == nil {
		json.NewEncoder(w).Encode(RecursorSystemInfo{})
		return
	}

	mgr := s.dnsServer.GetRecursorManager()
	if mgr == nil {
		json.NewEncoder(w).Encode(RecursorSystemInfo{})
		return
	}

	sysInfo := mgr.GetSystemInfo()
	response := RecursorSystemInfo{
		OS:          sysInfo.OS,
		Distro:      sysInfo.Distro,
		CPUCores:    sysInfo.CPUCores,
		MemoryGB:    sysInfo.MemoryGB,
		UnboundVer:  sysInfo.UnboundVer,
		IsInstalled: sysInfo.IsInstalled,
		IsRunning:   sysInfo.IsRunning,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// RecursorConfig 配置文件内容
type RecursorConfig struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// handleRecursorConfig 获取 Recursor 配置文件
func (s *Server) handleRecursorConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if s.dnsServer == nil {
		s.writeJSONError(w, "DNS server not initialized", http.StatusInternalServerError)
		return
	}

	mgr := s.dnsServer.GetRecursorManager()
	if mgr == nil {
		s.writeJSONError(w, "Recursor manager not initialized", http.StatusInternalServerError)
		return
	}

	// 获取配置文件路径
	// 在 Linux 上是 /etc/unbound/unbound.conf.d/smartdnssort.conf
	// 在 Windows 上是嵌入式路径
	configPath := "/etc/unbound/unbound.conf.d/smartdnssort.conf"

	// 尝试读取配置文件
	content, err := os.ReadFile(configPath)
	if err != nil {
		logger.Errorf("[Recursor] Failed to read config file %s: %v", configPath, err)
		s.writeJSONError(w, "Failed to read config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(RecursorConfig{
		Path:    configPath,
		Content: string(content),
	})
}
