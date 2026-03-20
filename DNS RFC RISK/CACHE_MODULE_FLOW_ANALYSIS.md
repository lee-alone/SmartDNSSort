# Cache 模块深度流程分析报告

## 1. 概述

本报告对 SmartDNSSort 项目的 cache 模块进行深度流程分析，评估其设计合理性、潜在风险和改进建议。

---

## 2. 缓存架构总览

### 2.1 核心数据结构

```
┌─────────────────────────────────────────────────────────────────┐
│                         Cache 结构                               │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │  rawCache    │  │ sortedCache  │  │ errorCache   │          │
│  │ (ShardedCache)│  │ (LRUCache)   │  │ (LRUCache)   │          │
│  │  原始DNS响应  │  │  排序后结果   │  │  错误响应    │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
│                                                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │ blockedCache │  │ allowedCache │  │  msgCache    │          │
│  │   (map)      │  │   (map)      │  │ (LRUCache)   │          │
│  │  拦截缓存    │  │  白名单缓存   │  │ DNSSEC消息   │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
│                                                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │ sortingState │  │ expiredHeap  │  │recentlyBlocked│          │
│  │   (map)      │  │ (heap)       │  │  (Tracker)   │          │
│  │  排序状态    │  │  过期堆      │  │ 最近拦截     │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
└─────────────────────────────────────────────────────────────────┘
```

### 2.2 缓存层次结构

| 缓存类型 | 数据结构 | 用途 | 容量管理 |
|---------|---------|------|---------|
| `rawCache` | ShardedCache (64分片) | 存储上游DNS原始响应 | LRU + 分片锁 |
| `sortedCache` | LRUCache | 存储IP排序结果 | LRU驱逐 |
| `errorCache` | LRUCache | 缓存错误响应(NXDOMAIN等) | LRU驱逐 |
| `blockedCache` | map | AdBlock拦截结果缓存 | 过期清理 |
| `allowedCache` | map | AdBlock白名单缓存 | 过期清理 |
| `msgCache` | LRUCache | DNSSEC完整消息缓存 | LRU驱逐 |

---

## 3. 初始化流程分析

### 3.1 初始化流程图

```
NewCache(cfg)
    │
    ├─── 计算maxEntries = cfg.CalculateMaxEntries()
    │
    ├─── 计算msgCache容量 (MsgCacheSizeMB / 2KB)
    │
    ├─── 创建各缓存实例
    │    ├── rawCache = NewShardedCache(maxEntries, 64)
    │    ├── sortedCache = NewLRUCache(maxEntries)
    │    ├── errorCache = NewLRUCache(maxEntries)
    │    ├── blockedCache = make(map)
    │    ├── allowedCache = make(map)
    │    ├── msgCache = NewLRUCache(msgCacheEntries)
    │    └── recentlyBlocked = NewRecentlyBlockedTracker()
    │
    ├─── 设置sortedCache驱逐回调
    │    └── 回调: ipPoolUpdater.UpdateDomainIPs()
    │
    ├─── 创建过期堆channel
    │    └── addHeapChan = make(chan, 10000)
    │
    └─── 启动后台worker
         └── startHeapWorker()
```

### 3.2 初始化合理性评估

#### ✅ 优点

1. **分片缓存设计**: `rawCache`使用64分片的`ShardedCache`，有效降低锁竞争
2. **异步堆维护**: 过期堆通过channel异步添加，避免Set路径的全局锁
3. **容量预计算**: 根据配置动态计算各缓存容量，灵活适配不同场景
4. **驱逐回调机制**: `sortedCache`设置驱逐回调，自动维护IP池引用计数

#### ⚠️ 潜在问题

1. **msgCache容量计算假设**
   - 代码假设平均DNS消息~2KB，但实际DNSSEC消息可能更大
   - 建议：增加配置项或动态调整

2. **channel缓冲区大小固定**
   - `addHeapChan`固定10000，高并发下可能不足
   - 已有`heapChannelFullCount`监控，但缺少自适应扩容机制

---

## 4. 读写流程分析

### 4.1 写入流程 (SetRaw)

```
SetRaw(domain, qtype, ips, cnames, upstreamTTL)
    │
    ├─── 计算EffectiveTTL = calculateEffectiveTTL(upstreamTTL)
    │    ├── 应用MinTTL限制
    │    └── 应用MaxTTL限制
    │
    ├─── 生成QueryVersion = timeNow().UnixNano()
    │
    ├─── 创建RawCacheEntry
    │    ├── IPs, CNAMEs, Records
    │    ├── UpstreamTTL, EffectiveTTL
    │    ├── AcquisitionTime
    │    └── QueryVersion
    │
    ├─── rawCache.Set(key, entry)  [无全局锁]
    │
    └─── addToExpiredHeap(key, expiryTime, queryVersion)  [异步]
         └── 非阻塞发送到addHeapChan
```

### 4.2 读取流程 (GetRaw)

```
GetRaw(domain, qtype)
    │
    ├─── 生成cacheKey = domain + "#" + qtype
    │
    ├─── rawCache.Get(key)  [分片读锁]
    │    ├── 获取分片 shard = getShard(key)
    │    ├── shard.mu.RLock()
    │    ├── 查找节点 node, exists := shard.cache[key]
    │    └── shard.mu.RUnlock()
    │
    └─── 异步更新LRU顺序
         └── shard.recordAccess(key) -> accessChan
```

### 4.3 三段式过期判定

```
GetStateWithConfig(keepExpired, gracePeriodSeconds)
    │
    ├─── 计算过期时间点
    │    ├── expiresAt = AcquisitionTime + EffectiveTTL
    │    └── graceExpiresAt = expiresAt + gracePeriodSeconds
    │
    └─── 三段式判定
         ├── now < expiresAt        → FRESH (新鲜)
         ├── expiresAt ≤ now < graceExpiresAt → STALE (陈旧可用)
         └── now ≥ graceExpiresAt   → EXPIRED (彻底过期)
```

### 4.4 读写流程合理性评估

#### ✅ 优点

1. **无锁读取**: `ShardedCache`使用分片读锁，读取不阻塞其他分片
2. **异步LRU更新**: 访问顺序更新通过channel异步处理，不阻塞读操作
3. **版本号机制**: `QueryVersion`防止旧数据覆盖新数据
4. **三段式过期**: 支持Stale-While-Revalidate模式，提高可用性

#### ⚠️ 潜在问题

1. **异步更新丢失风险**
   - `accessChan`满时丢弃访问记录，可能导致LRU顺序不准确
   - 建议：增加监控告警，或实现降级策略

2. **cacheKey格式简单**
   - 格式`domain#qtype`，没有考虑大小写规范化
   - DNS域名不区分大小写，可能导致重复缓存

---

## 5. 清理机制分析

### 5.1 清理流程图

```
CleanExpired()
    │
    ├─── 获取内存使用率 usage = getMemoryUsagePercentLocked()
    │
    ├─── 动态计算ancientLimit
    │    ├── usage < 0.5     → 24小时 (AncientLimitLowPressure)
    │    ├── 0.5 ≤ usage < threshold → 2小时 (AncientLimitMidPressure)
    │    └── usage ≥ threshold → 0 (立即清理)
    │
    ├─── 第一阶段：清理幽灵索引
    │    └── 遍历expiredHeap，删除缓存中不存在的索引
    │
    ├─── 第二阶段：清理实际过期条目
    │    ├── 高压力：清理所有已过期数据
    │    ├── 低压力+未开启KeepExpired：清理超过ancientLimit的数据
    │    └── 低压力+开启KeepExpired：保留所有数据
    │
    └─── 清理辅助缓存
         ├── cleanExpiredSortedCache()
         ├── cleanExpiredErrorCache()
         ├── cleanCompletedSortingStates()
         └── cleanAdBlockCaches()
```

### 5.2 清理限制机制

```go
const (
    MaxCleanupBatchSize = 200           // 单次最多清理条目数
    MaxCleanupDuration = 10ms           // 单次最长耗时
    MaxStaleHeapCleanupSize = 500       // 幽灵索引清理上限
)
```

### 5.3 清理机制合理性评估

#### ✅ 优点

1. **压力驱动策略**: 根据内存压力动态调整清理策略
2. **批量限制**: 防止单次清理耗时过长影响DNS查询
3. **版本号校验**: 清理时校验版本号，避免误删新数据
4. **两阶段清理**: 先清理幽灵索引，再清理实际过期条目

#### ⚠️ 潜在问题

1. **清理触发时机不明确**
   - 代码未展示清理的定时触发机制
   - 建议：确认是否有定时清理goroutine

2. **ancientLimit硬编码**
   - 24小时/2小时的阈值硬编码
   - 建议：可配置化

3. **幽灵索引计数可能不准确**
   - `staleHeapCount`只在清理时更新
   - 非清理期间可能不准确

---

## 6. 持久化机制分析

### 6.1 保存流程

```
SaveToDisk(filename)
    │
    ├─── 脏数据检查
    │    └── if lastSavedDirty == currentDirty → 跳过保存
    │
    ├─── 获取一致性快照
    │    └── snapshot = rawCache.GetSnapshot()  [分片锁定]
    │
    ├─── 准备持久化条目
    │    └── 遍历snapshot，提取IPs, CNAMEs
    │
    └─── 原子写入
         ├── 创建临时文件 filename.tmp
         ├── Gob流式编码
         └── os.Rename() 原子替换
```

### 6.2 加载流程

```
LoadFromDisk(filename)
    │
    ├─── 打开文件
    │    └── 文件不存在时返回nil
    │
    ├─── Gob解码
    │
    └─── 重建缓存
         ├── 遍历entries
         ├── 创建RawCacheEntry (TTL=300)
         └── rawCache.Set(key, entry)
```

### 6.3 持久化合理性评估

#### ✅ 优点

1. **脏数据检查**: 无变更时跳过保存，减少IO
2. **原子写入**: 使用临时文件+rename，保证数据一致性
3. **流式编码**: 使用Gob Encoder直接写入，减少内存分配

#### ⚠️ 潜在问题

1. **加载时TTL固定为300秒**
   - 加载后的数据TTL固定，可能不符合原始TTL策略
   - 建议：持久化原始TTL信息

2. **只持久化rawCache**
   - sortedCache、errorCache等未持久化
   - 重启后需要重新排序/重建

3. **缺少校验机制**
   - 加载时未校验数据完整性
   - 建议：添加校验和或版本号

---

## 7. 统计与错误处理分析

### 7.1 统计指标

| 指标 | 类型 | 用途 |
|-----|------|------|
| `hits` | atomic int64 | 缓存命中计数 |
| `misses` | atomic int64 | 缓存未命中计数 |
| `evictions` | atomic int64 | 驱逐计数 |
| `actualExpiredCount` | int64 | 实际过期条目数 |
| `staleHeapCount` | int64 | 幽灵索引计数 |
| `heapChannelFullCount` | int64 | channel满次数 |

### 7.2 错误缓存机制

```
SetError(domain, qtype, rcode, ttl)
    │
    └─── errorCache.Set(key, ErrorCacheEntry{rcode, time, ttl})

GetError(domain, qtype)
    │
    ├─── errorCache.Get(key)
    └─── 检查是否过期
```

### 7.3 统计与错误处理合理性评估

#### ✅ 优点

1. **原子计数器**: 使用atomic操作，无锁统计
2. **错误缓存**: 缓存NXDOMAIN等错误响应，减少上游压力
3. **监控指标完善**: 提供丰富的监控指标

#### ⚠️ 潜在问题

1. **actualExpiredCount增量更新**
   - 清理时递减，但新增过期条目时未递增
   - 依赖`recalculateActualExpiredCount`重新计算
   - 可能导致统计不准确

2. **缺少Prometheus集成**
   - 统计指标未暴露给Prometheus
   - 建议：添加metrics导出接口

---

## 8. 并发安全分析

### 8.1 锁层次结构

```
Cache.mu (全局锁)
    ├── 清理操作 (CleanExpired)
    ├── 排序状态管理 (GetOrStartSort, FinishSort)
    └── AdBlock缓存操作

ShardedCache.shards[i].mu (分片锁)
    ├── rawCache读写
    └── 分片内LRU操作

LRUCache.mu (独立锁)
    ├── sortedCache读写
    ├── errorCache读写
    └── msgCache读写
```

### 8.2 异步处理机制

| 组件 | Channel | Worker | 用途 |
|-----|---------|--------|------|
| ShardedCache | accessChan | processAccessRecords | 异步LRU更新 |
| LRUCache | accessChan | processAccessRecords | 异步LRU更新 |
| Cache | addHeapChan | heapWorker | 异步过期堆维护 |

### 8.3 并发安全合理性评估

#### ✅ 优点

1. **分片锁设计**: 大幅降低锁竞争
2. **读写分离**: 使用RLock/RUnlock，允许并发读
3. **异步处理**: 高频操作通过channel异步化

#### ⚠️ 潜在问题

1. **锁粒度不一致**
   - `rawCache`操作无需全局锁
   - 但`sortedCache`操作需要全局锁（通过LRUCache内部锁）
   - 可能导致不一致窗口

2. **Channel满时的行为**
   - `addHeapChan`满时丢弃，记录计数
   - `accessChan`满时丢弃，无记录
   - 建议：统一处理策略

---

## 9. DNS RFC合规性分析

### 9.1 TTL处理

| RFC要求 | 实现状态 | 说明 |
|---------|---------|------|
| RFC 1035: TTL定义 | ✅ 符合 | 使用uint32存储TTL |
| RFC 2181: TTL上限 | ⚠️ 部分 | MaxTTLSeconds可配置，但未强制上限 |
| RFC 2308: 负缓存 | ✅ 符合 | 实现errorCache和SOA记录 |

### 9.2 缓存行为

| RFC要求 | 实现状态 | 说明 |
|---------|---------|------|
| RFC 1034: 缓存基本行为 | ✅ 符合 | 正确缓存DNS响应 |
| RFC 2181: 缓存数据一致性 | ⚠️ 部分 | 版本号机制，但跨缓存一致性需关注 |
| RFC 8499: Stale缓存 | ✅ 符合 | 实现Stale-While-Revalidate |

---

## 10. 问题汇总与改进建议

### 10.1 高优先级问题

| 问题 | 影响 | 建议 |
|-----|------|------|
| cacheKey大小写未规范化 | 可能重复缓存 | 统一转小写 |
| 加载时TTL固定300秒 | TTL策略失效 | 持久化原始TTL |
| 清理触发时机不明确 | 内存泄漏风险 | 确认定时清理机制 |

### 10.2 中优先级问题

| 问题 | 影响 | 建议 |
|-----|------|------|
| actualExpiredCount增量更新不准确 | 统计偏差 | 改为实时更新 |
| msgCache容量假设固定2KB | 容量估算偏差 | 动态调整或配置化 |
| 只持久化rawCache | 重启后性能下降 | 扩展持久化范围 |

### 10.3 低优先级问题

| 问题 | 影响 | 建议 |
|-----|------|------|
| ancientLimit硬编码 | 灵活性不足 | 可配置化 |
| 缺少Prometheus集成 | 监控不便 | 添加metrics导出 |
| channel满时行为不一致 | 监控盲区 | 统一处理策略 |

---

## 11. 流程合理化总体评估

### 11.1 评分矩阵

| 维度 | 评分 | 说明 |
|-----|------|------|
| 架构设计 | ⭐⭐⭐⭐⭐ | 分片缓存、异步处理设计优秀 |
| 并发安全 | ⭐⭐⭐⭐ | 分片锁设计好，但跨缓存一致性需关注 |
| RFC合规 | ⭐⭐⭐⭐ | 基本符合RFC要求，Stale-While-Revalidate实现完善 |
| 可维护性 | ⭐⭐⭐⭐ | 代码结构清晰，注释完善 |
| 可观测性 | ⭐⭐⭐ | 统计指标丰富，但缺少外部导出 |
| 容错性 | ⭐⭐⭐⭐ | 原子写入、版本号机制保障数据安全 |

### 11.2 总结

Cache模块整体设计合理，采用了多种优化技术（分片缓存、异步LRU更新、异步过期堆维护）来提升性能。三段式过期判定和Stale-While-Revalidate机制的实现符合现代DNS缓存的最佳实践。

主要改进方向：
1. 完善跨缓存一致性保障
2. 增强可观测性（Prometheus集成）
3. 优化持久化策略（保存更多状态）
4. 统一异常处理策略

---

## 12. 附录：关键代码路径

### 12.1 查询处理主路径

```
dnsserver.handleQuery()
    │
    ├─── 检查拦截缓存 (blockedCache)
    ├─── 检查白名单缓存 (allowedCache)
    ├─── 检查错误缓存 (errorCache)
    │
    ├─── 检查排序缓存 (sortedCache)
    │    └── handleSortedCacheHit()
    │
    ├─── 检查原始缓存 (rawCache)
    │    └── handleRawCacheHit()
    │         ├── FRESH: 返回 + 异步测速
    │         ├── STALE: 返回 + 异步刷新
    │         └── EXPIRED: 去上游查询
    │
    └─── 上游查询
         └── SetRaw() + 触发排序
```

### 12.2 后台任务

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│  heapWorker     │     │ processAccess   │     │  定时清理       │
│  (过期堆维护)   │     │ (LRU更新)       │     │  (CleanExpired) │
└─────────────────┘     └─────────────────┘     └─────────────────┘
        │                       │                       │
        │                       │                       │
   addHeapChan             accessChan              定时触发
        │                       │                       │
        ▼                       ▼                       ▼
   expiredHeap            移动到链表头部          清理过期条目
```

---

*报告生成时间: 2026-03-20*
*分析版本: SmartDNSSort cache module*
