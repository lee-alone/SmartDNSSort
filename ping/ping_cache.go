package ping

import (
	"time"
)

// startRttCacheCleaner 启动 RTT 缓存清理器
// 定期清理过期的缓存条目
// 使用分片缓存，每个分片独立清理，减少锁竞争
func (p *Pinger) startRttCacheCleaner() {
	ticker := time.NewTicker(time.Duration(p.rttCacheTtlSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 分片缓存的清理操作会自动处理所有分片
			// 每个分片独立加锁，不会相互阻塞
			cleaned := p.rttCache.cleanupExpired()
			// 可选：记录清理统计信息
			_ = cleaned
		case <-p.stopChan:
			return
		}
	}
}
