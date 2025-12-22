package dnsserver

import (
	"context"
	"reflect"
	"time"

	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/logger"
	"smartdnssort/ping"
	"smartdnssort/prefetch"
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
			u, err := upstream.NewUpstream(serverUrl, boot)
			if err != nil {
				logger.Errorf("Failed to create upstream for %s: %v", serverUrl, err)
				continue
			}
			upstreams = append(upstreams, u)
		}

		newUpstream = upstream.NewManager(upstreams, newCfg.Upstream.Strategy, newCfg.Upstream.TimeoutMs, newCfg.Upstream.Concurrency, s.stats, convertHealthCheckConfig(&newCfg.Upstream.HealthCheck), newCfg.Upstream.RacingDelay, newCfg.Upstream.RacingMaxConcurrent)
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
