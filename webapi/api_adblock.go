package webapi

import (
	"encoding/json"
	"net/http"
	"smartdnssort/config"
	"smartdnssort/logger"

	"gopkg.in/yaml.v3"
)

// handleAdBlockStatus 处理广告拦截状态请求
func (s *Server) handleAdBlockStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	adblockMgr := s.dnsServer.GetAdBlockManager()
	if adblockMgr == nil {
		s.writeJSONSuccess(w, "AdBlock is disabled", map[string]interface{}{
			"enabled": false,
		})
		return
	}

	stats := adblockMgr.GetStats()
	s.writeJSONSuccess(w, "AdBlock status retrieved successfully", stats)
}

// handleAdBlockSources 处理广告拦截源请求
func (s *Server) handleAdBlockSources(w http.ResponseWriter, r *http.Request) {
	adblockMgr := s.dnsServer.GetAdBlockManager()
	if adblockMgr == nil {
		s.writeJSONError(w, "AdBlock is disabled", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		sources := adblockMgr.GetSources()
		s.writeJSONSuccess(w, "AdBlock sources retrieved successfully", sources)
	case http.MethodPost: // Corresponds to /api/adblock/sources/add
		var payload struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if payload.URL == "" {
			s.writeJSONError(w, "URL cannot be empty", http.StatusBadRequest)
			return
		}
		// Add to AdBlock manager
		if err := adblockMgr.AddSource(payload.URL); err != nil {
			logger.Errorf("[AdBlock] Failed to add source %s: %v", payload.URL, err)
			s.writeJSONError(w, "Failed to add source: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Also add to config.yaml
		if err := s.addSourceToConfig(payload.URL); err != nil {
			logger.Warnf("[AdBlock] Failed to add source to config: %v", err)
		}

		// Trigger an update in the background
		go func() {
			logger.Infof("[AdBlock] Auto-updating rules after adding new source: %s", payload.URL)
			if _, err := adblockMgr.UpdateRules(true); err != nil {
				logger.Errorf("[AdBlock] Auto-update failed after adding source: %v", err)
			}
		}()

		s.writeJSONSuccess(w, "AdBlock source added successfully, update started.", nil)

	case http.MethodPut: // Enable/disable a source
		var payload struct {
			URL     string `json:"url"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if payload.URL == "" {
			s.writeJSONError(w, "URL cannot be empty", http.StatusBadRequest)
			return
		}
		if err := adblockMgr.SetSourceEnabled(payload.URL, payload.Enabled); err != nil {
			logger.Errorf("[AdBlock] Failed to set source %s enabled to %v: %v", payload.URL, payload.Enabled, err)
			s.writeJSONError(w, "Failed to update source: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.writeJSONSuccess(w, "AdBlock source status updated successfully", nil)

	case http.MethodDelete: // Corresponds to /api/adblock/sources/remove
		var payload struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if payload.URL == "" {
			s.writeJSONError(w, "URL cannot be empty", http.StatusBadRequest)
			return
		}
		// Remove from AdBlock manager
		if err := adblockMgr.RemoveSource(payload.URL); err != nil {
			logger.Errorf("[AdBlock] Failed to remove source %s: %v", payload.URL, err)
			s.writeJSONError(w, "Failed to remove source: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Also remove from config.yaml
		if err := s.removeSourceFromConfig(payload.URL); err != nil {
			logger.Warnf("[AdBlock] Failed to remove source from config: %v", err)
		}

		// If the removed source is the custom rules file, also clear it from config
		if payload.URL == s.cfg.AdBlock.CustomRulesFile {
			if err := s.removeCustomRulesFromConfig(); err != nil {
				logger.Warnf("[AdBlock] Failed to remove custom rules file from config: %v", err)
			}
		}

		s.writeJSONSuccess(w, "AdBlock source removed successfully", nil)

	default:
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

// handleAdBlockUpdate 处理广告拦截规则更新请求
func (s *Server) handleAdBlockUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	adblockMgr := s.dnsServer.GetAdBlockManager()
	if adblockMgr == nil {
		s.writeJSONError(w, "AdBlock is disabled", http.StatusServiceUnavailable)
		return
	}

	// 检查是否有更新正在进行中
	s.adblockMutex.Lock()
	if s.isAdblockBusy {
		s.adblockMutex.Unlock()
		s.writeJSONError(w, "AdBlock update is already in progress, please wait", http.StatusConflict)
		return
	}
	s.isAdblockBusy = true
	s.adblockMutex.Unlock()

	go func() {
		defer func() {
			// 更新完成后重置标志
			s.adblockMutex.Lock()
			s.isAdblockBusy = false
			s.adblockMutex.Unlock()
		}()
		// Run in a goroutine to not block the API response
		result, err := adblockMgr.UpdateRules(true) // force update
		if err != nil {
			logger.Errorf("[AdBlock] Manual update failed: %v", err)
			return
		}
		logger.Infof("[AdBlock] Manual update completed: %+v", result)
	}()

	s.writeJSONSuccess(w, "AdBlock rule update started", nil)
}

// handleAdBlockToggle 处理广告拦截开关请求
func (s *Server) handleAdBlockToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 加锁保护配置文件操作
	s.cfgMutex.Lock()
	defer s.cfgMutex.Unlock()

	// Update in-memory config
	s.dnsServer.SetAdBlockEnabled(payload.Enabled)

	// Load current config from file
	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		logger.Errorf("[AdBlock] Failed to load config during toggle: %v", err)
		s.writeJSONError(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update adblock enable field
	cfg.AdBlock.Enable = payload.Enabled

	// Save to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		logger.Errorf("[AdBlock] Failed to marshal config during toggle: %v", err)
		s.writeJSONError(w, "Failed to marshal config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.writeConfigFile(yamlData); err != nil {
		logger.Errorf("[AdBlock] Failed to write config file during toggle: %v", err)
		s.writeJSONError(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infof("[AdBlock] Status toggled to: %v", payload.Enabled)
	s.writeJSONSuccess(w, "AdBlock status updated successfully", nil)
}

// handleAdBlockTest 处理广告拦截测试请求
func (s *Server) handleAdBlockTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	adblockMgr := s.dnsServer.GetAdBlockManager()
	if adblockMgr == nil {
		s.writeJSONError(w, "AdBlock is disabled", http.StatusServiceUnavailable)
		return
	}

	var payload struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if payload.Domain == "" {
		s.writeJSONError(w, "Domain cannot be empty", http.StatusBadRequest)
		return
	}

	result := adblockMgr.TestDomain(payload.Domain)
	s.writeJSONSuccess(w, "Domain test complete", result)
}

// handleAdBlockBlockMode 处理广告拦截模式请求
func (s *Server) handleAdBlockBlockMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		BlockMode string `json:"block_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate block mode
	if payload.BlockMode != "nxdomain" && payload.BlockMode != "refused" && payload.BlockMode != "zero_ip" {
		s.writeJSONError(w, "Invalid block mode. Must be 'nxdomain', 'refused', or 'zero_ip'", http.StatusBadRequest)
		return
	}

	// 加锁保护配置文件操作
	s.cfgMutex.Lock()
	defer s.cfgMutex.Unlock()

	// Load current config from file
	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		logger.Errorf("[AdBlock] Failed to load config for block mode change: %v", err)
		s.writeJSONError(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update block mode
	cfg.AdBlock.BlockMode = payload.BlockMode

	// Save to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		logger.Errorf("[AdBlock] Failed to marshal config for block mode change: %v", err)
		s.writeJSONError(w, "Failed to marshal config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.writeConfigFile(yamlData); err != nil {
		logger.Errorf("[AdBlock] Failed to write config file for block mode change: %v", err)
		s.writeJSONError(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply the new config to the running server
	if err := s.dnsServer.ApplyConfig(cfg); err != nil {
		logger.Errorf("[AdBlock] Failed to apply config for block mode change: %v", err)
		s.writeJSONError(w, "Failed to apply new configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Infof("[AdBlock] Block mode changed to: %s", payload.BlockMode)
	s.writeJSONSuccess(w, "Block mode updated successfully", nil)
}

// handleAdBlockSettings 处理广告拦截设置请求
func (s *Server) handleAdBlockSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetAdBlockSettings(w)
	case http.MethodPost:
		s.handlePostAdBlockSettings(w, r)
	default:
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

// handleGetAdBlockSettings 获取广告拦截设置
func (s *Server) handleGetAdBlockSettings(w http.ResponseWriter) {
	currentConfig := s.dnsServer.GetConfig()
	settings := map[string]interface{}{
		"update_interval_hours": currentConfig.AdBlock.UpdateIntervalHours,
		"max_cache_age_hours":   currentConfig.AdBlock.MaxCacheAgeHours,
		"max_cache_size_mb":     currentConfig.AdBlock.MaxCacheSizeMB,
		"blocked_ttl":           currentConfig.AdBlock.BlockedTTL,
	}
	s.writeJSONSuccess(w, "AdBlock settings retrieved successfully", settings)
}

// handlePostAdBlockSettings 更新广告拦截设置
func (s *Server) handlePostAdBlockSettings(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		UpdateIntervalHours int `json:"update_interval_hours"`
		MaxCacheAgeHours    int `json:"max_cache_age_hours"`
		MaxCacheSizeMB      int `json:"max_cache_size_mb"`
		BlockedTTL          int `json:"blocked_ttl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Basic validation
	if payload.UpdateIntervalHours < 0 || payload.MaxCacheAgeHours < 0 || payload.MaxCacheSizeMB < 0 || payload.BlockedTTL < 0 {
		s.writeJSONError(w, "Values cannot be negative", http.StatusBadRequest)
		return
	}

	// 加锁保护配置文件操作
	s.cfgMutex.Lock()
	defer s.cfgMutex.Unlock()

	// Load current config from file
	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		logger.Errorf("[AdBlock] Failed to load config for settings update: %v", err)
		s.writeJSONError(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update adblock settings
	cfg.AdBlock.UpdateIntervalHours = payload.UpdateIntervalHours
	cfg.AdBlock.MaxCacheAgeHours = payload.MaxCacheAgeHours
	cfg.AdBlock.MaxCacheSizeMB = payload.MaxCacheSizeMB
	cfg.AdBlock.BlockedTTL = payload.BlockedTTL

	// Save to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		logger.Errorf("[AdBlock] Failed to marshal config for settings update: %v", err)
		s.writeJSONError(w, "Failed to marshal config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.writeConfigFile(yamlData); err != nil {
		logger.Errorf("[AdBlock] Failed to write config file for settings update: %v", err)
		s.writeJSONError(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply the new config to the running server
	if err := s.dnsServer.ApplyConfig(cfg); err != nil {
		logger.Errorf("[AdBlock] Failed to apply config for settings update: %v", err)
		s.writeJSONError(w, "Failed to apply new configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logger.Info("[AdBlock] Settings updated via API")
	s.writeJSONSuccess(w, "AdBlock settings updated successfully", nil)
}
