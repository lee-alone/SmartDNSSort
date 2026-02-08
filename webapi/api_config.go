package webapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"smartdnssort/config"
	"smartdnssort/logger"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const MaxConfigSize = 1 << 20 // 1MB limit for config uploads

// 配置验证的常量定义
const (
	MinSequentialTimeoutMs = 100
	MaxSequentialTimeoutMs = 2000
	MinRacingDelayMs       = 50
	MaxRacingDelayMs       = 500
	MinRacingMaxConcurrent = 2
	MaxRacingMaxConcurrent = 10
)

// handleConfig 处理配置请求
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if r.URL.Path == "/api/config/export" {
			s.handleExportConfig(w, r)
		} else {
			s.handleGetConfig(w)
		}
	case http.MethodPost:
		if r.URL.Path == "/api/config/reset" {
			s.handleResetConfig(w, r)
		} else {
			s.handlePostConfig(w, r)
		}
	default:
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

// handleExportConfig 导出当前配置
func (s *Server) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	currentConfig := s.dnsServer.GetConfig()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=smartdnssort-config.json")
	if err := json.NewEncoder(w).Encode(currentConfig); err != nil {
		logger.Errorf("Failed to encode config for export: %v", err)
		s.writeJSONError(w, "Failed to encode config: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// handleGetConfig 获取当前配置
func (s *Server) handleGetConfig(w http.ResponseWriter) {
	currentConfig := s.dnsServer.GetConfig()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(currentConfig); err != nil {
		logger.Errorf("Failed to encode config for API: %v", err)
		s.writeJSONError(w, "Failed to encode config: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// handlePostConfig 处理配置更新请求
func (s *Server) handlePostConfig(w http.ResponseWriter, r *http.Request) {
	// 获取写锁，保护配置文件更新
	s.cfgMutex.Lock()
	defer s.cfgMutex.Unlock()

	// 检查 Content-Length 或限制读取大小以防止 DoS
	if r.ContentLength > MaxConfigSize {
		s.writeJSONError(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, MaxConfigSize))
	if err != nil {
		logger.Errorf("Failed to read request body: %v", err)
		s.writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	logger.Debugf("Received config update request (size: %d bytes)", len(bodyBytes))

	// 1. 加载现有配置作为基准
	newCfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		logger.Warnf("Failed to load existing config as base, using default template: %v", err)
		newCfg = &config.Config{}
		yaml.Unmarshal([]byte(config.DefaultConfigContent), newCfg)
		config.SetDefaults(newCfg)
	}

	// 2. 将新配置（JSON 格式）解析并覆盖到基准配置上
	if err := json.Unmarshal(bodyBytes, newCfg); err != nil {
		logger.Errorf("JSON Unmarshal failed: %v", err)
		s.writeJSONError(w, "Failed to parse config JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 3. 验证合并后的配置
	if err := s.validateConfig(newCfg); err != nil {
		logger.Errorf("Configuration validation failed: %v", err)
		s.writeJSONError(w, "Configuration validation failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 4. 备份当前配置文件
	backupPath := fmt.Sprintf("%s.backup_%d", s.configPath, time.Now().Unix())
	if err := s.backupConfigFile(s.configPath, backupPath); err != nil {
		logger.Warnf("Failed to create config backup: %v", err)
	} else {
		logger.Infof("✓ Current configuration backed up to %s", backupPath)
	}

	// 5. 使用正确的YAML标签将配置序列化为YAML
	yamlData, err := yaml.Marshal(newCfg)
	if err != nil {
		logger.Errorf("Failed to marshal config to YAML: %v", err)
		s.writeJSONError(w, "Failed to marshal config to YAML: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 6. 写入配置文件
	if err := s.writeConfigFile(yamlData); err != nil {
		logger.Errorf("Failed to write config file %s: %v", s.configPath, err)
		s.writeJSONError(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infof("✓ Configuration written to %s successfully", s.configPath)

	// 7. 应用新配置到运行中的服务器
	if err := s.dnsServer.ApplyConfig(newCfg); err != nil {
		logger.Errorf("✗ Failed to apply new configuration: %v", err)
		s.writeJSONError(w, "Failed to apply configuration to running server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Info("✓ Configuration applied to DNS server successfully")
	s.writeJSONSuccess(w, "Configuration saved and applied successfully", nil)
}

// handleResetConfig 恢复默认配置
func (s *Server) handleResetConfig(w http.ResponseWriter, r *http.Request) {
	s.cfgMutex.Lock()
	defer s.cfgMutex.Unlock()

	// 备份当前配置
	backupPath := fmt.Sprintf("%s.backup_%d", s.configPath, time.Now().Unix())
	if err := s.backupConfigFile(s.configPath, backupPath); err != nil {
		logger.Warnf("Failed to create config backup: %v", err)
	} else {
		logger.Infof("✓ Current configuration backed up to %s", backupPath)
	}

	// 从默认内容解析配置
	newCfg := &config.Config{}
	if err := yaml.Unmarshal([]byte(config.DefaultConfigContent), newCfg); err != nil {
		s.writeJSONError(w, "Failed to parse default config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 写入配置文件
	if err := s.writeConfigFile([]byte(config.DefaultConfigContent)); err != nil {
		logger.Errorf("Failed to reset config file %s: %v", s.configPath, err)
		s.writeJSONError(w, "Failed to write default config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 应用新配置到运行中的服务器
	if err := s.dnsServer.ApplyConfig(newCfg); err != nil {
		logger.Errorf("✗ Failed to apply default configuration: %v", err)
		s.writeJSONError(w, "Failed to apply default configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infof("✓ Configuration reset to defaults successfully")
	s.writeJSONSuccess(w, "Configuration reset to defaults successfully", nil)
}

// backupConfigFile 备份配置文件
func (s *Server) backupConfigFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	if err := os.WriteFile(dst, data, 0644); err != nil {
		return err
	}

	// 清理旧备份，只保留最近 5 个
	const maxBackups = 5
	backups, err := filepath.Glob(s.configPath + ".backup_*")
	if err == nil && len(backups) > maxBackups {
		// 按修改时间排序（或者按文件名，因为包含时间戳）
		sort.Strings(backups)
		// 删除多余的旧备份
		for i := 0; i < len(backups)-maxBackups; i++ {
			os.Remove(backups[i])
			logger.Debugf("Removed old config backup: %s", backups[i])
		}
	}

	return nil
}

// validateConfig 验证配置
func (s *Server) validateConfig(cfg *config.Config) error {
	if cfg.DNS.ListenPort <= 0 || cfg.DNS.ListenPort > 65535 {
		logger.Errorf("Validation failed: invalid DNS listen port %d", cfg.DNS.ListenPort)
		return fmt.Errorf("invalid DNS listen port: %d", cfg.DNS.ListenPort)
	}

	// Sanitize Upstream Servers (remove quotes and spaces)
	for i, server := range cfg.Upstream.Servers {
		cfg.Upstream.Servers[i] = strings.Trim(server, "' ")
	}
	// Sanitize Bootstrap DNS
	for i, server := range cfg.Upstream.BootstrapDNS {
		cfg.Upstream.BootstrapDNS[i] = strings.Trim(server, "' ")
	}

	if len(cfg.Upstream.Servers) == 0 && !cfg.Upstream.EnableRecursor {
		logger.Error("Validation failed: at least one upstream server is required, or enable local recursion")
		return fmt.Errorf("at least one upstream server is required, or enable local recursion")
	}
	if cfg.Upstream.TimeoutMs <= 0 {
		logger.Errorf("Validation failed: upstream timeout must be positive, got %d", cfg.Upstream.TimeoutMs)
		return fmt.Errorf("upstream timeout must be positive")
	}
	if cfg.Upstream.Strategy != "" && cfg.Upstream.Strategy != "random" && cfg.Upstream.Strategy != "parallel" && cfg.Upstream.Strategy != "sequential" && cfg.Upstream.Strategy != "racing" && cfg.Upstream.Strategy != "auto" {
		logger.Errorf("Validation failed: invalid upstream strategy %s", cfg.Upstream.Strategy)
		return fmt.Errorf("invalid upstream strategy: %s (must be 'random', 'parallel', 'sequential', 'racing', or 'auto')", cfg.Upstream.Strategy)
	}

	// User-provided concurrency, if any, must be positive.
	if cfg.Upstream.Concurrency != nil && *cfg.Upstream.Concurrency <= 0 {
		logger.Errorf("Validation failed: upstream concurrency must be positive, got %d", *cfg.Upstream.Concurrency)
		return fmt.Errorf("upstream concurrency must be positive if specified, got %d", *cfg.Upstream.Concurrency)
	}

	// User-provided sequential timeout, if any, must be within bounds.
	if cfg.Upstream.SequentialTimeout != nil {
		val := *cfg.Upstream.SequentialTimeout
		if val < MinSequentialTimeoutMs || val > MaxSequentialTimeoutMs {
			logger.Errorf("Validation failed: sequential timeout must be between %dms and %dms, got %dms", MinSequentialTimeoutMs, MaxSequentialTimeoutMs, val)
			return fmt.Errorf("sequential timeout must be between %dms and %dms if specified, got %dms", MinSequentialTimeoutMs, MaxSequentialTimeoutMs, val)
		}
	}

	// User-provided racing delay, if any, must be within bounds.
	if cfg.Upstream.RacingDelay != nil {
		val := *cfg.Upstream.RacingDelay
		if val < MinRacingDelayMs || val > MaxRacingDelayMs {
			logger.Errorf("Validation failed: racing delay must be between %dms and %dms, got %dms", MinRacingDelayMs, MaxRacingDelayMs, val)
			return fmt.Errorf("racing delay must be between %dms and %dms if specified, got %dms", MinRacingDelayMs, MaxRacingDelayMs, val)
		}
	}
	// User-provided racing max concurrent, if any, must be within bounds.
	if cfg.Upstream.RacingMaxConcurrent != nil {
		val := *cfg.Upstream.RacingMaxConcurrent
		if val < MinRacingMaxConcurrent || val > MaxRacingMaxConcurrent {
			logger.Errorf("Validation failed: racing max concurrent must be between 2 and 10, got %d", val)
			return fmt.Errorf("racing max concurrent must be between 2 and 10 if specified, got %d", val)
		}
	}

	// User-provided max connections, if any, must be non-negative (0 for auto).
	if cfg.Upstream.MaxConnections != nil && *cfg.Upstream.MaxConnections < 0 {
		logger.Error("Validation failed: max connections must be non-negative")
		return fmt.Errorf("max connections must be non-negative if specified")
	}

	// Validate Dynamic Param Optimization
	if cfg.Upstream.DynamicParamOptimization.EWMAAlpha != nil {
		alpha := *cfg.Upstream.DynamicParamOptimization.EWMAAlpha
		if alpha <= 0 || alpha >= 1 {
			logger.Errorf("Validation failed: ewma_alpha must be between 0 and 1, got %f", alpha)
			return fmt.Errorf("dynamic optimization ewma_alpha must be between 0 and 1")
		}
	}
	if cfg.Upstream.DynamicParamOptimization.MaxStepMs != nil {
		if *cfg.Upstream.DynamicParamOptimization.MaxStepMs <= 0 {
			logger.Errorf("Validation failed: max_step_ms must be positive, got %d", *cfg.Upstream.DynamicParamOptimization.MaxStepMs)
			return fmt.Errorf("dynamic optimization max_step_ms must be positive")
		}
	}
	if cfg.Cache.MinTTLSeconds < 0 {
		logger.Error("Validation failed: cache min TTL cannot be negative")
		return fmt.Errorf("cache min TTL cannot be negative")
	}
	if cfg.Cache.MaxTTLSeconds < 0 {
		logger.Error("Validation failed: cache max TTL cannot be negative")
		return fmt.Errorf("cache max TTL cannot be negative")
	}
	if cfg.Cache.MinTTLSeconds > 0 && cfg.Cache.MaxTTLSeconds > 0 && cfg.Cache.MinTTLSeconds > cfg.Cache.MaxTTLSeconds {
		logger.Errorf("Validation failed: cache min TTL (%d) cannot be greater than max TTL (%d)", cfg.Cache.MinTTLSeconds, cfg.Cache.MaxTTLSeconds)
		return fmt.Errorf("cache min TTL cannot be greater than max TTL")
	}
	if cfg.Cache.FastResponseTTL <= 0 {
		logger.Errorf("Validation failed: cache fast response TTL must be positive, got %d", cfg.Cache.FastResponseTTL)
		return fmt.Errorf("cache fast response TTL must be positive")
	}
	if cfg.Cache.UserReturnTTL <= 0 {
		logger.Errorf("Validation failed: cache user return TTL must be positive, got %d", cfg.Cache.UserReturnTTL)
		return fmt.Errorf("cache user return TTL must be positive")
	}
	if cfg.Cache.NegativeTTLSeconds < 0 {
		logger.Error("Validation failed: cache negative TTL cannot be negative")
		return fmt.Errorf("cache negative TTL cannot be negative")
	}
	if cfg.Cache.ErrorCacheTTL < 0 {
		logger.Error("Validation failed: cache error TTL cannot be negative")
		return fmt.Errorf("cache error TTL cannot be negative")
	}
	if cfg.Ping.Count <= 0 {
		logger.Errorf("Validation failed: ping count must be positive, got %d", cfg.Ping.Count)
		return fmt.Errorf("ping count must be positive")
	}
	if cfg.Ping.TimeoutMs <= 0 {
		logger.Errorf("Validation failed: ping timeout must be positive, got %d", cfg.Ping.TimeoutMs)
		return fmt.Errorf("ping timeout must be positive")
	}
	if cfg.Ping.Concurrency <= 0 {
		logger.Errorf("Validation failed: ping concurrency must be positive, got %d", cfg.Ping.Concurrency)
		return fmt.Errorf("ping concurrency must be positive")
	}
	if cfg.Ping.MaxTestIPs < 0 {
		logger.Error("Validation failed: max test IPs cannot be negative")
		return fmt.Errorf("ping max test IPs cannot be negative")
	}
	if cfg.Ping.RttCacheTtlSeconds < 0 {
		logger.Error("Validation failed: ping RTT cache TTL cannot be negative")
		return fmt.Errorf("ping RTT cache TTL cannot be negative")
	}
	if cfg.Ping.Strategy != "min" && cfg.Ping.Strategy != "avg" && cfg.Ping.Strategy != "auto" {
		logger.Errorf("Validation failed: invalid ping strategy %s", cfg.Ping.Strategy)
		return fmt.Errorf("ping strategy must be 'min', 'avg' or 'auto'")
	}
	if cfg.WebUI.ListenPort <= 0 || cfg.WebUI.ListenPort > 65535 {
		logger.Errorf("Validation failed: invalid WebUI listen port %d", cfg.WebUI.ListenPort)
		return fmt.Errorf("invalid WebUI listen port: %d", cfg.WebUI.ListenPort)
	}

	// 验证系统配置
	if cfg.System.MaxCPUCores < 0 {
		logger.Error("Validation failed: max_cpu_cores cannot be negative")
		return fmt.Errorf("max_cpu_cores cannot be negative")
	}
	if cfg.System.SortQueueWorkers < 0 {
		logger.Error("Validation failed: sort_queue_workers cannot be negative")
		return fmt.Errorf("sort_queue_workers cannot be negative")
	}
	if cfg.System.RefreshWorkers < 0 {
		logger.Error("Validation failed: refresh_workers cannot be negative")
		return fmt.Errorf("refresh_workers cannot be negative")
	}

	// 验证统计配置
	if cfg.Stats.HotDomainsWindowHours < 0 {
		logger.Error("Validation failed: hot_domains_window_hours cannot be negative")
		return fmt.Errorf("hot_domains_window_hours cannot be negative")
	}
	if cfg.Stats.BlockedDomainsWindowHours < 0 {
		logger.Error("Validation failed: blocked_domains_window_hours cannot be negative")
		return fmt.Errorf("blocked_domains_window_hours cannot be negative")
	}

	return nil
}

// derefOrDefaultVal returns the dereferenced value of an *int, or a default if nil.
func derefOrDefaultVal(ptr *int, defaultValue int) int {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}
