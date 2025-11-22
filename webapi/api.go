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

	// 创建独立的 ServeMux，避免全局路由污染
	mux := http.NewServeMux()

	// 注册 API 路由
	mux.HandleFunc("/api/query", s.handleQuery)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/stats/clear", s.handleClearStats)
	mux.HandleFunc("/api/cache/clear", s.handleClearCache)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/recent-queries", s.handleRecentQueries)
	mux.HandleFunc("/api/hot-domains", s.handleHotDomains)
	mux.HandleFunc("/api/restart", s.handleRestart)
	mux.HandleFunc("/health", s.handleHealth)

	// 首先尝试使用内嵌的 web 文件
	webSubFS, err := fs.Sub(webFilesFS, "web")
	if err == nil {
		log.Println("Using embedded web files")
		mux.Handle("/", s.corsMiddleware(http.FileServer(http.FS(webSubFS))))
	} else {
		// 备选：查找 Web 静态文件目录
		webDir := s.findWebDirectory()
		if webDir == "" {
			log.Println("Warning: Could not find web directory. Web UI may not work properly.")
			log.Println("Expected locations: /var/lib/SmartDNSSort/web, /usr/share/smartdnssort/web, or ./web")
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

// corsMiddleware 添加 CORS 支持
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// 处理 preflight 请求
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeJSONError 写入统一格式的 JSON 错误响应
func (s *Server) writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Message: message,
	})
}

// writeJSONSuccess 写入统一格式的 JSON 成功响应
func (s *Server) writeJSONSuccess(w http.ResponseWriter, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// findWebDirectory 查找 Web 静态文件目录
// 按优先级查找多个可能的位置
func (s *Server) findWebDirectory() string {
	possiblePaths := []string{}

	// 首先：在可执行文件目录查找 web 目录（对 Windows 最有效）
	if exePath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(exePath)
		possiblePaths = append(possiblePaths,
			filepath.Join(execDir, "web"),
			filepath.Join(execDir, "..", "web"), // 上级目录的 web
		)
	}

	// 当前工作目录相对路径（开发环境）
	possiblePaths = append(possiblePaths,
		"./web",
		"web",
	)

	// Linux 系统路径（Linux 服务部署）
	possiblePaths = append(possiblePaths,
		"/var/lib/SmartDNSSort/web",   // Linux 服务部署（推荐）
		"/usr/share/smartdnssort/web", // FHS 标准路径
		"/etc/SmartDNSSort/web",       // 备选路径
	)

	for _, path := range possiblePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			// 确认 index.html 存在
			if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
				return path
			}
		}
	}

	return ""
}

// handleQuery 处理 DNS 查询 API
// 使用方法: GET /api/query?domain=example.com&type=A
// 返回: 包含 IP 和 RTT 信息的 JSON
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

	// 解析查询类型
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

	// Replicate the logic of the old Get method: prefer sorted, fallback to raw.
	var ipsResult []IPResult
	var status string

	sortedEntry, ok := s.dnsCache.GetSorted(domain, qtype)
	if ok {
		// Found in sorted cache
		status = "cached_sorted"
		for i, ip := range sortedEntry.IPs {
			rtt := 0
			if i < len(sortedEntry.RTTs) {
				rtt = sortedEntry.RTTs[i]
			}
			ipsResult = append(ipsResult, IPResult{IP: ip, RTT: rtt})
		}
	} else {
		rawEntry, ok := s.dnsCache.GetRaw(domain, qtype)
		if ok {
			// Found in raw cache
			status = "cached_raw"
			for _, ip := range rawEntry.IPs {
				ipsResult = append(ipsResult, IPResult{IP: ip, RTT: 0}) // RTT is not available for raw entries
			}
		}
	}

	if len(ipsResult) == 0 {
		http.Error(w, "Domain not found in cache", http.StatusNotFound)
		return
	}

	// 构造响应
	result := QueryResult{
		Domain: domain,
		Type:   queryType,
		IPs:    ipsResult,
		Status: status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleStats 处理统计信息 API
// 使用方法: GET /api/stats
// 返回: DNS 查询统计信息
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	stats := s.dnsServer.GetStats()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		log.Printf("[ERROR] Failed to encode stats: %v", err)
		s.writeJSONError(w, "Failed to encode stats", http.StatusInternalServerError)
	}
}

// handleHealth 健康检查
// 使用方法: GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"healthy"}`)
}

// Stop 停止 Web API 服务
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	log.Println("Shutting down Web API server...")
	return s.listener.Shutdown(ctx)
}

// handleClearCache handles clearing the DNS cache.
// Usage: POST /api/cache/clear
func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	s.dnsCache.Clear()
	log.Println("DNS cache cleared via API request.")
	s.writeJSONSuccess(w, "Cache cleared successfully", nil)
}

// handleClearStats handles clearing all statistics.
// Usage: POST /api/stats/clear
func (s *Server) handleClearStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	s.dnsServer.ClearStats()
	log.Println("Statistics cleared via API request.")
	s.writeJSONSuccess(w, "All stats cleared successfully", nil)
}

// handleRecentQueries handles fetching the recent queries list.
// Usage: GET /api/recent-queries
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

// handleHotDomains handles fetching the hot domains list.
// Usage: GET /api/hot-domains
func (s *Server) handleHotDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// We need to access the stats object from the dnsServer to get the top domains
	stats := s.dnsServer.GetStats()
	topDomainsList, ok := stats["top_domains"]
	if !ok || topDomainsList == nil {
		// 返回空列表而不是 null
		topDomainsList = []interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(topDomainsList); err != nil {
		log.Printf("[ERROR] Failed to encode hot domains: %v", err)
		s.writeJSONError(w, "Failed to encode hot domains", http.StatusInternalServerError)
	}
}

// handleConfig handles getting and setting the configuration.
// GET /api/config - returns the current running configuration.
// POST /api/config - receives a new configuration, saves it, and applies it.
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

	// 1. Validate the new configuration
	if err := s.validateConfig(&newCfg); err != nil {
		s.writeJSONError(w, "Configuration validation failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 2. Save the new configuration to the YAML file
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

	// 3. Apply the new configuration to the running server (hot-reload)
	if err := s.dnsServer.ApplyConfig(&newCfg); err != nil {
		s.writeJSONError(w, "Failed to apply new configuration: "+err.Error(), http.StatusInternalServerError)
		// Note: At this point, the config file is updated, but the server state is not.
		// This might require a manual restart.
		return
	}
	log.Println("Configuration hot-reloaded successfully.")

	s.writeJSONSuccess(w, "Configuration updated and applied successfully", nil)
}

// validateConfig 验证配置的有效性
func (s *Server) validateConfig(cfg *config.Config) error {
	// DNS 配置验证
	if cfg.DNS.ListenPort <= 0 || cfg.DNS.ListenPort > 65535 {
		return fmt.Errorf("invalid DNS listen port: %d", cfg.DNS.ListenPort)
	}

	// Upstream 配置验证
	if len(cfg.Upstream.Servers) == 0 {
		return fmt.Errorf("at least one upstream server is required")
	}
	if cfg.Upstream.TimeoutMs <= 0 {
		return fmt.Errorf("upstream timeout must be positive")
	}
	if cfg.Upstream.Concurrency <= 0 {
		return fmt.Errorf("upstream concurrency must be positive")
	}

	// Cache 配置验证
	if cfg.Cache.MinTTLSeconds < 0 {
		return fmt.Errorf("cache min TTL cannot be negative")
	}
	if cfg.Cache.MaxTTLSeconds <= 0 {
		return fmt.Errorf("cache max TTL must be positive")
	}
	if cfg.Cache.MinTTLSeconds > cfg.Cache.MaxTTLSeconds {
		return fmt.Errorf("cache min TTL cannot be greater than max TTL")
	}

	// Ping 配置验证
	if cfg.Ping.Count <= 0 {
		return fmt.Errorf("ping count must be positive")
	}
	if cfg.Ping.TimeoutMs <= 0 {
		return fmt.Errorf("ping timeout must be positive")
	}
	if cfg.Ping.Concurrency <= 0 {
		return fmt.Errorf("ping concurrency must be positive")
	}

	// WebUI 配置验证
	if cfg.WebUI.ListenPort <= 0 || cfg.WebUI.ListenPort > 65535 {
		return fmt.Errorf("invalid WebUI listen port: %d", cfg.WebUI.ListenPort)
	}

	return nil
}

// handleRestart handles restarting the service.
// Usage: POST /api/restart
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	log.Println("Service restart requested via API.")

	// 先响应客户端
	s.writeJSONSuccess(w, "Service restart initiated", nil)

	// 在goroutine中执行重启,避免阻塞响应
	if s.restartFunc != nil {
		go func() {
			log.Println("Executing restart function...")
			s.restartFunc()
		}()
	} else {
		log.Println("No restart function configured. Please restart manually.")
	}
}
