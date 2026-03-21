package cache

import (
	"container/list"
	"hash/fnv"
	"sync"
	"sync/atomic"
)

// ShardedLRUCache 分片 LRU 缓存，结合了 ShardedCache 和 LRUCache 的优点
// 使用分片锁降低竞争，同时保持 LRU 驱逐策略
type ShardedLRUCache struct {
	shards   []*ShardedLRUShard
	mask     uint32
	onEvict  func(key string, value any)
	dirty    uint64
}

// ShardedLRUShard 单个分片
type ShardedLRUShard struct {
	mu       sync.RWMutex
	capacity int
	cache    map[string]*list.Element
	list     *list.List

	// 异步访问记录
	accessChan chan string
	stopChan   chan struct{}
	wg         sync.WaitGroup
	pending    int32
}

// shardedLRUNode 链表节点
type shardedLRUNode struct {
	key   string
	value any
}

// NewShardedLRUCache 创建分片 LRU 缓存
func NewShardedLRUCache(totalCapacity int, shardCount int) *ShardedLRUCache {
	if shardCount <= 0 || (shardCount&(shardCount-1)) != 0 {
		shardCount = 32
	}

	shards := make([]*ShardedLRUShard, shardCount)
	shardCapacity := (totalCapacity + shardCount - 1) / shardCount

	// 动态计算 channel 缓冲区大小
	accessChanCapacity := shardCapacity / 10
	if accessChanCapacity < 100 {
		accessChanCapacity = 100
	}
	if accessChanCapacity > 1000 {
		accessChanCapacity = 1000
	}

	for i := 0; i < shardCount; i++ {
		shard := &ShardedLRUShard{
			capacity:   shardCapacity,
			cache:      make(map[string]*list.Element),
			list:       list.New(),
			accessChan: make(chan string, accessChanCapacity),
			stopChan:   make(chan struct{}),
		}
		shard.wg.Add(1)
		go shard.processAccessRecords()
		shards[i] = shard
	}

	return &ShardedLRUCache{
		shards: shards,
		mask:   uint32(shardCount - 1),
	}
}

// getShard 根据 key 获取分片
func (slru *ShardedLRUCache) getShard(key string) *ShardedLRUShard {
	hash := fnv.New32a()
	hash.Write([]byte(key))
	index := hash.Sum32() & slru.mask
	return slru.shards[index]
}

// Get 获取值并更新 LRU 顺序
func (slru *ShardedLRUCache) Get(key string) (any, bool) {
	shard := slru.getShard(key)
	shard.mu.RLock()
	elem, exists := shard.cache[key]
	if !exists {
		shard.mu.RUnlock()
		return nil, false
	}
	value := elem.Value.(*shardedLRUNode).value
	shard.mu.RUnlock()

	// 异步更新访问顺序
	if exists {
		shard.recordAccess(key)
	}

	return value, true
}

// GetNoUpdate 获取值但不更新 LRU 顺序
func (slru *ShardedLRUCache) GetNoUpdate(key string) (any, bool) {
	shard := slru.getShard(key)
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	elem, exists := shard.cache[key]
	if !exists {
		return nil, false
	}
	return elem.Value.(*shardedLRUNode).value, true
}

// Set 设置值
func (slru *ShardedLRUCache) Set(key string, value any) {
	shard := slru.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if elem, exists := shard.cache[key]; exists {
		elem.Value.(*shardedLRUNode).value = value
		shard.list.MoveToFront(elem)
		return
	}

	node := &shardedLRUNode{key: key, value: value}
	elem := shard.list.PushFront(node)
	shard.cache[key] = elem

	if shard.capacity > 0 && shard.list.Len() > shard.capacity {
		shard.evictOne(slru.onEvict)
	}
	atomic.AddUint64(&slru.dirty, 1)
}

// Delete 删除键
func (slru *ShardedLRUCache) Delete(key string) {
	shard := slru.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if elem, exists := shard.cache[key]; exists {
		shard.list.Remove(elem)
		delete(shard.cache, key)
		atomic.AddUint64(&slru.dirty, 1)
	}
}

// Len 返回总元素数
func (slru *ShardedLRUCache) Len() int {
	total := 0
	for _, shard := range slru.shards {
		shard.mu.RLock()
		total += shard.list.Len()
		shard.mu.RUnlock()
	}
	return total
}

// Clear 清空所有分片
func (slru *ShardedLRUCache) Clear() {
	for _, shard := range slru.shards {
		shard.mu.Lock()
		shard.cache = make(map[string]*list.Element)
		shard.list = list.New()
		shard.mu.Unlock()
	}
	atomic.AddUint64(&slru.dirty, 1)
}

// Close 关闭所有分片的异步处理
func (slru *ShardedLRUCache) Close() error {
	for _, shard := range slru.shards {
		close(shard.stopChan)
		shard.wg.Wait()
	}
	return nil
}

// SetOnEvict 设置驱逐回调
func (slru *ShardedLRUCache) SetOnEvict(callback func(key string, value any)) {
	slru.onEvict = callback
}

// GetDirtyCount 获取变更计数
func (slru *ShardedLRUCache) GetDirtyCount() uint64 {
	return atomic.LoadUint64(&slru.dirty)
}

// CleanExpired 清理过期条目
func (slru *ShardedLRUCache) CleanExpired(isExpired func(value any) bool) int {
	totalCount := 0
	for _, shard := range slru.shards {
		shard.mu.Lock()
		elemsToRemove := make([]*list.Element, 0)
		for elem := shard.list.Front(); elem != nil; elem = elem.Next() {
			if node, ok := elem.Value.(*shardedLRUNode); ok {
				if isExpired(node.value) {
					elemsToRemove = append(elemsToRemove, elem)
				}
			}
		}

		count := 0
		for _, elem := range elemsToRemove {
			shard.list.Remove(elem)
			key := elem.Value.(*shardedLRUNode).key
			delete(shard.cache, key)
			count++
		}
		totalCount += count
		shard.mu.Unlock()
	}
	return totalCount
}

// GetAllEntries 获取所有条目快照
func (slru *ShardedLRUCache) GetAllEntries() []any {
	var entries []any
	for _, shard := range slru.shards {
		shard.mu.RLock()
		for elem := shard.list.Front(); elem != nil; elem = elem.Next() {
			entries = append(entries, elem.Value.(*shardedLRUNode).value)
		}
		shard.mu.RUnlock()
	}
	return entries
}

// GetAllKeys 获取所有键快照
func (slru *ShardedLRUCache) GetAllKeys() []string {
	var keys []string
	for _, shard := range slru.shards {
		shard.mu.RLock()
		for elem := shard.list.Front(); elem != nil; elem = elem.Next() {
			keys = append(keys, elem.Value.(*shardedLRUNode).key)
		}
		shard.mu.RUnlock()
	}
	return keys
}

// ShardedLRUShard 方法

// evictOne 驱逐尾部元素（最久未使用）
func (shard *ShardedLRUShard) evictOne(onEvict func(key string, value any)) {
	elem := shard.list.Back()
	if elem == nil {
		return
	}
	shard.list.Remove(elem)
	key := elem.Value.(*shardedLRUNode).key
	value := elem.Value.(*shardedLRUNode).value
	delete(shard.cache, key)

	if onEvict != nil {
		onEvict(key, value)
	}
}

// processAccessRecords 异步处理访问记录
func (shard *ShardedLRUShard) processAccessRecords() {
	defer shard.wg.Done()

	for {
		select {
		case key := <-shard.accessChan:
			atomic.AddInt32(&shard.pending, -1)
			shard.mu.Lock()
			if elem, exists := shard.cache[key]; exists {
				shard.list.MoveToFront(elem)
			}
			shard.mu.Unlock()

		case <-shard.stopChan:
			for {
				select {
				case key := <-shard.accessChan:
					atomic.AddInt32(&shard.pending, -1)
					shard.mu.Lock()
					if elem, exists := shard.cache[key]; exists {
						shard.list.MoveToFront(elem)
					}
					shard.mu.Unlock()
				default:
					return
				}
			}
		}
	}
}

// recordAccess 记录访问
func (shard *ShardedLRUShard) recordAccess(key string) {
	atomic.AddInt32(&shard.pending, 1)
	select {
	case shard.accessChan <- key:
	default:
		atomic.AddInt32(&shard.pending, -1)
	}
}

// GetPendingAccess 获取待处理访问数
func (shard *ShardedLRUShard) GetPendingAccess() int32 {
	return atomic.LoadInt32(&shard.pending)
}
