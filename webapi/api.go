package webapi

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"sync"
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

	// 并发控制
	cfgMutex            sync.RWMutex // 保护配置文件读写
	restartMutex        sync.Mutex   // 保护重启操作
	isRestarting        bool         // 重启进行中标志
	adblockMutex        sync.Mutex   // 保护 AdBlock 更新操作
	isAdblockBusy       bool         // AdBlock 更新进行中标志
	customRulesMutex    sync.RWMutex // 保护 custom_rules.txt 读写
	customResponseMutex sync.RWMutex // 保护 custom_response_rules.txt 读写
	unboundConfigMutex  sync.RWMutex // 保护 Unbound 配置文件读写
}

// NewServer 创建新的 Web API 服务器
func NewServer(cfg *config.Config, dnsCache *cache.Cache, dnsServer *dnsserver.Server, configPath string, restartFunc func()) *Server {
	return &Server{
		cfg:           cfg,
		dnsCache:      dnsCache,
		dnsServer:     dnsServer,
		configPath:    configPath,
		restartFunc:   restartFunc,
		isRestarting:  false,
		isAdblockBusy: false,
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
	mux.HandleFunc("/api/recent-blocked", s.handleRecentlyBlocked)
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

	// Recursor API 路由
	mux.HandleFunc("/api/recursor/status", s.handleRecursorStatus)
	mux.HandleFunc("/api/recursor/install-status", s.handleRecursorInstallStatus)
	mux.HandleFunc("/api/recursor/system-info", s.handleRecursorSystemInfo)
	mux.HandleFunc("/api/unbound/config", s.handleUnboundConfig)

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

// calculateAvgBytesPerEntry 计算缓存条目的平均字节数
// 通过采样缓存中的条目来获得更准确的平均值
func (s *Server) calculateAvgBytesPerEntry() int64 {
	const (
		defaultAvgBytes = 820 // 默认估算值
		sampleSize      = 100 // 采样100个条目
	)

	// 获取所有缓存条目
	allEntries := s.dnsCache.GetRawCacheSnapshot()
	if len(allEntries) == 0 {
		return defaultAvgBytes
	}

	// 如果条目数少于采样大小，直接计算所有条目的平均值
	if len(allEntries) <= sampleSize {
		totalSize := int64(0)
		for _, entry := range allEntries {
			totalSize += estimateEntrySize(entry)
		}
		return totalSize / int64(len(allEntries))
	}

	// 采样计算：每隔 len/sampleSize 个条目采样一个
	step := len(allEntries) / sampleSize
	if step < 1 {
		step = 1
	}

	totalSize := int64(0)
	sampleCount := 0
	for i := 0; i < len(allEntries); i += step {
		totalSize += estimateEntrySize(allEntries[i])
		sampleCount++
	}

	if sampleCount == 0 {
		return defaultAvgBytes
	}

	return totalSize / int64(sampleCount)
}

// estimateEntrySize 估算单个缓存条目的大小（字节）
func estimateEntrySize(entry *cache.RawCacheEntry) int64 {
	if entry == nil {
		return 0
	}

	size := int64(0)

	// RawCacheEntry 结构体本身的大小
	size += 8 + 8 + 8 + 8 + 8 + 8 + 8 + 1 + 8 // 各字段的大小

	// IPs 切片
	for _, ip := range entry.IPs {
		size += int64(len(ip))
	}
	size += int64(len(entry.IPs)) * 24 // 切片头的开销

	// CNAMEs 切片
	for _, cname := range entry.CNAMEs {
		size += int64(len(cname))
	}
	size += int64(len(entry.CNAMEs)) * 24 // 切片头的开销

	// Records 切片（如果有）
	if entry.Records != nil {
		size += int64(len(entry.Records)) * 100 // 粗略估算每个 DNS 记录约 100 字节
	}

	// 其他开销（map 节点、指针等）
	size += 100

	return size
}

// calculateEvictionsPerMinute 计算每分钟的驱逐率
func (s *Server) calculateEvictionsPerMinute() float64 {
	currentEvictions := s.dnsCache.GetEvictions()

	// 通过 dnsServer 的 stats 对象来计算驱逐率
	evictionsPerMin := s.dnsServer.CalculateEvictionsPerMinute(currentEvictions)

	return evictionsPerMin
}
