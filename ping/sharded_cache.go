package ping

import (
	"sync"
	"time"
)

// shardedRttCache 分片 RTT 缓存
// 将缓存分成多个分片，每个分片有独立的锁，减少锁竞争
type shardedRttCache struct {
	shards    []*rttCacheShard
	shardMask uint32 // 用于快速计算分片索引
}

// rttCacheShard 单个缓存分片
type rttCacheShard struct {
	mu    sync.RWMutex
	cache map[string]*rttCacheEntry
}

// newShardedRttCache 创建分片缓存
// shardCount 应该是 2 的幂次方（如 16, 32, 64）以便快速计算分片索引
func newShardedRttCache(shardCount int) *shardedRttCache {
	// 确保 shardCount 是 2 的幂次方
	if shardCount <= 0 {
		shardCount = 16
	}
	if shardCount&(shardCount-1) != 0 {
		// 不是 2 的幂次方，向上取整到最近的 2 的幂次方
		shardCount = 1
		for shardCount < 1024 && shardCount < shardCount*2 {
			shardCount *= 2
		}
	}

	sc := &shardedRttCache{
		shards:    make([]*rttCacheShard, shardCount),
		shardMask: uint32(shardCount - 1),
	}

	for i := 0; i < shardCount; i++ {
		sc.shards[i] = &rttCacheShard{
			cache: make(map[string]*rttCacheEntry),
		}
	}

	return sc
}

// getShardIndex 根据 IP 地址计算分片索引
// 使用零分配的内联 FNV-1a 哈希算法，避免 hash.Hash 接口对象分配
// 这在高频 IP 哈希计算场景下能显著提升性能
func (sc *shardedRttCache) getShardIndex(ip string) uint32 {
	// FNV-1a 哈希常数
	const (
		fnvOffset32 = uint32(2166136261)
		fnvPrime32  = uint32(16777619)
	)

	hash := fnvOffset32
	for i := 0; i < len(ip); i++ {
		hash ^= uint32(ip[i])
		hash *= fnvPrime32
	}
	return hash & sc.shardMask
}

// get 从缓存中获取条目
func (sc *shardedRttCache) get(ip string) (*rttCacheEntry, bool) {
	shard := sc.shards[sc.getShardIndex(ip)]
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	entry, ok := shard.cache[ip]
	return entry, ok
}

// set 将条目写入缓存
func (sc *shardedRttCache) set(ip string, entry *rttCacheEntry) {
	shard := sc.shards[sc.getShardIndex(ip)]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	shard.cache[ip] = entry
}

// delete 从缓存中删除条目
func (sc *shardedRttCache) delete(ip string) {
	shard := sc.shards[sc.getShardIndex(ip)]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	delete(shard.cache, ip)
}

// cleanupExpired 清理所有过期条目
// 返回清理的条目数
func (sc *shardedRttCache) cleanupExpired() int {
	now := time.Now()
	cleaned := 0

	// 遍历所有分片，并行清理
	// 每个分片独立清理，不会相互阻塞
	for _, shard := range sc.shards {
		shard.mu.Lock()
		for ip, entry := range shard.cache {
			if now.After(entry.expiresAt) {
				delete(shard.cache, ip)
				cleaned++
			}
		}
		shard.mu.Unlock()
	}

	return cleaned
}

// len 返回缓存中的条目总数
func (sc *shardedRttCache) len() int {
	total := 0
	for _, shard := range sc.shards {
		shard.mu.RLock()
		total += len(shard.cache)
		shard.mu.RUnlock()
	}
	return total
}

// clear 清空所有缓存
func (sc *shardedRttCache) clear() {
	for _, shard := range sc.shards {
		shard.mu.Lock()
		shard.cache = make(map[string]*rttCacheEntry)
		shard.mu.Unlock()
	}
}

// getAllEntries 获取所有缓存条目（用于调试和统计）
// 注意：这个操作会遍历所有分片，可能比较耗时
func (sc *shardedRttCache) getAllEntries() map[string]*rttCacheEntry {
	result := make(map[string]*rttCacheEntry)

	for _, shard := range sc.shards {
		shard.mu.RLock()
		for ip, entry := range shard.cache {
			// 复制条目，避免外部修改
			copy := *entry
			result[ip] = &copy
		}
		shard.mu.RUnlock()
	}

	return result
}
