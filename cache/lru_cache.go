package cache

import (
	"container/list"
	"sync"
	"sync/atomic"
)

// LRUCache 读友好的 LRU 缓存实现
// 使用哈希表 + 双向链表实现，O(1) 时间复杂度的 Get 和 Set 操作
// 改进：Get 操作使用 RLock，访问顺序更新通过异步机制处理
type LRUCache struct {
	mu       sync.RWMutex
	capacity int
	cache    map[string]*list.Element // key -> *list.Element
	list     *list.List               // 双向链表，头部为最新，尾部为最旧

	// 异步访问记录机制
	accessChan chan string // 接收访问记录的 channel
	stopChan   chan struct{}
	wg         sync.WaitGroup
	pending    int32 // 待处理的访问记录数
}

// lruNode 链表中的节点
type lruNode struct {
	key   string
	value any
}

// NewLRUCache 创建一个容量限制的 LRU 缓存
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 10000 // 默认容量
	}

	lru := &LRUCache{
		capacity:   capacity,
		cache:      make(map[string]*list.Element),
		list:       list.New(),
		accessChan: make(chan string, 1000), // 缓冲 channel，避免阻塞
		stopChan:   make(chan struct{}),
	}

	// 启动异步访问记录处理 goroutine
	lru.wg.Add(1)
	go lru.processAccessRecords()

	return lru
}

// processAccessRecords 异步处理访问记录，批量更新 LRU 链表
func (lru *LRUCache) processAccessRecords() {
	defer lru.wg.Done()

	for {
		select {
		case key := <-lru.accessChan:
			atomic.AddInt32(&lru.pending, -1)
			lru.mu.Lock()
			if elem, exists := lru.cache[key]; exists {
				lru.list.MoveToFront(elem)
			}
			lru.mu.Unlock()

		case <-lru.stopChan:
			// 处理剩余的访问记录
			for {
				select {
				case key := <-lru.accessChan:
					atomic.AddInt32(&lru.pending, -1)
					lru.mu.Lock()
					if elem, exists := lru.cache[key]; exists {
						lru.list.MoveToFront(elem)
					}
					lru.mu.Unlock()
				default:
					return
				}
			}
		}
	}
}

// Get 获取一个值，使用读锁，访问顺序更新异步处理
func (lru *LRUCache) Get(key string) (any, bool) {
	lru.mu.RLock()
	elem, exists := lru.cache[key]
	if !exists {
		lru.mu.RUnlock()
		return nil, false
	}
	value := elem.Value.(*lruNode).value
	lru.mu.RUnlock()

	// 异步记录访问，不阻塞读操作
	if exists {
		atomic.AddInt32(&lru.pending, 1)
		select {
		case lru.accessChan <- key:
		default:
			// channel 满，丢弃此次记录（可选：使用非阻塞发送）
			atomic.AddInt32(&lru.pending, -1)
		}
	}

	return value, true
}

// GetNoUpdate 获取一个值，但不更新 LRU 访问顺序
func (lru *LRUCache) GetNoUpdate(key string) (any, bool) {
	lru.mu.RLock()
	defer lru.mu.RUnlock()
	elem, exists := lru.cache[key]
	if !exists {
		return nil, false
	}
	return elem.Value.(*lruNode).value, true
}

// Set 添加或更新一个值
// 新条目添加到链表头部，如果超过容量则删除尾部元素（最久未使用）
func (lru *LRUCache) Set(key string, value any) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	// 如果 key 已存在，更新值并移到头部
	if elem, exists := lru.cache[key]; exists {
		elem.Value.(*lruNode).value = value
		lru.list.MoveToFront(elem)
		return
	}

	// 创建新节点
	node := &lruNode{key: key, value: value}
	elem := lru.list.PushFront(node)
	lru.cache[key] = elem

	// 如果超过容量，删除尾部元素（最久未使用）
	if lru.capacity > 0 && lru.list.Len() > lru.capacity {
		lru.evictOne()
	}
}

// evictOne 删除链表尾部的元素（最久未使用）
func (lru *LRUCache) evictOne() {
	elem := lru.list.Back()
	if elem != nil {
		lru.list.Remove(elem)
		key := elem.Value.(*lruNode).key
		delete(lru.cache, key)
	}
}

// Len 返回当前缓存中的元素个数
func (lru *LRUCache) Len() int {
	lru.mu.RLock()
	defer lru.mu.RUnlock()
	return lru.list.Len()
}

// Delete 从缓存中删除一个键
func (lru *LRUCache) Delete(key string) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if elem, exists := lru.cache[key]; exists {
		lru.list.Remove(elem)
		delete(lru.cache, key)
	}
}

// Clear 清空缓存
func (lru *LRUCache) Clear() {
	lru.mu.Lock()
	defer lru.mu.Unlock()
	lru.cache = make(map[string]*list.Element)
	lru.list = list.New()
}

// Close 关闭缓存，停止异步处理 goroutine
func (lru *LRUCache) Close() error {
	close(lru.stopChan)
	lru.wg.Wait()
	return nil
}

// GetPendingAccess 获取待处理的访问记录数（用于监控）
func (lru *LRUCache) GetPendingAccess() int32 {
	return atomic.LoadInt32(&lru.pending)
}

// CleanExpired 清理过期的条目，调用方需要提供过期检查函数
// 这个方法安全地遍历链表，收集过期条目，然后删除
// 避免在清理过程中持有锁过长时间
func (lru *LRUCache) CleanExpired(isExpired func(value any) bool) int {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	elemsToRemove := make([]*list.Element, 0)
	for elem := lru.list.Front(); elem != nil; elem = elem.Next() {
		if node, ok := elem.Value.(*lruNode); ok {
			if isExpired(node.value) {
				elemsToRemove = append(elemsToRemove, elem)
			}
		}
	}

	count := 0
	for _, elem := range elemsToRemove {
		lru.list.Remove(elem)
		key := elem.Value.(*lruNode).key
		delete(lru.cache, key)
		count++
	}
	return count
}
