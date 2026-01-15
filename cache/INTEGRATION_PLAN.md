# 缓存优化集成计划

## 概述

本计划分为三个阶段，逐步将缓存系统从单锁设计迁移到分片 + 读友好设计。

## 阶段 1：验证和基准测试（立即执行）

### 目标
- 验证新实现的正确性
- 建立性能基准
- 确认优化效果

### 执行步骤

1. **运行单元测试**
   ```bash
   go test -v cache/cache_benchmark_test.go
   ```
   验证 LRUCache 和 ShardedCache 的正确性

2. **运行基准测试**
   ```bash
   go test -bench=. -benchmem cache/cache_benchmark_test.go
   ```
   对比性能差异

3. **分析结果**
   - 记录各场景的性能数据
   - 确认分片缓存的性能优势
   - 评估是否满足 QPS 需求

### 预期结果
- 所有测试通过
- 分片缓存在高并发场景下性能提升 10-20x
- 读友好 LRU 在读密集场景下性能提升 3-5x

---

## 阶段 2：逐步迁移（第 1-2 周）

### 目标
- 将 rawCache 迁移到 ShardedCache
- 验证与现有代码的兼容性
- 监控性能和稳定性

### 执行步骤

1. **更新 Cache 初始化**

   在 `cache/cache.go` 中修改 `NewCache` 函数：

   ```go
   func NewCache(cfg *config.CacheConfig) *Cache {
       maxEntries := cfg.CalculateMaxEntries()
       msgCacheEntries := 0
       if cfg.MsgCacheSizeMB > 0 {
           msgCacheEntries = (cfg.MsgCacheSizeMB * 1024 * 1024) / 2048
           msgCacheEntries = max(msgCacheEntries, 10)
       }

       return &Cache{
           config:          cfg,
           maxEntries:      maxEntries,
           rawCache:        NewShardedCache(maxEntries, 64),  // 改为分片缓存
           sortedCache:     NewLRUCache(maxEntries),          // 保持不变
           sortingState:    make(map[string]*SortingState),
           errorCache:      NewLRUCache(maxEntries),          // 保持不变
           blockedCache:    make(map[string]*BlockedCacheEntry),
           allowedCache:    make(map[string]*AllowedCacheEntry),
           msgCache:        NewLRUCache(msgCacheEntries),     // 保持不变
           recentlyBlocked: NewRecentlyBlockedTracker(),
       }
   }
   ```

2. **更新 Cache 结构体**

   在 `cache/cache.go` 中修改 Cache 结构体：

   ```go
   type Cache struct {
       mu sync.RWMutex
       
       config       *config.CacheConfig
       maxEntries   int
       rawCache     *ShardedCache        // 改为 ShardedCache
       sortedCache  *LRUCache
       sortingState map[string]*SortingState
       errorCache   *LRUCache
       blockedCache map[string]*BlockedCacheEntry
       allowedCache map[string]*AllowedCacheEntry
       msgCache     *LRUCache
       
       prefetcher      PrefetchChecker
       recentlyBlocked RecentlyBlockedTracker
       hits            int64
       misses          int64
   }
   ```

3. **验证兼容性**

   由于 ShardedCache 和 LRUCache 有相同的接口（Get, Set, Delete, Len, Clear），
   现有代码无需修改即可工作。

4. **运行集成测试**
   ```bash
   go test -v ./cache/...
   ```

5. **性能监控**
   - 在生产环境中运行 1-2 周
   - 监控缓存命中率
   - 监控 CPU 和内存使用
   - 收集性能数据

### 验证清单
- [ ] 所有缓存操作正常
- [ ] 缓存命中率保持或提升
- [ ] CPU 使用率下降
- [ ] 内存使用率稳定
- [ ] 没有新的错误日志

---

## 阶段 3：完全优化（第 2-3 周）

### 目标
- 将所有 LRUCache 迁移到 ShardedCache
- 解耦全局锁
- 实现完整的性能优化

### 执行步骤

1. **迁移 sortedCache 和 errorCache**

   ```go
   func NewCache(cfg *config.CacheConfig) *Cache {
       maxEntries := cfg.CalculateMaxEntries()
       
       return &Cache{
           config:          cfg,
           maxEntries:      maxEntries,
           rawCache:        NewShardedCache(maxEntries, 64),
           sortedCache:     NewShardedCache(maxEntries, 64),  // 改为分片缓存
           sortingState:    make(map[string]*SortingState),
           errorCache:      NewShardedCache(maxEntries, 64),  // 改为分片缓存
           blockedCache:    make(map[string]*BlockedCacheEntry),
           allowedCache:    make(map[string]*AllowedCacheEntry),
           msgCache:        NewShardedCache(msgCacheEntries, 32),
           recentlyBlocked: NewRecentlyBlockedTracker(),
       }
   }
   ```

2. **解耦全局锁**

   ```go
   type Cache struct {
       // 为不同缓存类型使用独立的锁
       rawCacheMu    sync.RWMutex
       sortedCacheMu sync.RWMutex
       errorCacheMu  sync.RWMutex
       sortingMu     sync.RWMutex
       
       config       *config.CacheConfig
       maxEntries   int
       rawCache     *ShardedCache
       sortedCache  *ShardedCache
       sortingState map[string]*SortingState
       errorCache   *ShardedCache
       blockedCache map[string]*BlockedCacheEntry
       allowedCache map[string]*AllowedCacheEntry
       msgCache     *ShardedCache
       
       prefetcher      PrefetchChecker
       recentlyBlocked RecentlyBlockedTracker
       hits            int64
       misses          int64
   }
   ```

3. **更新所有缓存访问方法**

   示例（cache_raw.go）：
   ```go
   // 改前
   func (c *Cache) GetRaw(domain string, qtype uint16) (*RawCacheEntry, bool) {
       c.mu.RLock()
       defer c.mu.RUnlock()
       key := cacheKey(domain, qtype)
       value, exists := c.rawCache.Get(key)
       // ...
   }

   // 改后
   func (c *Cache) GetRaw(domain string, qtype uint16) (*RawCacheEntry, bool) {
       c.rawCacheMu.RLock()
       defer c.rawCacheMu.RUnlock()
       key := cacheKey(domain, qtype)
       value, exists := c.rawCache.Get(key)
       // ...
   }
   ```

4. **更新排序状态管理**

   ```go
   func (c *Cache) GetOrStartSort(domain string, qtype uint16) (*SortingState, bool) {
       c.sortingMu.Lock()
       defer c.sortingMu.Unlock()
       
       key := cacheKey(domain, qtype)
       if state, exists := c.sortingState[key]; exists {
           return state, false
       }
       
       newState := &SortingState{
           InProgress: true,
           Done:       make(chan struct{}),
       }
       c.sortingState[key] = newState
       return newState, true
   }
   ```

5. **添加 Close 方法**

   ```go
   func (c *Cache) Close() error {
       // 关闭所有异步处理
       if sc, ok := c.rawCache.(*ShardedCache); ok {
           // ShardedCache 不需要关闭
       }
       if lru, ok := c.msgCache.(*LRUCache); ok {
           lru.Close()
       }
       return nil
   }
   ```

6. **完整测试**
   ```bash
   go test -v ./...
   go test -bench=. -benchmem ./cache/...
   ```

### 验证清单
- [ ] 所有单元测试通过
- [ ] 所有集成测试通过
- [ ] 性能基准测试显示显著提升
- [ ] 没有竞争条件（使用 `go test -race`）
- [ ] 内存使用合理

---

## 性能验证

### 运行竞争检测
```bash
go test -race ./cache/...
```

### 运行压力测试
```bash
go test -count=100 -race ./cache/...
```

### 生成性能报告
```bash
go test -bench=. -benchmem -cpuprofile=cpu.prof ./cache/...
go tool pprof cpu.prof
```

---

## 回滚计划

如果在任何阶段发现问题，可以快速回滚：

1. **阶段 1 回滚**：无需回滚，仅添加新代码
2. **阶段 2 回滚**：将 rawCache 改回 LRUCache
3. **阶段 3 回滚**：将所有 ShardedCache 改回 LRUCache，恢复全局锁

---

## 预期收益

| 指标 | 改进前 | 改进后 | 提升 |
|------|--------|--------|------|
| 高并发读 QPS | 5,000 | 50,000+ | 10x |
| 平均延迟 | 10ms | 1ms | 10x |
| CPU 使用率 | 80% | 30% | 62% ↓ |
| 缓存命中率 | 95% | 95%+ | 稳定或提升 |

---

## 时间表

| 阶段 | 任务 | 时间 | 负责人 |
|------|------|------|--------|
| 1 | 验证和基准测试 | 1-2 天 | - |
| 2 | 迁移 rawCache | 3-5 天 | - |
| 2 | 生产环境监控 | 1-2 周 | - |
| 3 | 迁移其他缓存 | 3-5 天 | - |
| 3 | 解耦全局锁 | 2-3 天 | - |
| 3 | 完整测试和验证 | 2-3 天 | - |

**总计**：3-4 周

---

## 注意事项

1. **向后兼容性**：ShardedCache 和 LRUCache 有相同的接口，迁移无需修改调用代码
2. **性能监控**：在生产环境中持续监控性能指标
3. **逐步迁移**：不要一次性迁移所有缓存，逐步验证
4. **文档更新**：更新相关文档和注释
5. **团队沟通**：在迁移前与团队沟通计划和预期

---

## 相关文件

- `cache/sharded_cache.go` - 分片缓存实现
- `cache/lru_cache.go` - 改进的 LRU 缓存（读友好）
- `cache/cache_benchmark_test.go` - 性能基准测试
- `cache/OPTIMIZATION_GUIDE.md` - 详细优化指南
