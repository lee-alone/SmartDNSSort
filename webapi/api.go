package webapi

import (
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

	"github.com/miekg/dns"
	"gopkg.in/yaml.v3"
)

//go:embed web/*
var webFilesFS embed.FS

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
	cfg        *config.Config
	dnsCache   *cache.Cache
	dnsServer  *dnsserver.Server
	listener   http.Server
	configPath string // Store the path to the config file
}

// NewServer 创建新的 Web API 服务器
func NewServer(cfg *config.Config, dnsCache *cache.Cache, dnsServer *dnsserver.Server, configPath string) *Server {
	return &Server{
		cfg:        cfg,
		dnsCache:   dnsCache,
		dnsServer:  dnsServer,
		configPath: configPath,
	}
}

// Start 启动 Web API 服务
func (s *Server) Start() error {
	if !s.cfg.WebUI.Enabled {
		log.Println("WebAPI is disabled")
		return nil
	}

	addr := fmt.Sprintf(":%d", s.cfg.WebUI.ListenPort)

	// 注册 API 路由
	http.HandleFunc("/api/query", s.handleQuery)
	http.HandleFunc("/api/stats", s.handleStats)
	http.HandleFunc("/api/stats/clear", s.handleClearStats) // New
	http.HandleFunc("/api/cache/clear", s.handleClearCache)
	http.HandleFunc("/api/config", s.handleConfig) // New endpoint for config
	http.HandleFunc("/api/recent-queries", s.handleRecentQueries) // New
	http.HandleFunc("/api/hot-domains", s.handleHotDomains)       // New
	http.HandleFunc("/health", s.handleHealth)

	// 首先尝试使用内嵌的 web 文件
	webSubFS, err := fs.Sub(webFilesFS, "web")
	if err == nil {
		log.Println("Using embedded web files")
		http.Handle("/", http.FileServer(http.FS(webSubFS)))
	} else {
		// 备选：查找 Web 静态文件目录
		webDir := s.findWebDirectory()
		if webDir == "" {
			log.Println("Warning: Could not find web directory. Web UI may not work properly.")
			log.Println("Expected locations: /var/lib/SmartDNSSort/web, /usr/share/smartdnssort/web, or ./web")
		} else {
			log.Printf("Using web directory: %s\n", webDir)
			fsServer := http.FileServer(http.Dir(webDir))
			http.Handle("/", fsServer)
		}
	}

	s.listener = http.Server{Addr: addr}

	log.Printf("Web API server started on http://localhost:%d\n", s.cfg.WebUI.ListenPort)
	return s.listener.ListenAndServe()
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
	stats := s.dnsServer.GetStats()

	// 添加配置信息
	stats["cache_config"] = map[string]interface{}{
		"min_ttl_seconds": s.cfg.Cache.MinTTLSeconds,
		"max_ttl_seconds": s.cfg.Cache.MaxTTLSeconds,
	}
	stats["upstream_config"] = map[string]interface{}{
		"strategy":    s.cfg.Upstream.Strategy,
		"timeout_ms":  s.cfg.Upstream.TimeoutMs,
		"concurrency": s.cfg.Upstream.Concurrency,
	}
	stats["ping_config"] = map[string]interface{}{
		"count":       s.cfg.Ping.Count,
		"timeout_ms":  s.cfg.Ping.TimeoutMs,
		"concurrency": s.cfg.Ping.Concurrency,
		"strategy":    s.cfg.Ping.Strategy,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleHealth 健康检查
// 使用方法: GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"healthy"}`)
}

// Stop 停止 Web API 服务
func (s *Server) Stop() error {
	return s.listener.Close()
}

// handleClearCache handles clearing the DNS cache.
// Usage: POST /api/cache/clear
func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	s.dnsCache.Clear()
	log.Println("DNS cache cleared via API request.")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Cache cleared successfully"))
}

// handleClearStats handles clearing all statistics.
// Usage: POST /api/stats/clear
func (s *Server) handleClearStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}
	s.dnsServer.ClearStats()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("All stats cleared successfully"))
}

// handleRecentQueries handles fetching the recent queries list.
// Usage: GET /api/recent-queries
func (s *Server) handleRecentQueries(w http.ResponseWriter, r *http.Request) {
	queries := s.dnsServer.GetRecentQueries()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(queries); err != nil {
		http.Error(w, "Failed to encode recent queries: "+err.Error(), http.StatusInternalServerError)
	}
}

// handleHotDomains handles fetching the hot domains list.
// Usage: GET /api/hot-domains
func (s *Server) handleHotDomains(w http.ResponseWriter, r *http.Request) {
	// We need to access the stats object from the dnsServer to get the top domains
	stats := s.dnsServer.GetStats()
	topDomainsList := stats["top_domains"]

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(topDomainsList); err != nil {
		http.Error(w, "Failed to encode hot domains: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to decode new config: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 1. Validate the new configuration (basic validation)
	// More complex validation can be added here.
	if newCfg.DNS.ListenPort <= 0 {
		http.Error(w, "Invalid DNS listen port", http.StatusBadRequest)
		return
	}

	// 2. Save the new configuration to the YAML file
	yamlData, err := yaml.Marshal(&newCfg)
	if err != nil {
		http.Error(w, "Failed to marshal new config to YAML: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(s.configPath, yamlData, 0644); err != nil {
		http.Error(w, "Failed to write config file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Configuration saved to %s", s.configPath)

	// 3. Apply the new configuration to the running server (hot-reload)
	if err := s.dnsServer.ApplyConfig(&newCfg); err != nil {
		http.Error(w, "Failed to apply new configuration: "+err.Error(), http.StatusInternalServerError)
		// Note: At this point, the config file is updated, but the server state is not.
		// This might require a manual restart.
		return
	}
	log.Println("Configuration hot-reloaded successfully.")

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Configuration updated and applied successfully."))
}
