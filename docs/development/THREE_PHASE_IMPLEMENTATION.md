# SmartDNSSort 三阶段 DNS 查询优化方案 - 实现文档

## 概述

本文档说明了 SmartDNSSort DNS 查询优化系统的完整实现，核心目标是通过三个阶段的缓存和排序策略，提升 DNS 查询响应速度与连接质量，同时兼顾缓存利用率与数据新鲜度。

## 系统设计

### 1. 配置参数

**新增配置项 (`config.yaml`)**
```yaml
cache:
  # 首次查询或过期缓存返回时使用的 TTL（快速响应）
  fast_response_ttl: 60      # 单位：秒，默认值：60
  # 缓存最小 TTL
  min_ttl_seconds: 3600
  # 缓存最大 TTL
  max_ttl_seconds: 84600
```

**参数说明**
- `fast_response_ttl`: 用于快速返回响应，避免用户等待。在首次查询和缓存过期后再访问时使用此值。
- `min_ttl_seconds` 和 `max_ttl_seconds`: 用于限制排序后缓存的有效期范围。

### 2. 双层缓存架构

在 `cache/cache.go` 中实现了三层缓存管理体系：

#### 第一层：原始缓存 (`rawCache`)
- **功能**: 存储上游 DNS 服务器的原始响应
- **类型**: `map[string]*RawCacheEntry`
- **数据结构**:
  ```go
  type RawCacheEntry struct {
      IPs       []string  // 原始 IP 列表
      TTL       uint32    // 上游 DNS 返回的 TTL
      Timestamp time.Time // 缓存时间
  }
  ```
- **用途**: 在缓存过期时，快速返回旧数据而不需要重新查询上游

#### 第二层：排序缓存 (`sortedCache`)
- **功能**: 存储排序后的 IP 列表和 RTT 信息
- **类型**: `map[string]*SortedCacheEntry`
- **数据结构**:
  ```go
  type SortedCacheEntry struct {
      IPs       []string  // 排序后的 IP 列表
      RTTs      []int     // 对应的 RTT（毫秒）
      Timestamp time.Time // 排序完成时间
      TTL       int       // TTL（秒）
      IsValid   bool      // 排序是否有效
  }
  ```
- **用途**: 返回已排序的最优 IP 列表，提升连接质量

#### 第三层：排序队列状态 (`sortingState`)
- **功能**: 追踪当前正在进行的排序任务
- **防止重复排序**: 同一域名的并发请求只会触发一次排序
- **数据结构**:
  ```go
  type SortingState struct {
      InProgress bool                // 是否正在排序
      Done       chan struct{}        // 排序完成信号
      Result     *SortedCacheEntry    // 排序结果
      Error      error                // 排序错误
  }
  ```

### 3. 异步排序队列 (`cache/sortqueue.go`)

**功能**: 管理后台 IP 排序任务的执行

**核心特性**:
- **并发控制**: 支持配置工作线程数（默认 4 个）
- **队列管理**: 任务缓冲大小 200，避免内存溢出
- **超时控制**: 单个排序任务超时时间 10 秒
- **事件驱动**: 排序完成后通过回调函数通知
- **统计信息**: 追踪已处理和失败的任务数量

**数据结构**:
```go
type SortTask struct {
    Domain   string                      // 域名
    Qtype    uint16                      // DNS 查询类型
    IPs      []string                    // 待排序的 IP 列表
    TTL      uint32                      // 上游 DNS 的原始 TTL
    Callback func(*SortedCacheEntry, error) // 完成回调
}
```

### 4. 三阶段查询流程

#### 阶段一：首次查询（无缓存）

**触发条件**: 域名首次被请求，缓存中不存在

**流程**:
```
用户查询 -> 检查排序缓存(未命中) -> 检查原始缓存(未命中) -> 
向上游DNS查询 -> 设置原始缓存(使用上游TTL) ->
返回响应(使用fast_response_ttl=60s) -> 异步启动排序任务
```

**响应特性**:
- 使用 `fast_response_ttl` 快速返回（通常 60 秒）
- 异步进行 IP 排序，不阻塞用户
- 排序完成后自动更新排序缓存

**代码实现** (`dnsserver/server.go`):
```go
// 查询上游 DNS
result, err := s.upstream.QueryAll(ctx, domain)
// 缓存原始响应
s.cache.SetRaw(domain, question.Qtype, ips, upstreamTTL)
// 快速返回
fastTTL := uint32(s.cfg.Cache.FastResponseTTL)
s.buildDNSResponse(msg, domain, ips, question.Qtype, fastTTL)
w.WriteMsg(msg)
// 异步排序
go s.sortIPsAsync(domain, question.Qtype, ips, upstreamTTL)
```

#### 阶段二：排序完成后缓存命中

**触发条件**: 排序任务已完成，缓存仍然有效

**流程**:
```
用户查询 -> 检查排序缓存(命中) -> 
验证TTL -> 返回排序后的IP列表 + 原始TTL
```

**响应特性**:
- 优先返回排序后的 IP（最优连接路径）
- 使用排序缓存的原始 TTL（通常较长）
- 提升用户连接质量

**代码实现**:
```go
if sorted, ok := s.cache.GetSorted(domain, question.Qtype); ok {
    // 计算剩余 TTL
    elapsedSeconds := int(time.Since(sorted.Timestamp).Seconds())
    remainingTTL := sorted.TTL - elapsedSeconds
    // 返回排序后的 IP
    s.buildDNSResponse(msg, domain, sorted.IPs, question.Qtype, uint32(remainingTTL))
}
```

#### 阶段三：缓存过期后再次访问

**触发条件**: 排序缓存已过期，但原始缓存仍有效

**流程**:
```
用户查询 -> 检查排序缓存(已过期) -> 检查原始缓存(命中) ->
返回旧数据(TTL=fast_response_ttl=60s) -> 
异步刷新缓存(重新查询+排序)
```

**响应特性**:
- 立即返回旧缓存（避免用户等待）
- 设置较短 TTL（60 秒），促使客户端快速刷新
- 后台异步更新缓存和排序结果
- 下次查询时获得最新的排序结果

**代码实现**:
```go
if raw, ok := s.cache.GetRaw(domain, question.Qtype); ok {
    // 立即返回旧缓存，使用 fast_response_ttl
    fastTTL := uint32(s.cfg.Cache.FastResponseTTL)
    s.buildDNSResponse(msg, domain, raw.IPs, question.Qtype, fastTTL)
    w.WriteMsg(msg)
    // 异步重新查询和排序
    go s.refreshCacheAsync(domain, question.Qtype)
}
```

## 并发控制与线程安全

### 缓存同步机制

**RWMutex 读写锁** (`cache.mu`)
- 多个查询可以并发读取
- 排序结果写入时独占锁

**原子操作** (Atomic)
- 统计计数器使用 `sync/atomic` 避免锁竞争
- 例如: `atomic.AddInt64(&c.hits, 1)`

### 排序任务去重

**排序状态机制** (`sortingState` 映射)
- 首次排序时创建状态，设置 `InProgress=true`
- 并发请求检测到状态已存在则直接返回（不创建重复排序）
- 排序完成后通过 `Done` channel 通知所有等待者

**代码示例**:
```go
// 获取或创建排序状态
_, isNew := s.cache.GetOrStartSort(domain, qtype)
if !isNew {
    // 排序任务已在进行，跳过
    return
}
// 创建新排序任务
```

## 性能优化

### 1. 快速响应机制
- **阶段一**: 60 秒快速响应，不阻塞排序
- **阶段三**: 即使过期也立即返回旧数据，后台异步更新

### 2. 并发排序
- 排序队列使用 4 个工作线程
- 支持 200 个待排序域名的缓冲队列
- 单个排序任务超时 10 秒，防止无限等待

### 3. 缓存分层
- **原始缓存**: 保留较长时间（取决于上游 TTL）
- **排序缓存**: 独立 TTL 控制（`min_ttl_seconds` 到 `max_ttl_seconds`）
- **快速回退**: 排序缓存失效时自动回退到原始缓存

### 4. 内存管理
- 定期清理过期缓存（每 `min_ttl_seconds` 执行一次）
- 排序状态自动清理（完成后可删除）
- 队列大小限制防止内存溢出

## 测试验证

### 单元测试覆盖 (`cache/cache_three_phase_test.go`)

1. **TestThreePhaseCache**: 验证三阶段逻辑
   - 首次查询无缓存
   - 排序完成后缓存命中
   - 缓存过期后的处理

2. **TestSortingState**: 排序状态管理
   - 创建排序状态
   - 完成信号机制
   - 状态清理

3. **TestConcurrentCacheAccess**: 并发安全性
   - 多线程并发读写
   - 缓存一致性

4. **TestCacheExpiry**: 过期检测
   - TTL 倒计时
   - 过期判断

5. **TestCleanExpired**: 过期清理
   - 自动删除过期项
   - 保留有效项

6. **TestRawCacheLayer**: 原始缓存层
   - 设置和获取原始缓存
   - 与排序缓存的优先级

### 运行测试

```bash
cd d:\gb\SmartDNSSort
go test -v ./cache

# 输出示例
# === PASS: TestThreePhaseCache (0.00s)
#     --- PASS: TestThreePhaseCache/Phase1-FirstQuery
#     --- PASS: TestThreePhaseCache/Phase2-SortedCacheHit
#     --- PASS: TestThreePhaseCache/Phase3-ExpiredCacheRefresh
# === PASS: TestSortingState
# === PASS: TestConcurrentCacheAccess
# === PASS: TestCacheExpiry (1.10s)
# === PASS: TestCleanExpired (1.10s)
# === PASS: TestRawCacheLayer
# PASS
```

## 关键改动总结

### 文件修改

1. **config.yaml** 和 **config/config.go**
   - 新增 `fast_response_ttl` 参数（默认 60 秒）

2. **cache/cache.go** (重大重构)
   - 实现双层缓存结构（原始 + 排序）
   - 添加排序状态管理
   - 使用原子操作替代锁

3. **cache/sortqueue.go** (新文件)
   - 异步排序任务队列
   - 并发工作线程管理
   - 排序完成回调机制

4. **dnsserver/server.go** (核心逻辑改写)
   - 实现三阶段查询流程
   - 异步排序启动和回调处理
   - 缓存过期后的异步刷新

## 使用示例

### 启动服务器

```bash
SmartDNSSort -c config.yaml
```

### 查询流程示例

#### 首次查询 (example.com)
```
时间 t=0:   客户端查询 -> DNS 返回原始 IP（TTL=60s）+ 启动排序
时间 t=5:   排序完成 -> 排序缓存更新（TTL=3600s）
时间 t=10:  再次查询 -> DNS 返回排序后的 IP（TTL=3590s）
```

#### 缓存过期后查询
```
时间 t=3700: 排序缓存过期 + 原始缓存有效
时间 t=3700: 客户端查询 -> DNS 返回原始 IP（TTL=60s）+ 启动异步刷新
时间 t=3705: 刷新完成 -> 新排序缓存设置
时间 t=3710: 再次查询 -> DNS 返回新排序的 IP（TTL=3595s）
```

## 故障排查

### 排序队列满

**症状**: 日志中出现 "sort queue full"

**原因**: 排序任务过多，队列缓冲已满

**解决**: 
1. 增加工作线程数: `cache.NewSortQueue(8, 200, 10*time.Second)`
2. 增加队列大小: `cache.NewSortQueue(4, 500, 10*time.Second)`
3. 减少排序超时: `cache.NewSortQueue(4, 200, 5*time.Second)`

### 排序超时

**症状**: 日志中出现 "sort operation timeout"

**原因**: Ping 操作超时或网络延迟

**解决**: 调整 Ping 参数或排序超时

## 未来优化方向

1. **动态 TTL 调整**: 根据查询频率动态调整 `fast_response_ttl`
2. **排序结果缓存**: 对排序结果进行增量更新
3. **预测性预排序**: 预测热门域名并提前排序
4. **分布式缓存**: 支持跨节点缓存共享
5. **智能 TTL**: 根据上游 DNS 特性动态调整 min/max TTL

---

**文档版本**: 1.0  
**最后更新**: 2025-11-15  
**作者**: SmartDNSSort 开发团队
