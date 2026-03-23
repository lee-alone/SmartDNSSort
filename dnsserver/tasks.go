package dnsserver

import (
	"smartdnssort/logger"
	"time"
)

// cleanCacheRoutine 定期清理过期缓存
// 使用固定的清理间隔,与 min_ttl_seconds 配置无关
func (s *Server) cleanCacheRoutine() {
	ticker := time.NewTicker(DefaultCacheCleanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cache.CleanExpired()
		}
	}
}

// startIPMonitorRoutine 启动全局 IP 监控器
// 实现主动巡检调度器，根据权重优先级调度 IP 测速
func (s *Server) startIPMonitorRoutine() {
	if s.ipMonitor == nil {
		logger.Warn("[IPMonitor] IP Monitor is not initialized, skipping...")
		return
	}

	logger.Debug("[IPMonitor] Starting IP Monitor routine...")
	s.ipMonitor.Start()
}

// saveCacheRoutine 定期保存缓存到磁盘
func (s *Server) saveCacheRoutine() {
	interval := time.Duration(s.cfg.Cache.SaveToDiskIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = DefaultCacheSaveInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			logger.Debug("[Cache] Saving cache to disk...")
			if err := s.cache.SaveToDisk("dns_cache.bin"); err != nil {
				logger.Errorf("[Cache] Failed to save cache: %v", err)
			} else {
				logger.Debug("[Cache] Cache saved successfully.")
			}
		}
	}
}
