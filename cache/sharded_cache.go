package cache

import (
	"hash/fnv"
	"sync"
	"sync/atomic"
)

// ShardedCache 分片缓存，通过将缓存分成多个独立的分片来降低锁竞争
// 每个分片有独立的锁，不同的 key 可以并发访问不同的分片
type ShardedCache struct {
	shards []*CacheShard
	mask   uint32 // 用于快速计算分片索引
	dirty  uint64 // 变更计数器，用于持久化决策
}

// CacheShard 单个缓存分片
type CacheShard struct {
	mu       sync.RWMutex
	capacity int
	cache    map[string]*CacheNode
	list     *CacheList

	// 异步访问记录机制（每个分片独立）
	accessChan chan string
	stopChan   chan struct{}
	wg         sync.WaitGroup
}

// CacheNode 缓存节点
type CacheNode struct {
	key   string
	value any
	prev  *CacheNode
	next  *CacheNode
}

// CacheList 双向链表实现（避免使用 container/list 以获得更好的性能）
type CacheList struct {
	head *CacheNode
	tail *CacheNode
	len  int
}

// NewShardedCache 创建分片缓存
// shardCount 应该是 2 的幂次方（如 32, 64）以获得最佳性能
func NewShardedCache(totalCapacity int, shardCount int) *ShardedCache {
	// 确保 shardCount 是 2 的幂次方
	if shardCount <= 0 || (shardCount&(shardCount-1)) != 0 {
		shardCount = 32 // 默认 32 个分片
	}

	shards := make([]*CacheShard, shardCount)
	shardCapacity := (totalCapacity + shardCount - 1) / shardCount // 向上取整

	// 根据分片容量动态计算 accessChan 缓冲区大小
	// 规则：缓冲区大小 = 分片容量 / 10，最小 100，最大 1000
	// 这样在高并发下能更好地缓冲访问记录，减少丢弃
	accessChanCapacity := shardCapacity / 10
	if accessChanCapacity < 100 {
		accessChanCapacity = 100
	}
	if accessChanCapacity > 1000 {
		accessChanCapacity = 1000
	}

	for i := 0; i < shardCount; i++ {
		shard := &CacheShard{
			capacity:   shardCapacity,
			cache:      make(map[string]*CacheNode),
			list:       &CacheList{},
			accessChan: make(chan string, accessChanCapacity), // 动态计算缓冲区
			stopChan:   make(chan struct{}),
		}
		// 启动异步访问记录处理 goroutine
		shard.wg.Add(1)
		go shard.processAccessRecords()
		shards[i] = shard
	}

	return &ShardedCache{
		shards: shards,
		mask:   uint32(shardCount - 1),
	}
}

// getShard 根据 key 获取对应的分片
func (sc *ShardedCache) getShard(key string) *CacheShard {
	hash := fnv.New32a()
	hash.Write([]byte(key))
	index := hash.Sum32() & sc.mask
	return sc.shards[index]
}

// Get 获取值，并将其标记为最近使用
func (sc *ShardedCache) Get(key string) (any, bool) {
	shard := sc.getShard(key)
	shard.mu.RLock()
	node, exists := shard.cache[key]
	if !exists {
		shard.mu.RUnlock()
		return nil, false
	}
	value := node.value
	shard.mu.RUnlock()

	// 异步更新访问顺序，不阻塞读操作
	if exists {
		shard.recordAccess(key)
	}

	return value, true
}

// GetDirtyCount 获取缓存的变更计数
func (sc *ShardedCache) GetDirtyCount() uint64 {
	return atomic.LoadUint64(&sc.dirty)
}

// markDirty 增加变更计数
func (sc *ShardedCache) markDirty() {
	atomic.AddUint64(&sc.dirty, 1)
}

// Set 设置值
func (sc *ShardedCache) Set(key string, value any) {
	shard := sc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// 如果 key 已存在，更新值并移到头部
	if node, exists := shard.cache[key]; exists {
		node.value = value
		shard.list.moveToFront(node)
		return
	}

	// 创建新节点
	node := &CacheNode{key: key, value: value}
	shard.list.pushFront(node)
	shard.cache[key] = node

	// 如果超过容量，删除尾部元素
	if shard.list.len > shard.capacity {
		shard.evictOne()
	}
	sc.markDirty()
}

// evictOne 删除链表尾部的元素（在持有写锁的情况下调用）
func (shard *CacheShard) evictOne() {
	if shard.list.tail == nil {
		return
	}
	node := shard.list.tail
	shard.list.remove(node)
	delete(shard.cache, node.key)
}

// Delete 删除一个键
func (sc *ShardedCache) Delete(key string) {
	shard := sc.getShard(key)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if node, exists := shard.cache[key]; exists {
		shard.list.remove(node)
		delete(shard.cache, key)
		sc.markDirty()
	}
}

// Len 返回缓存中的元素个数
func (sc *ShardedCache) Len() int {
	total := 0
	for _, shard := range sc.shards {
		shard.mu.RLock()
		total += shard.list.len
		shard.mu.RUnlock()
	}
	return total
}

// Clear 清空所有分片
func (sc *ShardedCache) Clear() {
	for _, shard := range sc.shards {
		shard.mu.Lock()
		shard.cache = make(map[string]*CacheNode)
		shard.list = &CacheList{}
		shard.mu.Unlock()
	}
	sc.markDirty()
}

// Close 关闭所有分片的异步处理
func (sc *ShardedCache) Close() error {
	for _, shard := range sc.shards {
		close(shard.stopChan)
		shard.wg.Wait()
	}
	return nil
}

// GetAllEntries 获取所有缓存条目的快照（用于遍历）
func (sc *ShardedCache) GetAllEntries() []any {
	var entries []any
	for _, shard := range sc.shards {
		shard.mu.RLock()
		for elem := shard.list.head; elem != nil; elem = elem.next {
			entries = append(entries, elem.value)
		}
		shard.mu.RUnlock()
	}
	return entries
}

// GetAllKeys 获取所有缓存键的快照（用于遍历）
func (sc *ShardedCache) GetAllKeys() []string {
	var keys []string
	for _, shard := range sc.shards {
		shard.mu.RLock()
		for elem := shard.list.head; elem != nil; elem = elem.next {
			keys = append(keys, elem.key)
		}
		shard.mu.RUnlock()
	}
	return keys
}

// SnapshotEntry 快照条目
type SnapshotEntry struct {
	Key   string
	Value any
}

// GetSnapshot 获取所有缓存条目的一次性一致性快照（按分片锁定）
func (sc *ShardedCache) GetSnapshot() []SnapshotEntry {
	var results []SnapshotEntry
	for _, shard := range sc.shards {
		shard.mu.RLock()
		for elem := shard.list.head; elem != nil; elem = elem.next {
			results = append(results, SnapshotEntry{
				Key:   elem.key,
				Value: elem.value,
			})
		}
		shard.mu.RUnlock()
	}
	return results
}

// CacheShard 方法

// processAccessRecords 异步处理访问记录，批量更新 LRU 链表
func (shard *CacheShard) processAccessRecords() {
	defer shard.wg.Done()

	for {
		select {
		case key := <-shard.accessChan:
			shard.mu.Lock()
			if node, exists := shard.cache[key]; exists {
				shard.list.moveToFront(node)
			}
			shard.mu.Unlock()

		case <-shard.stopChan:
			// 处理剩余的访问记录
			for {
				select {
				case key := <-shard.accessChan:
					shard.mu.Lock()
					if node, exists := shard.cache[key]; exists {
						shard.list.moveToFront(node)
					}
					shard.mu.Unlock()
				default:
					return
				}
			}
		}
	}
}

// recordAccess 记录访问，异步更新链表
func (shard *CacheShard) recordAccess(key string) {
	select {
	case shard.accessChan <- key:
	default:
		// channel 满，丢弃此次记录（可接受，因为大多数访问会被记录）
	}
}

// CacheList 方法

// pushFront 将节点添加到链表头部
func (cl *CacheList) pushFront(node *CacheNode) {
	node.prev = nil
	node.next = cl.head
	if cl.head != nil {
		cl.head.prev = node
	}
	cl.head = node
	if cl.tail == nil {
		cl.tail = node
	}
	cl.len++
}

// moveToFront 将节点移动到链表头部
func (cl *CacheList) moveToFront(node *CacheNode) {
	if node == cl.head {
		return
	}
	cl.remove(node)
	cl.pushFront(node)
}

// remove 从链表中移除节点
func (cl *CacheList) remove(node *CacheNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		cl.head = node.next
	}

	if node.next != nil {
		node.next.prev = node.prev
	} else {
		cl.tail = node.prev
	}

	cl.len--
}
