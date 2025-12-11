package dnsserver

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"smartdnssort/adblock"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"smartdnssort/prefetch"
	"smartdnssort/stats"
	"smartdnssort/upstream"
	"smartdnssort/upstream/bootstrap"
	"sync"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
)

// Server DNS 服务器
type Server struct {
	mu                 sync.RWMutex
	cfg                *config.Config
	stats              *stats.Stats
	cache              *cache.Cache
	upstream           *upstream.Manager
	pinger             *ping.Pinger
	sortQueue          *cache.SortQueue
	prefetcher         *prefetch.Prefetcher
	refreshQueue       *RefreshQueue
	recentQueries      [20]string // Circular buffer for recent queries
	recentQueriesIndex int
	recentQueriesMu    sync.Mutex
	udpServer          *dns.Server
	tcpServer          *dns.Server
	adblockManager     *adblock.AdBlockManager // 广告拦截管理器
	customRespManager  *CustomResponseManager  // 自定义回复管理器
	requestGroup       singleflight.Group      // 用于合并并发请求
}

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
		upstream:     upstream.NewManager(upstreams, cfg.Upstream.Strategy, cfg.Upstream.TimeoutMs, cfg.Upstream.Concurrency, s, convertHealthCheckConfig(&cfg.Upstream.HealthCheck)),
		pinger:       ping.NewPinger(cfg.Ping.Count, cfg.Ping.TimeoutMs, cfg.Ping.Concurrency, cfg.Ping.MaxTestIPs, cfg.Ping.RttCacheTtlSeconds, cfg.Ping.Strategy),
		sortQueue:    sortQueue,
		refreshQueue: refreshQueue,
	}

	// 尝试加载持久化缓存
	logger.Info("[Cache] Loading cache from disk...")
	if err := server.cache.LoadFromDisk("dns_cache.json"); err != nil {
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

// setupUpstreamCallback 设置上游管理器的缓存更新回调
func (s *Server) setupUpstreamCallback(u *upstream.Manager) {
	u.SetCacheUpdateCallback(func(domain string, qtype uint16, ips []string, cname string, ttl uint32) {
		logger.Debugf("[CacheUpdateCallback] 更新缓存: %s (type=%s), IP数量=%d, CNAME=%s, TTL=%d秒",
			domain, dns.TypeToString[qtype], len(ips), cname, ttl)

		// 获取当前原始缓存中的 IP 数量
		var oldIPCount int
		if oldEntry, exists := s.cache.GetRaw(domain, qtype); exists {
			oldIPCount = len(oldEntry.IPs)
		}

		// 更新原始缓存中的IP列表
		// 注意：这里使用 time.Now() 作为获取时间，因为这是后台收集完成的时间
		s.cache.SetRaw(domain, qtype, ips, cname, ttl)

		// 如果后台收集的 IP 数量比之前多，需要重新排序
		if len(ips) > oldIPCount {
			logger.Debugf("[CacheUpdateCallback] 后台收集到更多IP (%d -> %d)，清除旧排序状态并重新排序",
				oldIPCount, len(ips))

			// 清除旧的排序状态，允许重新排序
			s.cache.CancelSort(domain, qtype)

			// 触发异步排序，更新排序缓存
			go s.sortIPsAsync(domain, qtype, ips, ttl, time.Now())
		} else {
			logger.Debugf("[CacheUpdateCallback] IP数量未增加 (%d)，保持现有排序", len(ips))
		}
	})
}

// GetCustomResponseManager returns the custom response manager instance
func (s *Server) GetCustomResponseManager() *CustomResponseManager {
	return s.customRespManager
}

// Start 启动 DNS 服务器
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.cfg.DNS.ListenPort)

	// 注册 DNS 处理函数
	dns.HandleFunc(".", s.handleQuery)

	// 启动 UDP 服务器
	s.udpServer = &dns.Server{
		Addr:    addr,
		Net:     "udp",
		Handler: dns.DefaultServeMux,
	}

	// 启动 TCP 服务器（如果启用）
	if s.cfg.DNS.EnableTCP {
		s.tcpServer = &dns.Server{
			Addr:    addr,
			Net:     "tcp",
			Handler: dns.DefaultServeMux,
		}

		go func() {
			logger.Infof("TCP DNS server started on %s", addr)
			if err := s.tcpServer.ListenAndServe(); err != nil {
				logger.Errorf("TCP server error: %v", err)
			}
		}()
	}

	// 启动清理过期缓存的 goroutine
	go s.cleanCacheRoutine()

	// 启动定期保存缓存的 goroutine
	go s.saveCacheRoutine()

	// Start the prefetcher
	s.prefetcher.Start()

	logger.Infof("UDP DNS server started on %s", addr)
	return s.udpServer.ListenAndServe()
}

// GetStats 获取统计信息
func (s *Server) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats.GetStats()
}

// ClearStats clears all collected statistics.
func (s *Server) ClearStats() {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Println("Clearing all statistics via API request.")
	s.stats.Reset()
}

// RecordRecentQuery adds a domain to the recent queries list.
func (s *Server) RecordRecentQuery(domain string) {
	s.recentQueriesMu.Lock()
	defer s.recentQueriesMu.Unlock()

	s.recentQueries[s.recentQueriesIndex] = domain
	s.recentQueriesIndex = (s.recentQueriesIndex + 1) % len(s.recentQueries)
}

// GetRecentQueries returns a slice of the most recent queries.
func (s *Server) GetRecentQueries() []string {
	s.recentQueriesMu.Lock()
	defer s.recentQueriesMu.Unlock()

	// The buffer is circular, so we need to reconstruct the order.
	// The oldest element is at `s.recentQueriesIndex`.
	var orderedQueries []string
	for i := 0; i < len(s.recentQueries); i++ {
		idx := (s.recentQueriesIndex + i) % len(s.recentQueries)
		if s.recentQueries[idx] != "" {
			orderedQueries = append(orderedQueries, s.recentQueries[idx])
		}
	}
	// Reverse to get the most recent first
	for i, j := 0, len(orderedQueries)-1; i < j; i, j = i+1, j-1 {
		orderedQueries[i], orderedQueries[j] = orderedQueries[j], orderedQueries[i]
	}

	return orderedQueries
}

// GetCache 获取缓存实例（供 WebAPI 使用）
func (s *Server) GetCache() *cache.Cache {
	return s.cache
}

// GetConfig returns the current server configuration.
func (s *Server) GetConfig() *config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Return a copy to prevent race conditions if the caller modifies it
	cfgCopy := *s.cfg
	return &cfgCopy
}

// ApplyConfig applies a new configuration to the running server (hot-reload).
func (s *Server) ApplyConfig(newCfg *config.Config) error {
	log.Println("Applying new configuration...")

	// Create new components outside the lock to avoid blocking.
	// Create new components outside the lock to avoid blocking.
	var newUpstream *upstream.Manager
	if !reflect.DeepEqual(s.cfg.Upstream, newCfg.Upstream) {
		log.Println("Reloading Upstream client due to configuration changes.")

		// Re-initialize bootstrap resolver
		boot := bootstrap.NewResolver(newCfg.Upstream.BootstrapDNS)

		var upstreams []upstream.Upstream
		for _, serverUrl := range newCfg.Upstream.Servers {
			u, err := upstream.NewUpstream(serverUrl, boot)
			if err != nil {
				log.Printf("Failed to create upstream for %s: %v", serverUrl, err)
				continue
			}
			upstreams = append(upstreams, u)
		}

		newUpstream = upstream.NewManager(upstreams, newCfg.Upstream.Strategy, newCfg.Upstream.TimeoutMs, newCfg.Upstream.Concurrency, s.stats, convertHealthCheckConfig(&newCfg.Upstream.HealthCheck))
		// 设置缓存更新回调
		s.setupUpstreamCallback(newUpstream)
	}

	var newPinger *ping.Pinger
	if !reflect.DeepEqual(s.cfg.Ping, newCfg.Ping) {
		log.Println("Reloading Pinger due to configuration changes.")
		newPinger = ping.NewPinger(newCfg.Ping.Count, newCfg.Ping.TimeoutMs, newCfg.Ping.Concurrency, newCfg.Ping.MaxTestIPs, newCfg.Ping.RttCacheTtlSeconds, newCfg.Ping.Strategy)
	}

	var newSortQueue *cache.SortQueue
	if s.cfg.System.SortQueueWorkers != newCfg.System.SortQueueWorkers {
		logger.Infof("Reloading SortQueue from %d to %d workers.", s.cfg.System.SortQueueWorkers, newCfg.System.SortQueueWorkers)
		newSortQueue = cache.NewSortQueue(newCfg.System.SortQueueWorkers, 200, 10*time.Second)
		newSortQueue.SetSortFunc(func(ctx context.Context, domain string, ips []string) ([]string, []int, error) {
			return s.performPingSort(ctx, domain, ips)
		})
	}

	var newRefreshQueue *RefreshQueue
	if s.cfg.System.RefreshWorkers != newCfg.System.RefreshWorkers {
		logger.Infof("Reloading RefreshQueue from %d to %d workers.", s.cfg.System.RefreshWorkers, newCfg.System.RefreshWorkers)
		newRefreshQueue = NewRefreshQueue(newCfg.System.RefreshWorkers, 100)
		newRefreshQueue.SetWorkFunc(s.refreshCacheAsync)
	}

	var newPrefetcher *prefetch.Prefetcher
	if !reflect.DeepEqual(s.cfg.Prefetch, newCfg.Prefetch) {
		logger.Info("Reloading Prefetcher due to configuration changes.")
		newPrefetcher = prefetch.NewPrefetcher(&newCfg.Prefetch, s.stats, s.cache, s)
	}

	// Now, acquire the lock and swap the components.
	s.mu.Lock()
	defer s.mu.Unlock()

	if newUpstream != nil {
		s.upstream = newUpstream
	}

	if newPinger != nil {
		if s.pinger != nil {
			s.pinger.Stop()
		}
		s.pinger = newPinger
	}

	if newSortQueue != nil {
		s.sortQueue.Stop()
		s.sortQueue = newSortQueue
	}

	if newRefreshQueue != nil {
		s.refreshQueue.Stop()
		s.refreshQueue = newRefreshQueue
	}

	if newPrefetcher != nil {
		s.prefetcher.Stop()
		s.prefetcher = newPrefetcher
		s.prefetcher.Start()
	}

	// Update the config reference
	s.cfg = newCfg

	logger.Info("New configuration applied successfully.")
	return nil
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown() {
	logger.Info("[Server] 开始关闭服务器...")

	if s.udpServer != nil {
		if err := s.udpServer.Shutdown(); err != nil {
			logger.Errorf("[Server] UDP server shutdown error: %v", err)
		}
	}
	if s.tcpServer != nil {
		if err := s.tcpServer.Shutdown(); err != nil {
			logger.Errorf("[Server] TCP server shutdown error: %v", err)
		}
	}

	s.sortQueue.Stop()
	s.prefetcher.Stop()
	s.refreshQueue.Stop()
	logger.Info("[Server] 服务器已关闭")
}

// GetAdBlockManager returns the adblock manager instance.
func (s *Server) GetAdBlockManager() *adblock.AdBlockManager {
	return s.adblockManager
}

// SetAdBlockEnabled dynamically enables or disables AdBlock filtering
func (s *Server) SetAdBlockEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cfg.AdBlock.Enable = enabled
	logger.Infof("[AdBlock] Filtering status changed to: %v", enabled)
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
