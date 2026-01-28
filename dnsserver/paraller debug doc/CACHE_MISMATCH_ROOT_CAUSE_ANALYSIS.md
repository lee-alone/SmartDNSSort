# 域名和IP池不匹配问题 - 根本原因分析

## 问题现象
- 缓存少时查询正常
- 查询多了以后，某些域名出现 **域名和IP池不匹配**
- 导致网页访问提示证书错误
- 清空缓存后恢复正常

## 根本原因

### 1. 并行查询的两阶段机制导致缓存不一致

系统使用**二阶段分层步进式并行查询**：

```
第一阶段（Active Tier）：快速返回
  ├─ 并发查询最优的N个服务器（默认2个）
  └─ 返回第一个成功响应 → 立即返回给客户端 + 缓存

第二阶段（Staggered Tier）：后台补全
  ├─ 按批次启动剩余服务器
  ├─ 收集所有响应
  └─ 合并去重后 → 通过cacheUpdateCallback更新缓存
```

### 2. 缓存更新的竞态条件

**时间序列：**

```
T1: 客户端查询 example.com
    ↓
T2: 第一阶段返回 IP池 = [1.1.1.1, 2.2.2.2]
    ├─ 立即返回给客户端
    ├─ 缓存到 rawCache
    └─ 触发排序任务 → sortedCache = [1.1.1.1, 2.2.2.2]（排序后）
    
T3: 浏览器使用 1.1.1.1 建立连接（证书绑定到1.1.1.1）

T4: 后台补全完成（第二阶段收集所有响应）
    ├─ 发现更多IP = [1.1.1.1, 2.2.2.2, 3.3.3.3, 4.4.4.4]
    ├─ 调用 cacheUpdateCallback
    ├─ 更新 rawCache = [1.1.1.1, 2.2.2.2, 3.3.3.3, 4.4.4.4]
    ├─ 清除旧排序状态（CancelSort）
    └─ 触发新排序任务 → sortedCache = [3.3.3.3, 1.1.1.1, 2.2.2.2, 4.4.4.4]（新排序）

T5: 浏览器第二次查询 example.com（DNS缓存过期或新标签页）
    ├─ 获取 sortedCache = [3.3.3.3, ...]
    ├─ 返回 3.3.3.3 作为首选IP
    └─ 浏览器尝试连接 3.3.3.3 → 证书错误！（证书是1.1.1.1的）
```

### 3. 问题的关键点

1. **缓存键设计缺陷**：
   - 缓存键 = `domain#qtype`（如 `example.com#1`）
   - 不包含IP池版本信息
   - 同一domain的不同IP池版本会相互覆盖

2. **排序状态管理不当**：
   - `CancelSort` 清除旧排序后，新排序可能改变IP顺序
   - 但客户端已经建立的连接仍使用旧IP
   - 导致IP池和实际连接的IP不匹配

3. **并发更新的时序问题**：
   - 第一阶段快速返回 → 客户端立即使用
   - 第二阶段后台更新 → 覆盖缓存
   - 中间没有同步机制

4. **高并发下问题加剧**：
   - 查询少时：后台补全快速完成，IP池变化不大
   - 查询多时：多个域名的后台补全并发进行，缓存频繁更新
   - 导致IP池变化频繁，客户端连接和缓存不同步

## 具体代码流程

### 第一阶段：快速返回
```go
// upstream/manager_parallel.go:queryParallel()
fastResponse := <-fastResponseChan  // 获取第一个成功响应
// 立即返回给客户端
return &QueryResultWithTTL{
    IPs: fastResponse.IPs,  // [1.1.1.1, 2.2.2.2]
    ...
}

// dnsserver/handler_query.go:handleCacheMiss()
s.cache.SetRaw(domain, qtype, result.IPs, ...)  // 缓存第一阶段结果
s.sortIPsAsync(domain, qtype, result.IPs, ...)  // 排序第一阶段IP
```

### 第二阶段：后台补全
```go
// upstream/manager_parallel.go:collectRemainingResponses()
mergedRecords := u.mergeAndDeduplicateRecords(allSuccessResults)
u.cacheUpdateCallback(domain, qtype, mergedRecords, ...)  // 更新缓存

// dnsserver/server_callbacks.go:setupUpstreamCallback()
s.cache.SetRawRecords(domain, qtype, records, ...)  // 覆盖rawCache
if newIPCount > oldIPCount {
    s.cache.CancelSort(domain, qtype)  // 清除旧排序
    // 触发新排序 → IP顺序可能改变
}
```

## 为什么清空缓存后恢复正常

1. 清空缓存 → 所有缓存项被删除
2. 下次查询 → 从头开始
3. 第一阶段返回 + 第二阶段补全 → 同时进行
4. 由于缓存为空，不存在"旧IP池"和"新IP池"的冲突
5. 最终只有一个完整的IP池版本

## 影响范围

- **高并发场景**：多个域名同时查询，后台补全频繁
- **多上游服务器**：第一阶段只查询部分服务器，第二阶段补全差异大
- **IP池变化频繁**：某些域名的IP池经常变化
- **长连接应用**：浏览器保持连接，DNS缓存过期时获取新IP

## 解决方案

### 方案A：禁用后台补全的缓存更新（快速修复）
- 第一阶段返回的IP池作为最终结果
- 后台补全仅用于统计，不更新缓存
- 优点：简单，避免缓存不一致
- 缺点：无法获得完整IP池

### 方案B：版本化缓存（推荐）
- 为每个IP池版本添加版本号
- 缓存键 = `domain#qtype#version`
- 客户端获取IP时同时获取版本号
- 下次查询时检查版本，版本不同则重新排序
- 优点：保留完整IP池，避免不一致
- 缺点：实现复杂

### 方案C：延迟后台补全（折中方案）
- 第一阶段返回后，等待一段时间（如100ms）
- 如果后台补全在此时间内完成，使用完整IP池
- 否则使用第一阶段IP池
- 优点：平衡完整性和一致性
- 缺点：增加延迟

### 方案D：IP池变化检测（推荐）
- 后台补全时，检查IP池是否有实质性变化
- 仅当IP池变化超过阈值时才更新缓存
- 否则保留第一阶段的IP池
- 优点：避免频繁更新，保持一致性
- 缺点：需要定义"实质性变化"的标准

## 建议实施步骤

1. **立即修复**：禁用后台补全的缓存更新（方案A）
2. **短期改进**：实现IP池变化检测（方案D）
3. **长期优化**：实现版本化缓存（方案B）
