# 缓存过期清理机制 - 混合动力方案

## 核心设计理念

**职责划分**：
- **Get 操作**：负责 Fresh → Stale 的判断，触发异步刷新
- **Heap 清理**：精确删除 Dead 数据（超过 Hard Limit）
- **LRU 链表**：自动淘汰热度低的数据

## 三个生命周期

```
Fresh (0 ~ TTL)
  ↓ 时间流逝
Stale (TTL ~ Hard Limit)
  ↓ 时间流逝
Dead (> Hard Limit) → 被 Heap 清理删除
```

### Fresh 周期
- 数据在 TTL 内
- Get 直接返回，不做任何事
- 无需后台清理

### Stale 周期
- 数据超过 TTL 但未到 Hard Limit（如 TTL 的 2 倍）
- Get 发现超期：
  - 直接返回旧数据给用户
  - 触发异步刷新任务，将域名丢入刷新队列
- 后台清理不干预

### Dead 周期
- 数据超过 Hard Limit（如 TTL 的 2 倍）
- Heap 精确定位并彻底删除
- 释放内存

## 实现细节

### 1. 过期堆（Expired Heap）

**数据结构**：
```go
type expireEntry struct {
    key    string
    expiry int64
}

type expireHeap []expireEntry  // 实现 container/heap.Interface
```

**维护时机**：
- 每次 `SetRaw*` 时，计算过期时间并使用 `heap.Push()` 添加到堆
- 堆中元素按过期时间自动排序（最小堆）

**清理逻辑**：
```go
func (c *Cache) CleanExpired() {
    // 计算 Hard Limit（TTL 的 2 倍，但不低于 600s）
    hardLimit := max(2 * maxTTL, 600)
    
    // 从堆顶开始删除超过 Hard Limit 的数据
    for len(heap) > 0 {
        entry := heap[0]
        if entry.expiry > hardLimit {
            break  // 后续都不用删
        }
        delete(entry.key)
        heap.Pop()  // O(log N)
    }
}
```

### 2. 堆的维护成本

**追加操作**：O(log N)
- 使用 `heap.Push()` 自动维护堆的有序性

**删除操作**：O(log N)
- 使用 `heap.Pop()` 删除堆顶

**清理操作**：O(k log k)
- k = 已过期数据数量（通常极小）
- 只删除真正该死的数据

### 3. 与 LRU 的协作

**LRU 的作用**：
- 自动淘汰热度低的数据
- 过期但未被访问的数据会逐渐移到链表尾部
- 容量满时自动删除尾部元素

**Heap 的作用**：
- 精确删除超过 Hard Limit 的数据
- 防止僵尸数据长期占用内存

## 性能特性

| 操作 | 复杂度 | 说明 |
|------|--------|------|
| Get | O(1) | 读锁，异步更新访问顺序 |
| Set | O(log N) | 堆插入 |
| CleanExpired | O(k log k) | k = 已过期数据数量 |
| 堆操作 | O(log N) | 稳定的对数时间 |

## 优势

✅ **简洁**：无复杂的状态机，职责清晰
✅ **高效**：清理时间与过期数据数量成正比，不是全表扫描
✅ **无全局锁**：Get 操作不受清理影响
✅ **自适应**：LRU 自动处理热度低的数据

## 注意事项

1. **堆的有序性**：
   - 追加时无序，每 1000 个元素排序一次
   - 清理时从堆顶开始，遇到未过期数据立即停止

2. **Hard Limit 的设置**：
   - 默认为 MaxTTLSeconds 的 2 倍
   - 可根据业务需求调整

3. **异步刷新队列**：
   - Get 发现 Stale 数据时触发
   - 需要单独的刷新机制实现

## 与现有代码的集成

### 修改点

1. **cache.go**：
   - 添加 `expiredHeap []string` 字段
   - 实现 `CleanExpired()` 新逻辑
   - 添加 `addToExpiredHeap()` 辅助方法

2. **cache_raw.go**：
   - `SetRawWithDNSSEC()` 调用 `addToExpiredHeap()`
   - `SetRawRecordsWithDNSSEC()` 调用 `addToExpiredHeap()`

3. **entries.go**：
   - 无需修改（现有的 `IsExpired()` 方法保持不变）

### 向后兼容

- 所有现有的 Get/Set 接口保持不变
- 清理逻辑完全独立，不影响读写路径
- 可以逐步迁移，无需一次性重构
