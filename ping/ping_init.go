package ping

import (
	"sync"
)

// NewPinger 创建新的 Pinger 实例
// 参数：
//   - count: 每个 IP 的测试次数
//   - timeoutMs: 单次测试超时时间（毫秒）
//   - concurrency: 并发测试的 IP 数量
//   - maxTestIPs: 最多测试的 IP 数量（0 表示测试所有）
//   - rttCacheTtlSeconds: RTT 缓存过期时间（秒）
//   - enableHttpFallback: 是否启用 HTTP 备选探测
//   - failureWeightPersistFile: IP失效权重持久化文件路径（空字符串表示不持久化）
func NewPinger(count, timeoutMs, concurrency, maxTestIPs, rttCacheTtlSeconds int, enableHttpFallback bool, failureWeightPersistFile string) *Pinger {
	if count <= 0 {
		count = 3
	}
	if timeoutMs <= 0 {
		timeoutMs = 800
	}
	if concurrency <= 0 {
		concurrency = 8
	}

	p := &Pinger{
		count:              count,
		timeoutMs:          timeoutMs,
		concurrency:        concurrency,
		maxTestIPs:         maxTestIPs,
		rttCacheTtlSeconds: rttCacheTtlSeconds,
		enableHttpFallback: enableHttpFallback,
		rttCache:           make(map[string]*rttCacheEntry),
		stopChan:           make(chan struct{}),
		bufferPool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 512)
			},
		},
		failureWeightMgr: NewIPFailureWeightManager(failureWeightPersistFile),
	}

	if rttCacheTtlSeconds > 0 {
		go p.startRttCacheCleaner()
	}
	return p
}
