package ping

import (
	"time"
)

// startRttCacheCleaner 启动 RTT 缓存清理器
// 定期清理过期的缓存条目
func (p *Pinger) startRttCacheCleaner() {
	ticker := time.NewTicker(time.Duration(p.rttCacheTtlSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.rttCacheMu.Lock()
			for ip, entry := range p.rttCache {
				if time.Now().After(entry.expiresAt) {
					delete(p.rttCache, ip)
				}
			}
			p.rttCacheMu.Unlock()
		case <-p.stopChan:
			return
		}
	}
}
