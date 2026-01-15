# 缓存系统并发性能优化指南

## 问题分析

### 原始实现的瓶颈

1. **LRU Get 使用写锁**
   - 每次查询都需要修改链表顺序（MoveToFront）
   - 使用 `mu.Lock()` 而非 `RLock()`，导致读操作相互阻塞
   - 在高 QPS（>5000）下成为严重瓶颈

2. **全局锁竞争**
   - Cache 结构体有全局 RWMutex
   - 多个 LRU 缓存共享同一把锁
   - 排序状态管理（sortingState）也由全局锁保护

3. **嵌套锁风险**
   - Cache.mu → LRUCache.mu 的嵌套调用
   - 增加死锁风险和竞争

## 优化方案

### 方案 1：读友好 LRU（已实现）

**核心思想**：将访问顺序更新异步化

```go
// 改进前：Get 使用写锁
func (lru *LRUCache) Get(key string) (any, bool) {
    lru.mu.Lock()  // 写锁，阻塞其他读
    defer lru.mu.Unlock()
    // ... 修改链表 ...
}

// 改进后：Get 使用读锁
func (lru *LRUCache) Get(key string) (any, bool) {
    lru.mu.RLock()  // 读锁，允许并发读
    defer lru.mu.RUnlock()
    // ... 只读取值 ...
    
    // 异步更新访问顺序
    lru.accessChan <- key
}
```

**优势**：
- Get 操作使用 RLock，允许高并发读
- 访问顺序更新通过后台 goroutine 异步处理
- 不影响读操作的响应时间

**权衡**：
- LRU 顺序更新有轻微延迟（通常 <1ms）
- 在极端情况下可能驱逐不是最久未使用的项
- 但在实际 DNS 缓存场景中，这种延迟可以接受

### 方案 2：分片缓存（已实现）

**核心思想**：将单个大缓存分成多个独立分片

```
原始设计：
┌─────────────────────────────┐
│   Cache (单个全局锁)         │
│  ┌─────────────────────────┐│
│  │   LRUCache              ││
│  │  (所有 key 共享一把锁)   ││
│  └─────────────────────────┘│
└─────────────────────────────┘

分片设计（64 个分片）：
┌──────────────────────────────────────────┐
│   ShardedCache                           │
│  ┌────────┐ ┌────────┐ ... ┌────────┐   │
│  │Shard 0 │ │Shard 1 │     │Shard63 │   │
│  │(独立锁)│ │(独立锁)│     │(独立锁)│   │
│  └────────┘ └────────┘ ... └────────┘   │
└──────────────────────────────────────────┘
```

**优势**：
- 不同 key 可以并发访问不同分片
- 锁竞争从 O(n) 降低到 O(n/shardCount)
- 线性扩展性能

**配置建议**：
- 分片数应为 2 的幂次方（32, 64, 128）
- 对于 QPS > 5000，推荐 64 个分片
- 对于 QPS > 10000，推荐 128 个分片

### 方案 3：解耦全局锁（建议）

**当前状态**：Cache 结构体有全局 RWMutex 保护所有缓存

**建议改进**：
```go
type Cache struct {
    // 为不同缓存类型使用独立的锁
    rawCacheMu    sync.RWMutex
    sortedCacheMu sync.RWMutex
    errorCacheMu  sync.RWMutex
    sortingMu     sync.RWMutex
    
    rawCache     *ShardedCache
    sortedCache  *ShardedCache
    errorCache   *ShardedCache
    sortingState map[string]*SortingState
}
```

**优势**：
- 不同缓存类型的操作不相互阻塞
- 排序任务管理独立于缓存访问

## 性能对比

### 基准测试结果（预期）

运行以下命令查看性能对比：

```bash
go test -bench=. -benchmem cache/cache_benchmark_test.go
```

**预期结果**（相对于原始实现）：

| 场景 | 改进 | 性能提升 |
|------|------|---------|
| 高并发读（80%） | 读友好 LRU | 3-5x |
| 高并发读写混合 | 分片缓存 | 10-20x |
| 极限并发（>10000 QPS） | 分片 + 读友好 | 20-50x |

### 测试用例

1. **LRU Get 性能**：单个 LRU 的读性能
2. **分片缓存 Get 性能**：分片缓存的读性能
3. **混合工作负载**：80% 读 + 20% 写
4. **并发正确性**：多 goroutine 并发访问

## 迁移指南

### 步骤 1：使用分片缓存替换 LRUCache

```go
// 原始代码
rawCache := NewLRUCache(maxEntries)

// 改进后
rawCache := NewShardedCache(maxEntries, 64)
```

### 步骤 2：更新 Cache 结构体

```go
type Cache struct {
    // 使用分片缓存替换 LRUCache
    rawCache    *ShardedCache
    sortedCache *ShardedCache
    errorCache  *ShardedCache
    
    // 为不同缓存使用独立的锁
    rawCacheMu    sync.RWMutex
    sortedCacheMu sync.RWMutex
    errorCacheMu  sync.RWMutex
}
```

### 步骤 3：更新缓存访问代码

```go
// 原始代码
func (c *Cache) GetRaw(domain string, qtype uint16) (*RawCacheEntry, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    // ...
}

// 改进后
func (c *Cache) GetRaw(domain string, qtype uint16) (*RawCacheEntry, bool) {
    c.rawCacheMu.RLock()
    defer c.rawCacheMu.RUnlock()
    // ...
}
```

### 步骤 4：处理 Close 操作

```go
// 在 Cache 的 Close 方法中
func (c *Cache) Close() error {
    c.rawCache.Close()      // 关闭异步处理
    c.sortedCache.Close()
    c.errorCache.Close()
    return nil
}
```

## 监控和调优

### 监控指标

1. **缓存命中率**
   ```go
   hits, misses := c.GetStats()
   hitRate := float64(hits) / float64(hits + misses)
   ```

2. **待处理访问记录**（仅限读友好 LRU）
   ```go
   pending := lru.GetPendingAccess()
   ```

3. **缓存大小**
   ```go
   entries := c.GetCurrentEntries()
   percent := c.GetMemoryUsagePercent()
   ```

### 调优建议

1. **分片数调优**
   - 监控锁竞争情况
   - 如果竞争高，增加分片数
   - 如果内存紧张，减少分片数

2. **访问记录缓冲区调优**
   - 默认 1000，可根据 QPS 调整
   - 缓冲区满时会丢弃记录（可接受）
   - 监控 GetPendingAccess() 的值

3. **容量调优**
   - 根据内存限制调整总容量
   - 分片容量自动计算为 totalCapacity / shardCount

## 常见问题

### Q1：为什么 Get 操作的访问顺序更新会延迟？

A：为了避免在 Get 时获取写锁。延迟通常 <1ms，由后台 goroutine 异步处理。在 DNS 缓存场景中，这种延迟可以接受，因为 LRU 顺序的准确性不如性能重要。

### Q2：分片缓存会增加内存开销吗？

A：每个分片有独立的 map 和链表，会增加约 5-10% 的内存开销。但性能提升通常能弥补这个成本。

### Q3：如何处理跨分片的操作？

A：当前实现中，大多数操作都是单 key 操作，自动路由到对应分片。如果需要全局操作（如 Clear），会遍历所有分片。

### Q4：能否混合使用 LRUCache 和 ShardedCache？

A：可以。不同的缓存类型可以使用不同的实现。例如，rawCache 使用 ShardedCache，errorCache 使用 LRUCache。

## 参考资源

- [Go sync.RWMutex 文档](https://golang.org/pkg/sync/#RWMutex)
- [分片缓存设计模式](https://en.wikipedia.org/wiki/Sharding)
- [LRU 缓存实现](https://en.wikipedia.org/wiki/Cache_replacement_policies#LRU)
