# 缓存清理机制 - 快速参考

## 一句话总结

**Get 负责刷新，Heap 负责彻底回收** - 职责清晰，性能高效。

## 核心概念

### 三个生命周期

```
Fresh (0 ~ TTL)
  ↓ Get 直接返回
Stale (TTL ~ Hard Limit)
  ↓ Get 返回旧数据 + 异步刷新
Dead (> Hard Limit)
  ↓ Heap 删除
```

### 极简堆 → 标准堆

**改进前**：
```go
expiredHeap []string  // 格式: "key|expiryTime"
```

**改进后**：
```go
type expireEntry struct {
    key    string
    expiry int64
}

expiredHeap expireHeap  // 实现 container/heap.Interface
```

**性能对比**：
- 插入：O(1) → O(log N)（但无字符串分配）
- 排序：O(n log n) 每 1000 个 → O(log N) 每次
- 清理：O(k) → O(k log k)（k 通常极小）

## 关键方法

### 添加数据到堆

```go
// 在 SetRaw* 时调用
c.mu.Lock()
expiryTime := timeNow().Unix() + int64(upstreamTTL)
c.addToExpiredHeap(key, expiryTime)
c.mu.Unlock()

// addToExpiredHeap 的实现
func (c *Cache) addToExpiredHeap(key string, expiryTime int64) {
    entry := expireEntry{key: key, expiry: expiryTime}
    heap.Push(&c.expiredHeap, entry)  // O(log N)
}
```

### 清理过期数据

```go
// 后台定期调用
c.CleanExpired()

// CleanExpired 的核心逻辑
func (c *Cache) CleanExpired() {
    // Hard Limit = max(2 * TTL, 600s)
    hardLimit := max(2 * maxTTL, 600)
    
    // 从堆顶开始删除
    for len(c.expiredHeap) > 0 {
        entry := c.expiredHeap[0]
        if entry.expiry > now + hardLimit {
            break
        }
        c.rawCache.Delete(entry.key)
        heap.Pop(&c.expiredHeap)  // O(log N)
    }
}
```

### 解析堆条目

```go
// 直接访问结构体字段
key := entry.key
expiryTime := entry.expiry
```

## 性能指标

| 操作 | 复杂度 | 说明 |
|------|--------|------|
| Get | O(1) | 无锁读 |
| Set | O(log N) | 堆插入 |
| CleanExpired | O(k log k) | k = 过期数据数量 |

## 配置参数

### Hard Limit

```go
// 计算公式：max(2 * MaxTTLSeconds, 600)
// 最少保留 10 分钟，确保异步刷新有足够时间

const minHardLimit = 600  // 秒

hardLimit := int64(c.config.MaxTTLSeconds) * 2
if hardLimit < minHardLimit {
    hardLimit = minHardLimit
}
```

可在 `cache.go` 的 `CleanExpired()` 中调整 `minHardLimit` 值。

## 常见问题

### Q: 为什么用 container/heap 而不是简单的 []string？

A: 
- 无字符串拆分的 GC 压力
- O(log N) 稳定的插入性能
- 避免了每 1000 个元素排序一次的延迟抖动

### Q: Hard Limit 为什么是 max(2*TTL, 600s)？

A:
- TTL 很短（10s）时，2*TTL = 20s 太短，异步刷新可能还没完成
- 最少保留 10 分钟，给异步刷新充足的时间窗口
- TTL 很长（3600s）时，2*TTL = 7200s，不受 minHardLimit 影响

### Q: LRU 和 Heap 如何协作？

A: 
- LRU：自动淘汰热度低的数据
- Heap：精确删除超过 Hard Limit 的数据
- 两者互补，无冲突

## 集成检查清单

- [ ] `cache.go` 添加 `expiredHeap` 字段
- [ ] `cache.go` 实现 `CleanExpired()` 新逻辑
- [ ] `cache.go` 添加 `addToExpiredHeap()` 方法
- [ ] `cache_raw.go` 修改 `SetRawWithDNSSEC()`
- [ ] `cache_raw.go` 修改 `SetRawRecordsWithDNSSEC()`
- [ ] 运行测试验证
- [ ] 监控 P999 延迟改善

## 下一步

1. **异步刷新队列**：实现 Get 发现 Stale 时的刷新机制
2. **监控指标**：添加堆大小、清理效率的监控
3. **性能测试**：对比优化前后的 P999 延迟
4. **生产验证**：灰度发布，观察实际效果
