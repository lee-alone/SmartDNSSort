package webapi

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/dnsserver"
	"smartdnssort/logger"
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
		logger.Info("WebAPI is disabled")
		return nil
	}

	addr := fmt.Sprintf(":%d", s.cfg.WebUI.ListenPort)

	mux := http.NewServeMux()

	// 基础 API 路由
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

	// AdBlock API 路由
	mux.HandleFunc("/api/adblock/status", s.handleAdBlockStatus)
	mux.HandleFunc("/api/adblock/sources", s.handleAdBlockSources)
	mux.HandleFunc("/api/adblock/update", s.handleAdBlockUpdate)
	mux.HandleFunc("/api/adblock/toggle", s.handleAdBlockToggle)
	mux.HandleFunc("/api/adblock/test", s.handleAdBlockTest)
	mux.HandleFunc("/api/adblock/blockmode", s.handleAdBlockBlockMode)
	mux.HandleFunc("/api/adblock/settings", s.handleAdBlockSettings)

	// 自定义规则 API 路由
	mux.HandleFunc("/api/custom/blocked", s.handleCustomBlocked)
	mux.HandleFunc("/api/custom/response", s.handleCustomResponse)

	// Web 文件服务
	webSubFS, err := fs.Sub(webFilesFS, "web")
	if err == nil {
		logger.Info("Using embedded web files")
		mux.Handle("/", s.corsMiddleware(http.FileServer(http.FS(webSubFS))))
	} else {
		webDir := s.findWebDirectory()
		if webDir == "" {
			logger.Warn("Warning: Could not find web directory. Web UI may not work properly.")
		} else {
			logger.Infof("Using web directory: %s", webDir)
			fsServer := http.FileServer(http.Dir(webDir))
			mux.Handle("/", s.corsMiddleware(fsServer))
		}
	}

	s.listener = http.Server{
		Addr:    addr,
		Handler: mux,
	}

	logger.Infof("Web API server started on http://localhost:%d", s.cfg.WebUI.ListenPort)
	return s.listener.ListenAndServe()
}

// Stop 停止 Web API 服务
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	logger.Info("Shutting down Web API server...")
	return s.listener.Shutdown(ctx)
}
