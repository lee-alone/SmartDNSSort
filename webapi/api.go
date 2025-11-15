package webapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/dnsserver"
	"strings"

	"github.com/miekg/dns"
)

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
	cfg       *config.Config
	dnsCache  *cache.Cache
	dnsServer *dnsserver.Server
	listener  http.Server
}

// NewServer 创建新的 Web API 服务器
func NewServer(cfg *config.Config, dnsCache *cache.Cache, dnsServer *dnsserver.Server) *Server {
	return &Server{
		cfg:       cfg,
		dnsCache:  dnsCache,
		dnsServer: dnsServer,
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
	http.HandleFunc("/api/cache/clear", s.handleClearCache)
	http.HandleFunc("/health", s.handleHealth)

	// 注册静态文件服务，用于提供 Web UI
	fs := http.FileServer(http.Dir("web"))
	http.Handle("/", fs)

	s.listener = http.Server{Addr: addr}

	log.Printf("Web API server started on http://localhost:%d\n", s.cfg.WebUI.ListenPort)
	return s.listener.ListenAndServe()
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

	// 从缓存中获取结果
	entry, ok := s.dnsCache.Get(domain, qtype)
	if !ok {
		http.Error(w, "Domain not found in cache", http.StatusNotFound)
		return
	}

	// 构造响应
	var ips []IPResult
	for i, ip := range entry.IPs {
		rtt := 0
		if i < len(entry.RTTs) {
			rtt = entry.RTTs[i]
		}
		ips = append(ips, IPResult{
			IP:  ip,
			RTT: rtt,
		})
	}

	result := QueryResult{
		Domain: domain,
		Type:   queryType,
		IPs:    ips,
		Status: "success",
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
		"strategy":     s.cfg.Upstream.Strategy,
		"timeout_ms":   s.cfg.Upstream.TimeoutMs,
		"concurrency":  s.cfg.Upstream.Concurrency,
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
