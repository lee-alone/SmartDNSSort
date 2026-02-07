package dnsserver

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"smartdnssort/prefetch"
	"smartdnssort/recursor"
	"smartdnssort/upstream"
	"smartdnssort/upstream/bootstrap"
)

// ApplyConfig applies a new configuration to the running server (hot-reload).
func (s *Server) ApplyConfig(newCfg *config.Config) error {
	logger.Info("Applying new configuration...")

	// Create new components outside the lock to avoid blocking.
	var newUpstream *upstream.Manager
	if !reflect.DeepEqual(s.cfg.Upstream, newCfg.Upstream) {
		logger.Info("Reloading Upstream client due to configuration changes.")

		// Re-initialize bootstrap resolver
		boot := bootstrap.NewResolver(newCfg.Upstream.BootstrapDNS)

		var upstreams []upstream.Upstream
		for _, serverUrl := range newCfg.Upstream.Servers {
			u, err := upstream.NewUpstream(serverUrl, boot, &newCfg.Upstream)
			if err != nil {
				logger.Errorf("Failed to create upstream for %s: %v", serverUrl, err)
				continue
			}
			upstreams = append(upstreams, u)
		}

		// 如果新配置启用了 Recursor，将其添加为上游源
		if newCfg.Upstream.EnableRecursor {
			recursorPort := newCfg.Upstream.RecursorPort
			if recursorPort == 0 {
				recursorPort = 5353
			}
			recursorAddr := fmt.Sprintf("tcp://127.0.0.1:%d", recursorPort)
			u, err := upstream.NewUpstream(recursorAddr, boot, &newCfg.Upstream)
			if err != nil {
				logger.Warnf("Failed to create upstream for recursor %s: %v", recursorAddr, err)
			} else {
				upstreams = append(upstreams, u)
				logger.Infof("Added recursor as upstream: %s", recursorAddr)
			}
		}

		newUpstream = upstream.NewManager(&newCfg.Upstream, upstreams, s.stats, &upstream.StatsConfig{
			UpstreamStatsBucketMinutes: newCfg.Stats.UpstreamStatsBucketMinutes,
			UpstreamStatsRetentionDays: newCfg.Stats.UpstreamStatsRetentionDays,
		})
		// 设置缓存更新回调
		s.setupUpstreamCallback(newUpstream)
	}

	var newPinger *ping.Pinger
	if !reflect.DeepEqual(s.cfg.Ping, newCfg.Ping) {
		logger.Info("Reloading Pinger due to configuration changes.")
		newPinger = ping.NewPinger(newCfg.Ping.Count, newCfg.Ping.TimeoutMs, newCfg.Ping.Concurrency, newCfg.Ping.MaxTestIPs, newCfg.Ping.RttCacheTtlSeconds, newCfg.Ping.EnableHttpFallback, "adblock_cache/ip_failure_weights.json")
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

	//处理 Recursor 生命周期 (必须在锁内进行，因为不仅涉及 s.recursorMgr 的替换，
	// 还因为需要先停止旧的才能释放端口给新的使用)
	recursorChanged := s.cfg.Upstream.EnableRecursor != newCfg.Upstream.EnableRecursor ||
		s.cfg.Upstream.RecursorPort != newCfg.Upstream.RecursorPort

	if recursorChanged {
		logger.Info("Recursor configuration changed, updating manager...")

		// 1. 停止现有的 Recursor (如果存在)
		if s.recursorMgr != nil {
			logger.Info("Stopping existing recursor...")
			if err := s.recursorMgr.Stop(); err != nil {
				logger.Warnf("Failed to stop existing recursor: %v", err)
			}
			s.recursorMgr = nil
		}

		// 2. 如果新配置启用，创建并启动新的
		if newCfg.Upstream.EnableRecursor {
			recursorPort := newCfg.Upstream.RecursorPort
			if recursorPort == 0 {
				recursorPort = 5353
			}

			logger.Infof("Initializing new recursor on port %d...", recursorPort)
			newMgr := recursor.NewManager(recursorPort)

			// 尝试启动
			if err := newMgr.Start(); err != nil {
				logger.Errorf("Failed to start new recursor: %v", err)
				// 即使启动失败也保留 manager 引用，以便后续可以查询状态或重试
			} else {
				logger.Infof("New recursor started successfully on port %d", recursorPort)
			}
			s.recursorMgr = newMgr
		}
	}

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
		// Update the cache's reference to the new prefetcher
		s.cache.SetPrefetcher(newPrefetcher)
		s.prefetcher.Start()
	}

	// Handle AdBlock configuration changes
	if !reflect.DeepEqual(s.cfg.AdBlock, newCfg.AdBlock) {
		logger.Info("AdBlock configuration changed, updating manager...")
		if s.adblockManager != nil {
			s.adblockManager.SetEnabled(newCfg.AdBlock.Enable)
		}
	}

	// Update the config reference
	s.cfg = newCfg

	logger.Info("New configuration applied successfully.")
	return nil
}
