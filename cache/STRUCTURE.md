# Cache Package Structure

## Overview
cache 包已被拆分为多个文件，以提高代码的可维护性和可读性。

## File Organization

### 1. **lru_cache.go**
LRU 缓存的核心实现。
- `LRUCache` 结构体：使用哈希表 + 双向链表实现 O(1) 的 Get/Set 操作
- `lruNode` 结构体：链表中的节点
- 主要方法：`Get()`, `Set()`, `Delete()`, `Clear()`, `Len()`

### 2. **entries.go**
各种缓存条目类型的定义。
- `RawCacheEntry`：原始缓存项（上游 DNS 响应）
- `SortedCacheEntry`：排序后的缓存项
- `SortingState`：排序任务状态
- `ErrorCacheEntry`：错误响应缓存项
- `DNSSECCacheEntry`：DNSSEC 完整消息缓存项
- `PersistentCacheEntry`：用于持久化的缓存项

### 3. **cache.go**
主缓存管理器的核心逻辑。
- `Cache` 结构体：DNS 缓存管理器
- `PrefetchChecker` 接口：热点域名检查接口
- 初始化和统计方法：`NewCache()`, `GetStats()`, `RecordHit()`, `RecordMiss()`
- 缓存管理方法：`Clear()`, `CleanExpired()`, `GetCurrentEntries()`, `GetMemoryUsagePercent()`

### 4. **cache_operations.go**
缓存操作方法的实现。
- 原始缓存操作：`GetRaw()`, `SetRaw()`, `SetRawWithDNSSEC()`
- 排序缓存操作：`GetSorted()`, `SetSorted()`, `GetOrStartSort()`, `FinishSort()`, `CancelSort()`
- 错误缓存操作：`GetError()`, `SetError()`
- 内部辅助方法：`getRawCacheSnapshot()`, `getRawCacheKeysSnapshot()`, `extractDomain()`, `isProtectedDomain()`
- 清理方法：`cleanExpiredSortedCache()`, `cleanExpiredErrorCache()`, `cleanCompletedSortingStates()`

### 5. **cache_persistence.go**
缓存持久化相关的方法。
- 磁盘操作：`SaveToDisk()`, `LoadFromDisk()`
- DNSSEC 消息缓存：`GetMsg()`, `SetMsg()`
- 辅助方法：`extractMinTTLFromMsg()`

### 6. **adblock_cache.go**
广告拦截缓存的实现。
- `BlockedCacheEntry`：拦截缓存项
- `AllowedCacheEntry`：白名单缓存项
- 操作方法：`GetBlocked()`, `SetBlocked()`, `GetAllowed()`, `SetAllowed()`
- 清理方法：`cleanAdBlockCaches()`

### 7. **sortqueue.go**
排序队列的实现（已存在）。

## Dependencies

```
lru_cache.go
    ↓
entries.go
    ↓
cache.go ← cache_operations.go ← cache_persistence.go
    ↓
adblock_cache.go
```

## Usage Example

```go
// 创建缓存实例
cfg := &config.CacheConfig{...}
cache := NewCache(cfg)

// 设置原始缓存
cache.SetRaw("example.com", dns.TypeA, []string{"1.1.1.1"}, nil, 300)

// 获取原始缓存
entry, ok := cache.GetRaw("example.com", dns.TypeA)

// 设置排序缓存
sortedEntry := &SortedCacheEntry{
    IPs:       []string{"1.1.1.1"},
    RTTs:      []int{10},
    Timestamp: time.Now(),
    TTL:       300,
    IsValid:   true,
}
cache.SetSorted("example.com", dns.TypeA, sortedEntry)

// 清理过期缓存
cache.CleanExpired()
```

## Testing

所有测试文件都保持不变：
- `cache_test.go`：主要缓存功能测试
- `error_cache_test.go`：错误缓存测试
