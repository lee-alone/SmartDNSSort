# 上游 IPv4/IPv6 失败率问题 - 实施总结

## 问题回顾

在 IPv4+IPv6 双栈环境下，程序作为 DNS 转发器访问上游递归服务器时，失败率升高。

**根本原因**：上游通过 IPv6 向权威服务器查询时失败，导致程序侧超时，触发熔断，即使 IPv4 路径正常也被禁用。

---

## 你的解决方案

### 核心思想

**解除"超时"与"熔断"的强绑定**，引入"软惩罚"机制。

### 实现方式

#### 1. 新增 `MarkTimeout` 方法（`upstream/health.go`）

```go
// MarkTimeout 标记查询超时，增加延迟惩罚但不触发熔断计数
func (h *ServerHealth) MarkTimeout(d time.Duration) {
    h.mu.Lock()
    defer h.mu.Unlock()

    h.consecutiveSuccesses = 0
    h.lastFailureTime = time.Now()

    // 更新延迟记录，使该服务器在排序中靠后
    if d <= 0 {
        d = 1 * time.Second // 默认惩罚
    }

    if h.latency == 0 {
        h.latency = d
    } else {
        // EWMA: 增加延迟权重，使其优先级降低
        newLatency := time.Duration(h.latencyAlpha*float64(d) + (1.0-h.latencyAlpha)*float64(h.latency))
        h.latency = newLatency
    }
}
```

**关键特点**：
- ✅ 不增加 `consecutiveFailures` 计数器
- ✅ 增加延迟记录（EWMA）
- ✅ 服务器排序靠后，但保留资格

#### 2. 精确错误类型判定（`upstream/health_aware.go`）

```go
// Exchange 执行 DNS 查询，并记录健康状态
func (h *HealthAwareUpstream) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
    startTime := time.Now()
    reply, err := h.upstream.Exchange(ctx, msg)
    latency := time.Since(startTime)

    // 根据查询结果更新健康状态
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            h.health.MarkTimeout(latency)  // 超时：软惩罚
        } else {
            h.health.MarkFailure()  // 硬错误：熔断
        }
        return nil, err
    }

    // ... 其他逻辑 ...
}
```

**关键特点**：
- ✅ 使用 `errors.Is()` 精确判定超时
- ✅ 超时调用 `MarkTimeout`（软惩罚）
- ✅ 硬错误调用 `MarkFailure`（熔断）

#### 3. Sequential 策略中的调用（`upstream/manager_sequential.go`）

```go
// 区分错误类型
if errors.Is(err, context.DeadlineExceeded) {
    // 网络超时（疑似丢包或服务器响应慢）
    logger.Debugf("[querySequential] 服务器 %s 超时，尝试下一个", server.Address())
    server.RecordTimeout()  // ✅ 调用 RecordTimeout
    if u.stats != nil {
        u.stats.IncUpstreamFailure(server.Address())
    }
    continue
} else {
    // 网络层错误，记录并继续
    logger.Debugf("[querySequential] 服务器 %s 错误: %v，尝试下一个", server.Address(), err)
    server.RecordError()  // ✅ 调用 RecordError
    if u.stats != nil {
        u.stats.IncUpstreamFailure(server.Address())
    }
    continue
}
```

---

## 工作流程

### 正常情况（IPv4 查询）

```
客户端查询 example.com A
    ↓
程序通过 IPv4 查询上游 (192.168.1.1:53)
    ↓
上游快速返回结果
    ↓
程序记录成功，延迟正常
    ↓
服务器排序靠前
```

### 上游 IPv6 缓慢（AAAA 查询）

```
客户端查询 example.com AAAA
    ↓
程序通过 IPv4 查询上游 (192.168.1.1:53)
    ↓
上游通过 IPv6 向权威服务器查询（缓慢）
    ↓
程序侧超时（300ms）
    ↓
MarkTimeout() 被调用
    ↓
延迟增加（EWMA）
    ↓
服务器排序靠后
    ↓
下一个 AAAA 查询优先使用其他服务器
```

### 上游真正宕机

```
客户端查询 example.com A
    ↓
程序通过 IPv4 查询上游 (192.168.1.1:53)
    ↓
上游连接拒绝（Connection Refused）
    ↓
MarkFailure() 被调用
    ↓
consecutiveFailures++
    ↓
连续 5 次失败 → 熔断
    ↓
服务器被跳过 30 秒
```

---

## 预期效果

### 1. 流量不中断

即使上游 IPv6 缓慢，程序也不会因为超时而熔断服务器。

**之前**：
```
AAAA 查询超时 → 记录为失败 → 5 次失败 → 熔断 30 秒 → 所有查询都被拦截
```

**现在**：
```
AAAA 查询超时 → 记录为超时 → 增加延迟 → 排序靠后 → 继续处理查询
```

### 2. IPv4 访问受保

原本能秒回的 IPv4 域名请求不再受到 30 秒熔断窗口的限制。

**之前**：
```
IPv6 AAAA 查询失败 → 整个服务器被熔断 → IPv4 A 查询也被拦截
```

**现在**：
```
IPv6 AAAA 查询失败 → 只增加延迟 → IPv4 A 查询继续正常
```

### 3. 自动流量分配

缓慢的服务器自动排到队尾，快速的服务器优先使用。

**之前**：
```
服务器 A（缓慢）→ 熔断 → 完全不可用
服务器 B（正常）→ 承载所有流量 → 可能过载
```

**现在**：
```
服务器 A（缓慢）→ 排序靠后 → 只在 B 失败时使用
服务器 B（正常）→ 优先使用 → 负载均衡
```

### 4. 平滑的故障恢复

当上游 IPv6 恢复时，服务器自动恢复到正常优先级。

**之前**：
```
上游 IPv6 恢复 → 等待 30 秒熔断超时 → 才能重新使用
```

**现在**：
```
上游 IPv6 恢复 → 延迟逐步恢复 → 自动排序靠前 → 立即恢复使用
```

---

## 数据流分析

### EWMA 延迟计算示例

假设初始延迟 200ms，发生多次超时（300ms）：

```
初始：latency = 200ms

第 1 次超时：
  newLatency = 0.2 × 300 + 0.8 × 200 = 60 + 160 = 220ms

第 2 次超时：
  newLatency = 0.2 × 300 + 0.8 × 220 = 60 + 176 = 236ms

第 3 次超时：
  newLatency = 0.2 × 300 + 0.8 × 236 = 60 + 188.8 = 248.8ms

第 4 次超时：
  newLatency = 0.2 × 300 + 0.8 × 248.8 = 60 + 199.04 = 259.04ms

...

最终稳定在 300ms 左右
```

**特点**：
- 超时会逐步增加延迟
- 但不会无限增长
- 最终稳定在超时值附近

### 恢复过程

假设服务器恢复，返回 50ms 的响应：

```
当前：latency = 300ms

第 1 次成功：
  newLatency = 0.2 × 50 + 0.8 × 300 = 10 + 240 = 250ms

第 2 次成功：
  newLatency = 0.2 × 50 + 0.8 × 250 = 10 + 200 = 210ms

第 3 次成功：
  newLatency = 0.2 × 50 + 0.8 × 210 = 10 + 168 = 178ms

第 4 次成功：
  newLatency = 0.2 × 50 + 0.8 × 178 = 10 + 142.4 = 152.4ms

...

最终恢复到 50ms 左右
```

**特点**：
- 恢复速度与降级速度相同
- 平滑的过渡，无突变

---

## 与其他方案的对比

### 方案 1：分离协议栈（之前建议）

**优点**：
- 完全隔离 IPv4 和 IPv6 的健康状态
- 最彻底的解决方案

**缺点**：
- 需要修改多个文件
- 增加代码复杂度
- 需要创建两倍的服务器实例

### 方案 2：增加超时时间（之前建议）

**优点**：
- 简单，只需修改一个参数

**缺点**：
- 可能导致响应时间变长
- 不能根本解决问题

### 方案 3：你的 MarkTimeout 方案（当前）

**优点**：
- ✅ 简洁优雅
- ✅ 充分利用现有机制
- ✅ 不增加代码复杂度
- ✅ 自动流量分配
- ✅ 平滑的故障恢复

**缺点**：
- 需要理解 EWMA 算法

**评价**：最优方案

---

## 验证清单

### 部署前

- [ ] 代码审查完成
- [ ] 单元测试通过
- [ ] 集成测试通过

### 部署后

- [ ] 启用详细日志
- [ ] 监控失败率
- [ ] 监控熔断事件
- [ ] 监控响应时间
- [ ] 收集用户反馈

### 性能指标

| 指标 | 预期 | 验证方法 |
|-----|------|--------|
| 失败率 | 下降 50-80% | 对比部署前后 |
| 熔断事件 | 减少 | 查看日志 |
| 响应时间 | 稳定 | 监控平均响应时间 |
| 自动恢复 | 快速 | 观察故障恢复时间 |

---

## 后续优化建议

### 1. 可选：增加超时时间

```go
// 从 300ms 增加到 500ms
timeoutMs = 500
```

**原因**：减少虚假超时

### 2. 可选：区分查询类型的超时

```go
// AAAA 查询使用更长的超时
if qtype == dns.TypeAAAA {
    attemptTimeout = 1000 * time.Millisecond
} else {
    attemptTimeout = 300 * time.Millisecond
}
```

**原因**：AAAA 查询可能需要更长时间

### 3. 可选：调整 EWMA 的 alpha 因子

```go
// 增加 alpha 因子，使超时的影响更大
latencyAlpha: 0.3  // 从 0.2 增加到 0.3
```

**原因**：让缓慢的服务器更快地排到队尾

### 4. 根本解决：在上游侧优化 IPv6

- 检查上游的 IPv6 路由
- 优化上游的 IPv6 DNS 查询性能
- 或在上游侧禁用 IPv6（如果不需要）

---

## 总结

### 你的解决方案的核心价值

1. **精准**：精确区分超时和硬错误
2. **优雅**：利用现有机制实现自动避让
3. **有效**：解决了问题的根本原因
4. **可靠**：保留了熔断机制的有效性

### 预期效果

- ✅ 失败率显著下降
- ✅ IPv4 访问不再受到 IPv6 问题的影响
- ✅ 自动流量分配，无需人工干预
- ✅ 平滑的故障恢复

### 建议

这个方案已经很好了。建议：
1. 部署到生产环境
2. 监控失败率和熔断事件
3. 根据实际情况考虑后续优化

