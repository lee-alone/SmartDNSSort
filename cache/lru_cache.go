package cache

import (
	"container/list"
	"sync"
)

// LRUCache 标准的 LRU 缓存实现
// 使用哈希表 + 双向链表实现，O(1) 时间复杂度的 Get 和 Set 操作
type LRUCache struct {
	mu       sync.RWMutex
	capacity int
	cache    map[string]*list.Element // key -> list.Element
	list     *list.List               // 双向链表，头部为最新，尾部为最旧
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
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Get 获取一个值，并将其移动到链表头部（标记为最新）
func (lru *LRUCache) Get(key string) (any, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	elem, exists := lru.cache[key]
	if !exists {
		return nil, false
	}

	// 将访问的元素移动到链表头部（最新）
	lru.list.MoveToFront(elem)
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
	if lru.list.Len() > lru.capacity {
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
