package dnsserver

import (
	"context"
	"time"

	"smartdnssort/adblock"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"smartdnssort/prefetch"
	"smartdnssort/stats"
	"smartdnssort/upstream"
	"smartdnssort/upstream/bootstrap"
)

// NewServer 创建新的 DNS 服务器
func NewServer(cfg *config.Config, s *stats.Stats) *Server {
	// 创建异步排序队列
	sortQueue := cache.NewSortQueue(cfg.System.SortQueueWorkers, 200, 10*time.Second)

	// 创建异步刷新队列
	refreshQueue := NewRefreshQueue(cfg.System.RefreshWorkers, 100)

	// Initialize Bootstrap Resolver
	boot := bootstrap.NewResolver(cfg.Upstream.BootstrapDNS)

	// Initialize Upstream Interfaces
	var upstreams []upstream.Upstream
	for _, serverUrl := range cfg.Upstream.Servers {
		u, err := upstream.NewUpstream(serverUrl, boot)
		if err != nil {
			logger.Errorf("Failed to create upstream for %s: %v", serverUrl, err)
			continue
		}
		upstreams = append(upstreams, u)
	}

	server := &Server{
		cfg:          cfg,
		stats:        s,
		cache:        cache.NewCache(&cfg.Cache),
		msgPool:      cache.NewMsgPool(),
		upstream:     upstream.NewManager(upstreams, cfg.Upstream.Strategy, cfg.Upstream.TimeoutMs, cfg.Upstream.Concurrency, s, convertHealthCheckConfig(&cfg.Upstream.HealthCheck), cfg.Upstream.RacingDelay, cfg.Upstream.RacingMaxConcurrent),
		pinger:       ping.NewPinger(cfg.Ping.Count, cfg.Ping.TimeoutMs, cfg.Ping.Concurrency, cfg.Ping.MaxTestIPs, cfg.Ping.RttCacheTtlSeconds, cfg.Ping.EnableHttpFallback, "adblock_cache/ip_failure_weights.json"),
		sortQueue:    sortQueue,
		refreshQueue: refreshQueue,
	}

	// 尝试加载持久化缓存
	logger.Info("[Cache] Loading cache from disk...")
	if err := server.cache.LoadFromDisk("dns_cache.bin"); err != nil {
		logger.Errorf("[Cache] Failed to load cache: %v", err)
	} else {
		logger.Infof("[Cache] Loaded %d entries from disk.", server.cache.GetCurrentEntries())
	}

	// 初始化 AdBlock 管理器
	logger.Info("[AdBlock] Initializing AdBlock Manager...")
	adblockMgr, err := adblock.NewManager(&cfg.AdBlock)
	if err != nil {
		logger.Errorf("[AdBlock] Failed to initialize manager: %v", err)
		// If initialization fails, we must ensure it's disabled in config
		cfg.AdBlock.Enable = false
	} else {
		server.adblockManager = adblockMgr
		// Start the adblock manager (downloads rules, etc.)
		go server.adblockManager.Start(context.Background())
		if cfg.AdBlock.Enable {
			logger.Info("[AdBlock] Manager initialized and started (Enabled).")
		} else {
			logger.Info("[AdBlock] Manager initialized and started (Disabled).")
		}
	}

	// Initialize Custom Response Manager
	logger.Info("[Ref] Initializing Custom Response Manager...")
	customRespMgr := NewCustomResponseManager(cfg.AdBlock.CustomResponseFile)
	if err := customRespMgr.Load(); err != nil {
		logger.Errorf("[Ref] Failed to load custom response rules: %v", err)
	} else {
		logger.Info("[Ref] Custom response rules loaded.")
	}
	server.customRespManager = customRespMgr

	// 设置刷新队列的工作函数
	refreshQueue.SetWorkFunc(server.refreshCacheAsync)

	// Create the prefetcher and link it with the cache
	server.prefetcher = prefetch.NewPrefetcher(&cfg.Prefetch, s, server.cache, server)
	server.cache.SetPrefetcher(server.prefetcher)

	// 设置排序函数：使用 ping 进行 IP 排序
	sortQueue.SetSortFunc(func(ctx context.Context, domain string, ips []string) ([]string, []int, error) {
		return server.performPingSort(ctx, domain, ips)
	})

	// 设置上游管理器的缓存更新回调
	server.setupUpstreamCallback(server.upstream)

	return server
}

// convertHealthCheckConfig 将 config.HealthCheckConfig 转换为 upstream.HealthCheckConfig
func convertHealthCheckConfig(cfg *config.HealthCheckConfig) *upstream.HealthCheckConfig {
	if cfg == nil || !cfg.Enabled {
		// 如果未启用健康检查，返回 nil（将使用默认配置）
		return nil
	}

	return &upstream.HealthCheckConfig{
		FailureThreshold:        cfg.FailureThreshold,
		CircuitBreakerThreshold: cfg.CircuitBreakerThreshold,
		CircuitBreakerTimeout:   cfg.CircuitBreakerTimeout,
		SuccessThreshold:        cfg.SuccessThreshold,
	}
}
