package webapi

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/dnsserver"
	"strings"
	"time"

	"github.com/miekg/dns"
	"gopkg.in/yaml.v3"
)

//go:embed web/*
var webFilesFS embed.FS

// APIResponse 统一的 API 响应格式
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// QueryResult API 返回格式
type QueryResult struct {
	Domain string     `json:"domain"`
	Type   string     `json:"type"`
	IPs    []IPResult `json:"ips"`
	Status string     `json:"status"`
}

// IPResult 单个 IP 的结果，包含 RTT
type IPResult struct {
	IP  string `json:"ip"`
	RTT int    `json:"rtt"` // 毫秒
}

// Server Web API 服务器
type Server struct {
	cfg         *config.Config
	dnsCache    *cache.Cache
	dnsServer   *dnsserver.Server
	listener    http.Server
	configPath  string // Store the path to the config file
	restartFunc func() // 重启服务的回调函数
}

// NewServer 创建新的 Web API 服务器
func NewServer(cfg *config.Config, dnsCache *cache.Cache, dnsServer *dnsserver.Server, configPath string, restartFunc func()) *Server {
	return &Server{
		cfg:         cfg,
		dnsCache:    dnsCache,
		dnsServer:   dnsServer,
		configPath:  configPath,
		restartFunc: restartFunc,
	}
}

// Start 启动 Web API 服务
func (s *Server) Start() error {
	if !s.cfg.WebUI.Enabled {
		log.Println("WebAPI is disabled")
		return nil
	}

	addr := fmt.Sprintf(":%d", s.cfg.WebUI.ListenPort)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/query", s.handleQuery)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/stats/clear", s.handleClearStats)
	mux.HandleFunc("/api/cache/clear", s.handleClearCache)
	mux.HandleFunc("/api/cache/memory", s.handleCacheMemoryStats)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/recent-queries", s.handleRecentQueries)
	mux.HandleFunc("/api/hot-domains", s.handleHotDomains)
	mux.HandleFunc("/api/restart", s.handleRestart)
	mux.HandleFunc("/health", s.handleHealth)

	// AdBlock API routes
	mux.HandleFunc("/api/adblock/status", s.handleAdBlockStatus)
	mux.HandleFunc("/api/adblock/sources", s.handleAdBlockSources) // GET for list, POST for add, DELETE for remove
	mux.HandleFunc("/api/adblock/update", s.handleAdBlockUpdate)
	mux.HandleFunc("/api/adblock/toggle", s.handleAdBlockToggle)
	mux.HandleFunc("/api/adblock/test", s.handleAdBlockTest)
	mux.HandleFunc("/api/adblock/blockmode", s.handleAdBlockBlockMode)

	webSubFS, err := fs.Sub(webFilesFS, "web")
	if err == nil {
		log.Println("Using embedded web files")
		mux.Handle("/", s.corsMiddleware(http.FileServer(http.FS(webSubFS))))
	} else {
		webDir := s.findWebDirectory()
		if webDir == "" {
			log.Println("Warning: Could not find web directory. Web UI may not work properly.")
		} else {
			log.Printf("Using web directory: %s\n", webDir)
			fsServer := http.FileServer(http.Dir(webDir))
			mux.Handle("/", s.corsMiddleware(fsServer))
		}
	}

	s.listener = http.Server{
		Addr:    addr,
		Handler: mux,
	}

	log.Printf("Web API server started on http://localhost:%d\n", s.cfg.WebUI.ListenPort)
	return s.listener.ListenAndServe()
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Message: message,
	})
}

func (s *Server) writeJSONSuccess(w http.ResponseWriter, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

func (s *Server) findWebDirectory() string {
	possiblePaths := []string{}
	if exePath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(exePath)
		possiblePaths = append(possiblePaths,
			filepath.Join(execDir, "web"),
			filepath.Join(execDir, "..", "web"),
		)
	}
	possiblePaths = append(possiblePaths, "./web", "web")
	possiblePaths = append(possiblePaths, "/var/lib/SmartDNSSort/web", "/usr/share/smartdnssort/web", "/etc/SmartDNSSort/web")

	for _, path := range possiblePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
				return path
			}
		}
	}
	return ""
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	queryType := r.URL.Query().Get("type")

	if domain == "" {
		http.Error(w, "Missing domain parameter", http.StatusBadRequest)
		return
	}
	if queryType == "" {
		queryType = "A"
	}

	var qtype uint16
	switch strings.ToUpper(queryType) {
	case "A":
		qtype = dns.TypeA
	case "AAAA":
		qtype = dns.TypeAAAA
	default:
		http.Error(w, "Invalid query type (must be A or AAAA)", http.StatusBadRequest)
		return
	}

	var ipsResult []IPResult
	var status string

	if sortedEntry, ok := s.dnsCache.GetSorted(domain, qtype); ok {
		status = "cached_sorted"
		for i, ip := range sortedEntry.IPs {
			rtt := 0
			if i < len(sortedEntry.RTTs) {
				rtt = sortedEntry.RTTs[i]
			}
			ipsResult = append(ipsResult, IPResult{IP: ip, RTT: rtt})
		}
	} else if rawEntry, ok := s.dnsCache.GetRaw(domain, qtype); ok {
		status = "cached_raw"
		for _, ip := range rawEntry.IPs {
			ipsResult = append(ipsResult, IPResult{IP: ip, RTT: 0})
		}
	}

	if len(ipsResult) == 0 {
		http.Error(w, "Domain not found in cache", http.StatusNotFound)
		return
	}

	result := QueryResult{
		Domain: domain,
		Type:   queryType,
		IPs:    ipsResult,
		Status: status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	stats := s.dnsServer.GetStats()
	cacheCfg := s.dnsServer.GetConfig().Cache
	currentEntries := s.dnsCache.GetCurrentEntries()
	expiredEntries := s.dnsCache.GetExpiredEntries()
	maxEntries := cacheCfg.CalculateMaxEntries()
	var memoryPercent float64
	if maxEntries > 0 {
		memoryPercent = (float64(currentEntries) / float64(maxEntries)) * 100
	}

	var expiredPercent float64
	if currentEntries > 0 {
		expiredPercent = (float64(expiredEntries) / float64(currentEntries)) * 100
	}

	stats["cache_memory_stats"] = map[string]interface{}{
		"max_memory_mb":     cacheCfg.MaxMemoryMB,
		"max_entries":       maxEntries,
		"current_entries":   currentEntries,
		"current_memory_mb": int(float64(currentEntries) * config.AvgBytesPerDomain / (1024 * 1024)),
		"memory_percent":    memoryPercent,
		"expired_entries":   expiredEntries,
		"expired_percent":   expiredPercent,
		"protected_entries": s.dnsCache.GetProtectedEntries(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		log.Printf("[ERROR] Failed to encode stats: %v", err)
		s.writeJSONError(w, "Failed to encode stats", http.StatusInternalServerError)
	}
}

func (s *Server) handleCacheMemoryStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	cacheCfg := s.dnsServer.GetConfig().Cache
	currentEntries := s.dnsCache.GetCurrentEntries()
	maxEntries := cacheCfg.CalculateMaxEntries()
	expiredEntries := s.dnsCache.GetExpiredEntries()
	protectedEntries := s.dnsCache.GetProtectedEntries()

	var memoryPercent float64
	if maxEntries > 0 {
		memoryPercent = (float64(currentEntries) / float64(maxEntries)) * 100
	}

	var expiredPercent float64
	if currentEntries > 0 {
		expiredPercent = (float64(expiredEntries) / float64(currentEntries)) * 100
	}

	stats := map[string]interface{}{
		"max_memory_mb":     cacheCfg.MaxMemoryMB,
		"max_entries":       maxEntries,
		"current_entries":   currentEntries,
		"current_memory_mb": int(float64(currentEntries) * config.AvgBytesPerDomain / (1024 * 1024)),
		"memory_percent":    memoryPercent,
		"expired_entries":   expiredEntries,
		"expired_percent":   expiredPercent,
		"protected_entries": protectedEntries,
	}

	s.writeJSONSuccess(w, "Cache memory stats retrieved successfully", stats)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"healthy"}`)
}

func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	log.Println("Shutting down Web API server...")
	return s.listener.Shutdown(ctx)
}

func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	s.dnsCache.Clear()
	log.Println("DNS cache cleared via API request.")
	s.writeJSONSuccess(w, "Cache cleared successfully", nil)
}

func (s *Server) handleClearStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	s.dnsServer.ClearStats()
	log.Println("Statistics cleared via API request.")
	s.writeJSONSuccess(w, "All stats cleared successfully", nil)
}

func (s *Server) handleRecentQueries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	queries := s.dnsServer.GetRecentQueries()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(queries); err != nil {
		log.Printf("[ERROR] Failed to encode recent queries: %v", err)
		s.writeJSONError(w, "Failed to encode recent queries", http.StatusInternalServerError)
	}
}

func (s *Server) handleHotDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	stats := s.dnsServer.GetStats()
	topDomainsList, ok := stats["top_domains"]
	if !ok || topDomainsList == nil {
		topDomainsList = []interface{}{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(topDomainsList); err != nil {
		log.Printf("[ERROR] Failed to encode hot domains: %v", err)
		s.writeJSONError(w, "Failed to encode hot domains", http.StatusInternalServerError)
	}
}

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

func (s *Server) handleGetConfig(w http.ResponseWriter) {
	currentConfig := s.dnsServer.GetConfig()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(currentConfig); err != nil {
		log.Printf("[ERROR] Failed to encode config for API: %v", err)
		http.Error(w, "Failed to encode config: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handlePostConfig(w http.ResponseWriter, r *http.Request) {
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		s.writeJSONError(w, "Failed to decode new config: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.validateConfig(&newCfg); err != nil {
		s.writeJSONError(w, "Configuration validation failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	yamlData, err := yaml.Marshal(&newCfg)
	if err != nil {
		s.writeJSONError(w, "Failed to marshal new config to YAML: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(s.configPath, yamlData, 0644); err != nil {
		s.writeJSONError(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Configuration saved to %s", s.configPath)
	if err := s.dnsServer.ApplyConfig(&newCfg); err != nil {
		s.writeJSONError(w, "Failed to apply new configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Println("Configuration hot-reloaded successfully.")
	s.writeJSONSuccess(w, "Configuration updated and applied successfully", nil)
}

func (s *Server) validateConfig(cfg *config.Config) error {
	if cfg.DNS.ListenPort <= 0 || cfg.DNS.ListenPort > 65535 {
		return fmt.Errorf("invalid DNS listen port: %d", cfg.DNS.ListenPort)
	}

	// Sanitize Upstream Servers (remove quotes and spaces)
	for i, server := range cfg.Upstream.Servers {
		cfg.Upstream.Servers[i] = strings.Trim(server, "\"' ")
	}
	// Sanitize Bootstrap DNS
	for i, server := range cfg.Upstream.BootstrapDNS {
		cfg.Upstream.BootstrapDNS[i] = strings.Trim(server, "\"' ")
	}

	if len(cfg.Upstream.Servers) == 0 {
		return fmt.Errorf("at least one upstream server is required")
	}
	if cfg.Upstream.TimeoutMs <= 0 {
		return fmt.Errorf("upstream timeout must be positive")
	}
	if cfg.Upstream.Strategy != "random" && cfg.Upstream.Strategy != "parallel" {
		return fmt.Errorf("invalid upstream strategy: %s (must be 'random' or 'parallel')", cfg.Upstream.Strategy)
	}
	if cfg.Upstream.Concurrency <= 0 {
		return fmt.Errorf("upstream concurrency must be positive")
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

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	log.Println("Service restart requested via API.")
	s.writeJSONSuccess(w, "Service restart initiated", nil)
	if s.restartFunc != nil {
		go func() {
			log.Println("Executing restart function...")
			s.restartFunc()
		}()
	} else {
		log.Println("No restart function configured. Please restart manually.")
	}
}

// handleAdBlockStatus handles requests for adblock status.
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

// handleAdBlockSources handles requests for adblock sources.
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
			s.writeJSONError(w, "Failed to add source: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Also add to config.yaml
		if err := s.addSourceToConfig(payload.URL); err != nil {
			log.Printf("[AdBlock] Warning: Failed to add source to config: %v", err)
		}

		// Trigger an update in the background
		go func() {
			log.Printf("[AdBlock] Auto-updating rules after adding new source: %s", payload.URL)
			if _, err := adblockMgr.UpdateRules(true); err != nil {
				log.Printf("[AdBlock] Auto-update failed after adding source: %v", err)
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
			s.writeJSONError(w, "Failed to remove source: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Also remove from config.yaml
		if err := s.removeSourceFromConfig(payload.URL); err != nil {
			log.Printf("[AdBlock] Warning: Failed to remove source from config: %v", err)
		}

		// If the removed source is the custom rules file, also clear it from config
		if payload.URL == s.cfg.AdBlock.CustomRulesFile {
			if err := s.removeCustomRulesFromConfig(); err != nil {
				log.Printf("[AdBlock] Warning: Failed to remove custom rules file from config: %v", err)
			}
		}

		s.writeJSONSuccess(w, "AdBlock source removed successfully", nil)

	default:
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

// handleAdBlockUpdate handles requests to trigger a manual adblock rule update.
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

	go func() {
		// Run in a goroutine to not block the API response
		result, err := adblockMgr.UpdateRules(true) // force update
		if err != nil {
			log.Printf("[AdBlock] Manual update failed: %v", err)
			return
		}
		log.Printf("[AdBlock] Manual update completed: %+v", result)
	}()

	s.writeJSONSuccess(w, "AdBlock rule update started", nil)
}

// handleAdBlockToggle handles toggling the adblock feature.
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

	// Update in-memory config
	s.dnsServer.SetAdBlockEnabled(payload.Enabled)

	// Load current config from file
	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		s.writeJSONError(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update adblock enable field
	cfg.AdBlock.Enable = payload.Enabled

	// Save to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		s.writeJSONError(w, "Failed to marshal config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(s.configPath, yamlData, 0644); err != nil {
		s.writeJSONError(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[AdBlock] Status toggled to: %v", payload.Enabled)
	s.writeJSONSuccess(w, "AdBlock status updated successfully", nil)
}

// addSourceToConfig adds a rule source URL to config.yaml
func (s *Server) addSourceToConfig(url string) error {
	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		return err
	}

	// Check if already exists
	for _, u := range cfg.AdBlock.RuleURLs {
		if u == url {
			return nil // Already exists
		}
	}

	// Add to list
	cfg.AdBlock.RuleURLs = append(cfg.AdBlock.RuleURLs, url)

	// Save back to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(s.configPath, yamlData, 0644)
}

// removeSourceFromConfig removes a rule source URL from config.yaml
func (s *Server) removeSourceFromConfig(url string) error {
	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		return err
	}

	// Filter out the URL
	var newURLs []string
	for _, u := range cfg.AdBlock.RuleURLs {
		if u != url {
			newURLs = append(newURLs, u)
		}
	}
	cfg.AdBlock.RuleURLs = newURLs

	// Save back to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(s.configPath, yamlData, 0644)
}

// removeCustomRulesFromConfig removes the custom rules file path from config.yaml
func (s *Server) removeCustomRulesFromConfig() error {
	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		return err
	}

	// Set CustomRulesFile to empty
	cfg.AdBlock.CustomRulesFile = ""

	// Save back to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(s.configPath, yamlData, 0644)
}

// handleAdBlockTest handles testing a domain against the adblock filter.
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

// handleAdBlockBlockMode handles requests to change the adblock block mode.
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

	// Load current config from file
	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		s.writeJSONError(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update block mode
	cfg.AdBlock.BlockMode = payload.BlockMode

	// Save to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		s.writeJSONError(w, "Failed to marshal config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(s.configPath, yamlData, 0644); err != nil {
		s.writeJSONError(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Apply the new config to the running server
	if err := s.dnsServer.ApplyConfig(cfg); err != nil {
		s.writeJSONError(w, "Failed to apply new configuration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[AdBlock] Block mode changed to: %s", payload.BlockMode)
	s.writeJSONSuccess(w, "Block mode updated successfully", nil)
}
