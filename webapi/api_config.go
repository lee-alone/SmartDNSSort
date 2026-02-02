package webapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"smartdnssort/config"
	"smartdnssort/logger"
	"strings"

	"gopkg.in/yaml.v3"
)

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
		s.handleGetConfig(w)
	case http.MethodPost:
		s.handlePostConfig(w, r)
	default:
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
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

	// 读取请求体
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close() // 显式关闭请求体
	logger.Debugf("Received config update request: %s", string(bodyBytes))

	// 解码新配置为新对象（不使用现有配置）
	newCfg := &config.Config{}
	if err := json.Unmarshal(bodyBytes, newCfg); err != nil {
		s.writeJSONError(w, "Failed to parse config JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	logger.Debugf("Parsed config - DNS port: %d, Cache TTL: %d/%d",
		newCfg.DNS.ListenPort, newCfg.Cache.FastResponseTTL, newCfg.Cache.UserReturnTTL)
	logger.Debugf("Upstream servers: %v", newCfg.Upstream.Servers)
	logger.Debugf("Upstream bootstrap DNS: %v", newCfg.Upstream.BootstrapDNS)

	// 验证配置
	if err := s.validateConfig(newCfg); err != nil {
		s.writeJSONError(w, "Configuration validation failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	existingCfg, err := config.LoadConfig(s.configPath)
	if err == nil {
		// 保留不在 Web UI 中修改或由系统动态更新的字段
		if newCfg.AdBlock.LastUpdate == 0 {
			newCfg.AdBlock.LastUpdate = existingCfg.AdBlock.LastUpdate
		}
		if len(newCfg.AdBlock.FailedSources) == 0 {
			newCfg.AdBlock.FailedSources = existingCfg.AdBlock.FailedSources
		}

		if newCfg.System.LogLevel == "" && existingCfg.System.LogLevel != "" {
			newCfg.System.LogLevel = existingCfg.System.LogLevel
		}
		if newCfg.Stats.HotDomainsWindowHours == 0 && existingCfg.Stats.HotDomainsWindowHours > 0 {
			newCfg.Stats = existingCfg.Stats
		}

		// 如果 Web UI 没有传递动态优化配置，保留现有的
		if newCfg.Upstream.DynamicParamOptimization.EWMAAlpha == nil && existingCfg.Upstream.DynamicParamOptimization.EWMAAlpha != nil {
			newCfg.Upstream.DynamicParamOptimization = existingCfg.Upstream.DynamicParamOptimization
		}
	}

	// 使用正确的YAML标签将配置序列化为YAML
	// 创建一个自定义编码器来确保格式正确
	yamlData, err := yaml.Marshal(newCfg)
	if err != nil {
		logger.Errorf("Failed to marshal config to YAML: %v", err)
		s.writeJSONError(w, "Failed to marshal config to YAML: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Debugf("Generated YAML:\n%s", string(yamlData))

	// 写入配置文件
	if err := s.writeConfigFile(yamlData); err != nil {
		logger.Errorf("Failed to write config file %s: %v", s.configPath, err)
		s.writeJSONError(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infof("✓ Configuration written to %s successfully", s.configPath)

	// 应用新配置到运行中的服务器
	if err := s.dnsServer.ApplyConfig(newCfg); err != nil {
		logger.Errorf("✗ Failed to apply new configuration: %v", err)
		s.writeJSONError(w, "Failed to apply configuration to running server: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Info("✓ Configuration applied to DNS server successfully")
	s.writeJSONSuccess(w, "Configuration saved and applied successfully", nil)
}

// validateConfig 验证配置
func (s *Server) validateConfig(cfg *config.Config) error {
	if cfg.DNS.ListenPort <= 0 || cfg.DNS.ListenPort > 65535 {
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
		return fmt.Errorf("at least one upstream server is required, or enable local recursion")
	}
	if cfg.Upstream.TimeoutMs <= 0 {
		return fmt.Errorf("upstream timeout must be positive")
	}
	if cfg.Upstream.Strategy != "" && cfg.Upstream.Strategy != "random" && cfg.Upstream.Strategy != "parallel" && cfg.Upstream.Strategy != "sequential" && cfg.Upstream.Strategy != "racing" && cfg.Upstream.Strategy != "auto" {
		return fmt.Errorf("invalid upstream strategy: %s (must be 'random', 'parallel', 'sequential', 'racing', or 'auto')", cfg.Upstream.Strategy)
	}

	// User-provided concurrency, if any, must be positive.
	if cfg.Upstream.Concurrency != nil && *cfg.Upstream.Concurrency <= 0 {
		return fmt.Errorf("upstream concurrency must be positive if specified, got %d", *cfg.Upstream.Concurrency)
	}

	// User-provided sequential timeout, if any, must be within bounds.
	if cfg.Upstream.SequentialTimeout != nil {
		val := *cfg.Upstream.SequentialTimeout
		if val < MinSequentialTimeoutMs || val > MaxSequentialTimeoutMs {
			return fmt.Errorf("sequential timeout must be between %dms and %dms if specified, got %dms", MinSequentialTimeoutMs, MaxSequentialTimeoutMs, val)
		}
	}

	// User-provided racing delay, if any, must be within bounds.
	if cfg.Upstream.RacingDelay != nil {
		val := *cfg.Upstream.RacingDelay
		if val < MinRacingDelayMs || val > MaxRacingDelayMs {
			return fmt.Errorf("racing delay must be between %dms and %dms if specified, got %dms", MinRacingDelayMs, MaxRacingDelayMs, val)
		}
	}
	// User-provided racing max concurrent, if any, must be within bounds.
	if cfg.Upstream.RacingMaxConcurrent != nil {
		val := *cfg.Upstream.RacingMaxConcurrent
		if val < MinRacingMaxConcurrent || val > MaxRacingMaxConcurrent { // 适当放宽上限，manager 会根据服务器数再次限制
			return fmt.Errorf("racing max concurrent must be between 2 and 10 if specified, got %d", val)
		}
	}

	// User-provided max connections, if any, must be non-negative (0 for auto).
	if cfg.Upstream.MaxConnections != nil && *cfg.Upstream.MaxConnections < 0 {
		return fmt.Errorf("max connections must be non-negative if specified")
	}

	// Validate Dynamic Param Optimization
	if cfg.Upstream.DynamicParamOptimization.EWMAAlpha != nil {
		alpha := *cfg.Upstream.DynamicParamOptimization.EWMAAlpha
		if alpha <= 0 || alpha >= 1 {
			return fmt.Errorf("dynamic optimization ewma_alpha must be between 0 and 1")
		}
	}
	if cfg.Upstream.DynamicParamOptimization.MaxStepMs != nil {
		if *cfg.Upstream.DynamicParamOptimization.MaxStepMs <= 0 {
			return fmt.Errorf("dynamic optimization max_step_ms must be positive")
		}
	}
	if cfg.Cache.MinTTLSeconds < 0 {
		return fmt.Errorf("cache min TTL cannot be negative")
	}
	if cfg.Cache.MaxTTLSeconds < 0 {
		return fmt.Errorf("cache max TTL cannot be negative")
	}
	if cfg.Cache.MinTTLSeconds > 0 && cfg.Cache.MaxTTLSeconds > 0 && cfg.Cache.MinTTLSeconds > cfg.Cache.MaxTTLSeconds {
		return fmt.Errorf("cache min TTL cannot be greater than max TTL")
	}
	if cfg.Cache.FastResponseTTL <= 0 {
		return fmt.Errorf("cache fast response TTL must be positive")
	}
	if cfg.Cache.UserReturnTTL <= 0 {
		return fmt.Errorf("cache user return TTL must be positive")
	}
	if cfg.Cache.NegativeTTLSeconds < 0 {
		return fmt.Errorf("cache negative TTL cannot be negative")
	}
	if cfg.Cache.ErrorCacheTTL < 0 {
		return fmt.Errorf("cache error TTL cannot be negative")
	}
	if cfg.Ping.Count <= 0 {
		return fmt.Errorf("ping count must be positive")
	}
	if cfg.Ping.TimeoutMs <= 0 {
		return fmt.Errorf("ping timeout must be positive")
	}
	if cfg.Ping.Concurrency <= 0 {
		return fmt.Errorf("ping concurrency must be positive")
	}
	if cfg.Ping.MaxTestIPs < 0 {
		return fmt.Errorf("ping max test IPs cannot be negative")
	}
	if cfg.Ping.RttCacheTtlSeconds < 0 {
		return fmt.Errorf("ping RTT cache TTL cannot be negative")
	}
	if cfg.Ping.Strategy != "min" && cfg.Ping.Strategy != "avg" && cfg.Ping.Strategy != "auto" {
		return fmt.Errorf("ping strategy must be 'min', 'avg' or 'auto'")
	}
	if cfg.WebUI.ListenPort <= 0 || cfg.WebUI.ListenPort > 65535 {
		return fmt.Errorf("invalid WebUI listen port: %d", cfg.WebUI.ListenPort)
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
