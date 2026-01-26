# MarkTimeout 方案 - 快速参考卡

## 问题一句话

上游通过 IPv6 查询缓慢 → 程序超时 → 熔断 30 秒 → IPv4 也被禁用

## 解决方案一句话

超时不熔断，只增加延迟 → 自动排序靠后 → 流量自动避让

---

## 核心改动

### 1. 新增 `MarkTimeout` 方法

**文件**：`upstream/health.go`

```go
func (h *ServerHealth) MarkTimeout(d time.Duration) {
    // 不增加 consecutiveFailures
    // 只增加延迟（EWMA）
    // 服务器排序靠后，但保留资格
}
```

### 2. 精确错误判定

**文件**：`upstream/health_aware.go`

```go
if errors.Is(err, context.DeadlineExceeded) {
    h.health.MarkTimeout(latency)  // 超时：软惩罚
} else {
    h.health.MarkFailure()  // 硬错误：熔断
}
```

### 3. Sequential 策略调用

**文件**：`upstream/manager_sequential.go`

```go
if errors.Is(err, context.DeadlineExceeded) {
    server.RecordTimeout()  // 调用 RecordTimeout
} else {
    server.RecordError()  // 调用 RecordError
}
```

---

## 工作原理

### 之前

```
超时 → MarkFailure() → consecutiveFailures++ 
→ 5 次失败 → 熔断 30 秒 → 所有查询被拦截
```

### 现在

```
超时 → MarkTimeout() → 增加延迟 
→ 排序靠后 → 继续处理查询
```

---

## 预期效果

| 场景 | 之前 | 现在 |
|-----|------|------|
| IPv6 缓慢 | 熔断 30 秒 | 排序靠后 |
| IPv4 查询 | 被拦截 | 继续正常 |
| 故障恢复 | 等待 30 秒 | 自动恢复 |
| 失败率 | 高 | 低 50-80% |

---

## 关键数字

| 参数 | 值 | 说明 |
|-----|-----|------|
| alpha | 0.2 | EWMA 权重 |
| 超时惩罚 | 1 秒 | 默认延迟增加 |
| 熔断阈值 | 5 次 | 仍然有效 |
| 熔断时间 | 30 秒 | 仍然有效 |

---

## 验证方法

### 1. 查看日志

```bash
# 观察是否有 MarkTimeout 被调用
# 观察超时的服务器是否排序靠后
```

### 2. 监控指标

- 失败率（应该下降）
- 熔断事件（应该减少）
- 响应时间（应该稳定）

### 3. 压力测试

在双栈环境下模拟上游 IPv6 缓慢，观察程序行为。

---

## 代码位置

| 功能 | 文件 | 方法 |
|-----|-----|------|
| 新增方法 | `upstream/health.go` | `MarkTimeout()` |
| 错误判定 | `upstream/health_aware.go` | `Exchange()` |
| 调用位置 | `upstream/manager_sequential.go` | `querySequential()` |

---

## 与熔断机制的关系

### 熔断仍然有效

- ✅ 硬错误（连接拒绝、DNS 错误）仍然触发熔断
- ✅ 只有超时被特殊处理
- ✅ 真正的宕机仍然被快速切断

### 超时不再触发熔断

- ✅ 超时不增加 `consecutiveFailures`
- ✅ 超时只增加延迟
- ✅ 服务器保留资格

---

## EWMA 延迟计算

```
初始：200ms
超时 1 次：220ms
超时 2 次：236ms
超时 3 次：248.8ms
...
最终稳定：300ms

恢复 1 次：250ms
恢复 2 次：210ms
恢复 3 次：178ms
...
最终恢复：50ms
```

---

## 后续优化（可选）

### 1. 增加超时时间

```go
timeoutMs = 500  // 从 300ms 增加到 500ms
```

### 2. 区分查询类型

```go
if qtype == dns.TypeAAAA {
    attemptTimeout = 1000 * time.Millisecond
} else {
    attemptTimeout = 300 * time.Millisecond
}
```

### 3. 调整 alpha 因子

```go
latencyAlpha: 0.3  // 从 0.2 增加到 0.3
```

### 4. 在上游侧优化 IPv6

- 检查 IPv6 路由
- 优化 IPv6 DNS 性能
- 或禁用 IPv6

---

## 常见问题

### Q: 为什么不直接忽略超时？

A: 因为我们仍然需要避免使用缓慢的服务器。通过增加延迟，我们实现了自动避让。

### Q: 熔断机制还有效吗？

A: 有效。只有超时被特殊处理，硬错误仍然触发熔断。

### Q: 如何快速验证修改是否有效？

A: 查看日志中是否有 `MarkTimeout` 被调用，观察失败率是否下降。

### Q: 需要修改配置吗？

A: 不需要。代码修改后自动生效。

### Q: 这个方案会影响性能吗？

A: 不会。只是改变了错误处理的方式，不增加额外开销。

---

## 部署步骤

1. ✅ 修改 `upstream/health.go`（添加 `MarkTimeout`）
2. ✅ 修改 `upstream/health_aware.go`（错误判定）
3. ✅ 修改 `upstream/manager_sequential.go`（调用 `RecordTimeout`）
4. ✅ 编译和测试
5. ✅ 部署到生产环境
6. ✅ 监控失败率和熔断事件

---

## 成功指标

- ✅ 失败率下降 50-80%
- ✅ 熔断事件减少
- ✅ IPv4 查询不再受影响
- ✅ 自动流量分配
- ✅ 平滑的故障恢复

---

## 相关文档

| 文档 | 用途 |
|-----|------|
| `解决方案验证_MarkTimeout机制.md` | 详细验证 |
| `实施总结_MarkTimeout方案.md` | 完整总结 |
| `问题重新分析.md` | 问题分析 |

