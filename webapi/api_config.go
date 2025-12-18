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

// handleConfig 处理配置请求
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfig(w)
	case http.MethodPost:
		s.handlePostConfig(w, r)
	default:
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

// handleGetConfig 获取当前配置
func (s *Server) handleGetConfig(w http.ResponseWriter) {
	currentConfig := s.dnsServer.GetConfig()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(currentConfig); err != nil {
		logger.Errorf("Failed to encode config for API: %v", err)
		http.Error(w, "Failed to encode config: "+err.Error(), http.StatusInternalServerError)
	}
}

// handlePostConfig 更新配置
func (s *Server) handlePostConfig(w http.ResponseWriter, r *http.Request) {
	// 读取请求体
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
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

	// 加载现有配置以获取某些不在Web UI中的字段
	existingCfg, err := config.LoadConfig(s.configPath)
	if err == nil {
		// 保留一些 Web UI 中不能修改的字段
		if newCfg.AdBlock.Enable == false && existingCfg.AdBlock.Enable {
			// 保留 AdBlock 的一些私有状态（如果 Web UI 没有更新）
			// 这里仅保留 Enable 状态，其他字段由 Web UI 更新
		}
		if newCfg.System.LogLevel == "" && existingCfg.System.LogLevel != "" {
			newCfg.System.LogLevel = existingCfg.System.LogLevel
		}
		if newCfg.Stats.HotDomainsWindowHours == 0 && existingCfg.Stats.HotDomainsWindowHours > 0 {
			newCfg.Stats = existingCfg.Stats
		}
	}

	// 使用正确的YAML标签将配置序列化为YAML
	// 创建一个自定义编码器来确保格式正确
	yamlData, err := yaml.Marshal(newCfg)
	if err != nil {
		s.writeJSONError(w, "Failed to marshal config to YAML: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Debugf("Generated YAML:\n%s", string(yamlData))

	// 写入配置文件
	if err := s.writeConfigFile(yamlData); err != nil {
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

	if len(cfg.Upstream.Servers) == 0 {
		return fmt.Errorf("at least one upstream server is required")
	}
	if cfg.Upstream.TimeoutMs <= 0 {
		return fmt.Errorf("upstream timeout must be positive")
	}
	if cfg.Upstream.Strategy != "random" && cfg.Upstream.Strategy != "parallel" && cfg.Upstream.Strategy != "sequential" && cfg.Upstream.Strategy != "racing" {
		return fmt.Errorf("invalid upstream strategy: %s (must be 'random', 'parallel', 'sequential', or 'racing')", cfg.Upstream.Strategy)
	}
	if cfg.Upstream.Concurrency <= 0 {
		return fmt.Errorf("upstream concurrency must be positive")
	}

	// 验证 sequential 策略参数
	if cfg.Upstream.SequentialTimeout < 100 || cfg.Upstream.SequentialTimeout > 2000 {
		return fmt.Errorf("sequential timeout must be between 100ms and 2000ms, got %dms", cfg.Upstream.SequentialTimeout)
	}

	// 验证 racing 策略参数
	if cfg.Upstream.RacingDelay < 50 || cfg.Upstream.RacingDelay > 500 {
		return fmt.Errorf("racing delay must be between 50ms and 500ms, got %dms", cfg.Upstream.RacingDelay)
	}
	if cfg.Upstream.RacingMaxConcurrent < 2 || cfg.Upstream.RacingMaxConcurrent > 5 {
		return fmt.Errorf("racing max concurrent must be between 2 and 5, got %d", cfg.Upstream.RacingMaxConcurrent)
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
	if cfg.Ping.Strategy != "min" && cfg.Ping.Strategy != "avg" {
		return fmt.Errorf("system refresh workers cannot be negative")
	}
	if cfg.WebUI.ListenPort <= 0 || cfg.WebUI.ListenPort > 65535 {
		return fmt.Errorf("invalid WebUI listen port: %d", cfg.WebUI.ListenPort)
	}
	return nil
}
