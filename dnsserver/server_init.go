package dnsserver

import (
	"context"
	"fmt"
	"time"

	"smartdnssort/adblock"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"smartdnssort/prefetch"
	"smartdnssort/recursor"
	"smartdnssort/stats"
	"smartdnssort/upstream"
	"smartdnssort/upstream/bootstrap"
	"smartdnssort/connectivity"
)

// NewServer 创建新的 DNS 服务器
func NewServer(cfg *config.Config, s *stats.Stats) *Server {
	// 创建异步排序队列
	sortQueue := cache.NewSortQueue(cfg.System.SortQueueWorkers, 200, 10*time.Second)

	// 创建异步刷新队列
	refreshQueue := NewRefreshQueue(cfg.System.RefreshWorkers, 100)

	// Initialize Bootstrap Resolver
	boot := bootstrap.NewResolver(cfg.Upstream.BootstrapDNS)

	// 静默隔离改造：将全局网络健康检查器注入给 bootstrap resolver
	// 这样 bootstrap resolver 就可以在断网时熔断引导解析，避免无效的 DNS 请求
	checker := connectivity.GetGlobalNetworkChecker()
	boot.SetNetworkHealthChecker(checker)
	logger.Info("[Server] Network health checker injected to Bootstrap Resolver for silent isolation.")

	// Initialize Upstream Interfaces
	var upstreams []upstream.Upstream
	for _, serverUrl := range cfg.Upstream.Servers {
		u, err := upstream.NewUpstream(serverUrl, boot, &cfg.Upstream)
		if err != nil {
			logger.Errorf("Failed to create upstream for %s: %v", serverUrl, err)
			continue
		}
		upstreams = append(upstreams, u)
	}

	// 如果启用了 Recursor，将其添加为上游源
	if cfg.Upstream.EnableRecursor {
		recursorAddr := fmt.Sprintf("tcp://127.0.0.1:%d", cfg.Upstream.RecursorPort)
		u, err := upstream.NewUpstream(recursorAddr, boot, &cfg.Upstream)
		if err != nil {
			logger.Warnf("Failed to create upstream for recursor %s: %v", recursorAddr, err)
		} else {
			upstreams = append(upstreams, u)
			logger.Infof("Added recursor as upstream: %s", recursorAddr)
		}
	}

	server := &Server{
		cfg:     cfg,
		stats:   s,
		cache:   cache.NewCache(&cfg.Cache),
		msgPool: cache.NewMsgPool(),
		upstream: upstream.NewManager(&cfg.Upstream, upstreams, s, &upstream.StatsConfig{
			UpstreamStatsBucketMinutes: cfg.Stats.UpstreamStatsBucketMinutes,
			UpstreamStatsRetentionDays: cfg.Stats.UpstreamStatsRetentionDays,
		}),
		pinger:        ping.NewPinger(cfg.Ping.Count, cfg.Ping.TimeoutMs, cfg.Ping.Concurrency, cfg.Ping.MaxTestIPs, cfg.Ping.RttCacheTtlSeconds, cfg.Ping.EnableHttpFallback, "adblock_cache/ip_failure_weights.json"),
		sortQueue:     sortQueue,
		refreshQueue:  refreshQueue,
		stopCh:        make(chan struct{}),
		sortSemaphore: make(chan struct{}, 50), // 限制最多 50 个并发排序任务
	}

	// 静默隔离改造：将全局网络健康检查器注入给 pinger 实例
	// 这样 pinger 就可以在断网时拒绝更新缓存，防止缓存污染
	server.pinger.SetHealthChecker(checker)
	logger.Info("[Server] Network health checker injected to Pinger for silent isolation.")

	// 静默隔离改造：将全局网络健康检查器注入给 server 实例
	// 这样 refresh queue 就可以在断网时跳过背景更新任务，避免无效的队列占用
	server.networkChecker = checker
	logger.Info("[Server] Network health checker injected to Server for silent isolation.")

	// 静默隔离改造：将全局网络健康检查器注入给 stats 实例
	// 这样 stats 就可以在断网时熔断外部行为统计，避免统计污染
	s.SetNetworkChecker(checker)
	logger.Info("[Server] Network health checker injected to Stats for silent isolation.")

	// 尝试加载持久化缓存
	logger.Info("[Cache] Loading cache from disk...")
	if err := server.cache.LoadFromDisk("dns_cache.bin"); err != nil {
		logger.Errorf("[Cache] Failed to load cache: %v", err)
	} else {
		logger.Infof("[Cache] Loaded %d entries from disk.", server.cache.GetCurrentEntries())
	}

	// 初始化 AdBlock 管理器
	logger.Info("[AdBlock] Initializing AdBlock Manager...")
	adblockMgr, err := adblock.NewManager(&cfg.AdBlock, checker)
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

	// 静默隔离改造：将全局网络健康检查器注入给 prefetcher 实例
	// 这样 prefetcher 就可以在断网时跳过预取，避免无效的上游请求
	server.prefetcher.SetNetworkChecker(checker)
	logger.Info("[Server] Network health checker injected to Prefetcher for silent isolation.")

	// 设置 IP 池更新器，用于维护全局 IP 资源
	server.cache.SetIPPoolUpdater(server.pinger.GetIPPool())

	// 初始化 IP 主动巡检调度器
	logger.Info("[IPMonitor] Initializing IP Monitor...")
	monitorConfig := ping.DefaultIPMonitorConfig()
	// 从配置文件读取自定义配置
	if cfg.IPMonitor.Enabled {
		monitorConfig.Enabled = cfg.IPMonitor.Enabled
	}
	if cfg.IPMonitor.T0RefreshInterval > 0 {
		monitorConfig.T0RefreshInterval = cfg.IPMonitor.T0RefreshInterval
	}
	if cfg.IPMonitor.T1RefreshInterval > 0 {
		monitorConfig.T1RefreshInterval = cfg.IPMonitor.T1RefreshInterval
	}
	if cfg.IPMonitor.T2RefreshInterval > 0 {
		monitorConfig.T2RefreshInterval = cfg.IPMonitor.T2RefreshInterval
	}
	if cfg.IPMonitor.CleanupInterval > 0 {
		monitorConfig.CleanupInterval = cfg.IPMonitor.CleanupInterval
	}
	if cfg.IPMonitor.RefCountWeight > 0 {
		monitorConfig.RefCountWeight = cfg.IPMonitor.RefCountWeight
	}
	if cfg.IPMonitor.AccessHeatWeight > 0 {
		monitorConfig.AccessHeatWeight = cfg.IPMonitor.AccessHeatWeight
	}
	if cfg.IPMonitor.MaxRefreshPerCycle > 0 {
		monitorConfig.MaxRefreshPerCycle = cfg.IPMonitor.MaxRefreshPerCycle
	}
	if cfg.IPMonitor.RefreshConcurrency > 0 {
		monitorConfig.RefreshConcurrency = cfg.IPMonitor.RefreshConcurrency
	}
	server.ipMonitor = ping.NewIPMonitor(server.pinger, monitorConfig)
	logger.Info("[IPMonitor] IP Monitor initialized.")

	// 设置排序函数：使用 ping 进行 IP 排序
	sortQueue.SetSortFunc(func(ctx context.Context, domain string, ips []string) ([]string, []int, error) {
		return server.performPingSort(ctx, domain, ips)
	})

	// 设置上游管理器的缓存更新回调
	server.setupUpstreamCallback(server.upstream)

	// 初始化嵌入式递归解析器（如果启用）
	if cfg.Upstream.EnableRecursor {
		recursorPort := cfg.Upstream.RecursorPort
		if recursorPort == 0 {
			recursorPort = 5353
		}
		server.recursorMgr = recursor.NewManager(recursorPort)
		logger.Infof("[Recursor] Manager initialized for port %d", recursorPort)
	}

	return server
}
