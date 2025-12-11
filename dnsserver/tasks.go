package dnsserver

import (
	"smartdnssort/logger"
	"time"
)

// cleanCacheRoutine 定期清理过期缓存
// 使用固定的清理间隔,与 min_ttl_seconds 配置无关
func (s *Server) cleanCacheRoutine() {
	// 使用固定的60秒清理间隔
	// 注意：这个间隔与 min_ttl_seconds 是独立的概念
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		s.cache.CleanExpired()
	}
}

// saveCacheRoutine 定期保存缓存到磁盘
func (s *Server) saveCacheRoutine() {
	interval := time.Duration(s.cfg.Cache.SaveToDiskIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 60 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		logger.Info("[Cache] Saving cache to disk...")
		if err := s.cache.SaveToDisk("dns_cache.json"); err != nil {
			logger.Errorf("[Cache] Failed to save cache: %v", err)
		} else {
			logger.Info("[Cache] Cache saved successfully.")
		}
	}
}
