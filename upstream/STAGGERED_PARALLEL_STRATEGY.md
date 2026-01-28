# 两阶段、带节奏的并行查询策略

## 概述

这是一个工业级的 DNS 并行查询优化方案，旨在在保证 IP 池完整性的同时，削减对上游服务器的瞬时并发压力。

## 核心设计

### 问题背景

传统的全并发查询方式存在以下问题：
- **流量尖峰**：所有上游服务器在同一微秒内收到请求，造成瞬时并发压力
- **资源浪费**：等待所有响应才能返回，用户体感延迟高
- **上游压力**：特别是在配置了大量上游服务器时，压力成倍增加

### 解决方案：两阶段、带节奏的并行

```
时间轴 ────────────────────────────────────────────────────────────────>

第一阶段（Active Tier）- 极速响应
├─ T=0ms:    启动 2 个最优服务器 (A, B)
├─ T=50ms:   A 返回成功 ✅ → 立即响应用户
└─ 目标：快速反馈，用户无感知延迟

第二阶段（Staggered Tier）- 节律补全
├─ T=300ms:  启动第一批 (C, D)
├─ T=350ms:  启动第二批 (E, F)
├─ T=400ms:  启动第三批 (G, H)
└─ 目标：平滑流量，完整收集所有 IP

总耗时：3 秒硬超时（确保后台补全不会无限期运行）
```

## 实现细节

### 1. 第一阶段：Active Tier（冲锋队）

**目标**：以最快速度获得第一个正确的响应

**执行流程**：
```go
// 选择最优的 N 个服务器（按健康度 + 延迟排序）
activeTierServers := sortedServers[:activeTierSize]  // 默认 2 个

// 立即并发启动
for _, server := range activeTierServers {
    go executeQuery(server)
}

// 等待第一个成功响应或 fallback 超时
select {
case fastResponse = <-fastResponseChan:
    // 立即返回给用户
case <-fallbackTimeout:
    // 启动第二阶段
}
```

**关键参数**：
- `activeTierSize`: 2（可配置）
- `fallbackTimeout`: 300ms（若第一阶段未响应，启动第二阶段的等待时间）

**优势**：
- 利用健康度和延迟信息，优先选择最可靠的服务器
- 快速失败：如果最优服务器都失败，快速降级到第二阶段
- 用户体感延迟最小化

### 2. 第二阶段：Staggered Tier（后备军）

**目标**：平滑地补全所有上游的 IP，不造成流量冲击

**执行流程**：
```go
// 将剩余服务器分组
remainingServers := sortedServers[activeTierSize:]
batches := splitIntoBatches(remainingServers, batchSize)  // 默认每批 2 个

// 分组步进启动
for i, batch := range batches {
    if i > 0 {
        time.Sleep(staggerDelay)  // 默认 50ms
    }
    for _, server := range batch {
        go executeQuery(server)
    }
}
```

**关键参数**：
- `batchSize`: 2（每批启动的服务器数量）
- `staggerDelay`: 50ms（批次间的延迟）
- `totalCollectTimeout`: 3s（后台收集的最大总时长）

**优势**：
- 将原本的尖峰并发平铺成平滑流量
- 对上游服务器压力友好
- 确保最终收集到所有上游的 IP

### 3. 智能降级机制

如果第一阶段全部失败（明确报错），系统会：
1. 立即启动第二阶段的第一批
2. 不再等待 fallback 超时
3. 加快补全速度

```go
// 第一阶段全失败时的快速降级
if fastResponse == nil {
    select {
    case fastResponse = <-fastResponseChan:
        // 第二阶段首个成功
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}
```

## 配置参数

### 默认配置

| 参数 | 默认值 | 说明 |
|------|-------|------|
| `activeTierSize` | 2 | 第一梯队并发数 |
| `fallbackTimeout` | 300ms | 第一梯队未响应时启动第二梯队的等待时间 |
| `batchSize` | 2 | 第二梯队每批次启动的数量 |
| `staggerDelay` | 50ms | 批次间的步进延迟 |
| `totalCollectTimeout` | 3s | 背景补全的最大总时长 |

### 调优建议

**场景 1：上游服务器较少（2-5 个）**
```go
activeTierSize: 2
batchSize: 1
staggerDelay: 100ms
totalCollectTimeout: 2s
```
理由：服务器少，不需要分组，可以更快地启动剩余服务器

**场景 2：上游服务器较多（10+ 个）**
```go
activeTierSize: 3
batchSize: 3
staggerDelay: 50ms
totalCollectTimeout: 5s
```
理由：服务器多，需要更细致的分组控制，给后台补全更多时间

**场景 3：对延迟敏感（如移动应用）**
```go
activeTierSize: 3
fallbackTimeout: 200ms
batchSize: 2
staggerDelay: 30ms
totalCollectTimeout: 2s
```
理由：更激进的第一阶段，更快的第二阶段启动

**场景 4：对完整性敏感（如缓存预热）**
```go
activeTierSize: 2
fallbackTimeout: 500ms
batchSize: 2
staggerDelay: 100ms
totalCollectTimeout: 5s
```
理由：给第一阶段更多时间，给后台补全更多时间

## 与 Singleflight 的协作

Singleflight 去重机制与两阶段并行完美配合：

```
请求 1: example.com (触发 Parallel 查询)
  ├─ 第一阶段：启动 2 个服务器
  ├─ 返回快速响应
  └─ 后台补全：启动分组步进

请求 2: example.com (同时到达)
  └─ Singleflight 拦截：共享请求 1 的结果
     （不会产生额外的并行查询）

请求 3: example.com (后台补全中)
  └─ Singleflight 拦截：等待请求 1 的后台补全完成
     （获得完整的 IP 池）
```

## 性能指标

### 典型场景（5 个上游服务器）

**传统全并发方式**：
```
T=0ms:    发起 5 个并发请求
T=50ms:   第一个响应返回 ✅
T=200ms:  所有响应收集完成
总耗时：200ms
上游瞬时并发：5
```

**两阶段方式**：
```
T=0ms:    发起 2 个 Active Tier 请求
T=50ms:   第一个响应返回 ✅（用户立即获得）
T=300ms:  启动第一批 Staggered Tier（2 个）
T=350ms:  启动第二批 Staggered Tier（1 个）
T=400ms:  所有响应收集完成
总耗时：400ms（但用户感知延迟仅 50ms）
上游瞬时并发：2 → 2 → 1（平滑递进）
```

**优势**：
- 用户体感延迟：50ms（vs 200ms）
- 上游瞬时并发：2（vs 5）
- 流量分布：平滑（vs 尖峰）

## 日志示例

```
[queryParallel] 两阶段并行查询 5 个服务器，查询 example.com (type=A)，Active Tier=2，Batch Size=2，Stagger Delay=50ms
[queryParallel] 分层: Active Tier=2 个服务器, Staggered Tier=3 个服务器
[queryParallel] 🚀 第一阶段: 启动 2 个 Active Tier 服务器
[executeQuery] 🚀 快速响应: 服务器 8.8.8.8:53 返回成功结果
[queryParallel] ✅ 第一阶段成功: 服务器 8.8.8.8:53 返回 2 个IP
[queryParallel] 📊 第二阶段: 启动分组步进，共 3 个服务器，批大小=2，步进延迟=50ms
[launchStaggeredTier] 批次 0: 启动 2 个服务器
[launchStaggeredTier] 批次 1: 启动 1 个服务器
[collectRemainingResponsesWithTimeout] 🔄 开始后台收集剩余响应: example.com (type=A)，总超时=3s
[collectRemainingResponsesWithTimeout] 服务器 1.1.1.1:53 查询成功(第2个成功),返回 2 条记录
[collectRemainingResponsesWithTimeout] 服务器 208.67.222.222:53 查询成功(第3个成功),返回 2 条记录
[collectRemainingResponsesWithTimeout] 服务器 9.9.9.9:53 查询成功(第4个成功),返回 2 条记录
[collectRemainingResponsesWithTimeout] ✅ 后台收集完成: 从 4 个服务器收集到 8 条记录
[collectRemainingResponsesWithTimeout] 📝 调用缓存更新回调，更新完整记录池到缓存
```

## 故障处理

### 场景 1：第一阶段全失败

```
T=0ms:    启动 Active Tier
T=300ms:  fallback 超时，启动第二阶段
T=350ms:  第二阶段首个成功 ✅
```

### 场景 2：后台补全超时

```
T=0ms:    启动 Active Tier
T=50ms:   返回快速响应
T=3000ms: 后台补全超时，停止收集
          已收集的 IP 更新到缓存
```

### 场景 3：所有服务器都失败

```
T=0ms:    启动 Active Tier
T=300ms:  启动第二阶段
T=3000ms: 总超时，返回错误
```

## 与其他策略的对比

| 策略 | 用户延迟 | 上游压力 | IP 完整性 | 适用场景 |
|------|---------|---------|---------|---------|
| Sequential | 高 | 低 | 低 | 上游不稳定 |
| Parallel (全并发) | 低 | 高 | 高 | 上游充足 |
| Racing | 低 | 中 | 中 | 平衡型 |
| **Staggered Parallel** | **低** | **低** | **高** | **最优平衡** |

## 总结

两阶段、带节奏的并行策略通过以下机制实现了最优平衡：

1. **快速响应**：第一阶段立即返回，用户无感知延迟
2. **完整性保证**：第二阶段确保所有上游都被查询
3. **压力削峰**：分组步进平滑流量，对上游友好
4. **智能降级**：失败快速降级，不浪费时间
5. **可配置性**：参数灵活调整，适应不同场景

这是一个**工业级的生产方案**，特别适合：
- 配置了多个上游服务器的场景
- 对用户体感延迟敏感的应用
- 需要保证 IP 池完整性的缓存系统
