# 快速失败机制 - 快速参考

## 什么是快速失败？

当一个 IP 的**第一次探测就超时**时，系统会立即判定该 IP 为"坏 IP"，**取消后续探测**，而不是继续等待。

## 为什么需要快速失败？

### 问题
- 如果 `count=3`，第一次超时（800ms）后，系统还要再等 2 次（共 2400ms）
- 根据阶梯评分逻辑，第一次就超时的 IP 几乎不可能排到前面
- 继续探测是浪费时间和资源

### 解决
- 第一次超时 → 直接返回失败
- 节省 66% 的时间（2400ms → 800ms）
- 更快地排除"坏 IP"

## 关键数据

| 指标 | 值 |
|------|-----|
| 快速失败权重惩罚 | 500ms/次 |
| 普通失效权重惩罚 | 50ms/次 |
| 惩罚比例 | 10:1 |
| 时间节省 | 66% |

## 代码改动

### 1. 新增字段
```go
type IPFailureRecord struct {
    // ...
    FastFailCount int `json:"fast_fail_count"` // 快速失败次数
}
```

### 2. 新增方法
```go
// 记录快速失败
func (m *IPFailureWeightManager) RecordFastFail(ip string)

// 在 Pinger 中调用
func (p *Pinger) RecordIPFastFail(ip string)
```

### 3. 探测逻辑
```go
// 第一次探测失败 → 快速失败
if i == 0 && rtt < 0 {
    p.RecordIPFastFail(ip)
    return &Result{IP: ip, RTT: 999999, Loss: 100, ProbeMethod: "none"}
}
```

## 权重计算

```
权重 = FastFailCount * 500 + FailureCount * 50 + 时间衰减
```

### 示例
- 1 次快速失败：550ms（500 + 50）
- 10 次普通失效：500ms（10 × 50）
- 快速失败的 IP 排序更靠后

## 监控

```go
record := pinger.GetIPFailureRecord("1.2.3.4")

// 查看快速失败次数
fmt.Println(record.FastFailCount)

// 查看权重值
weight := pinger.failureWeightMgr.GetWeight("1.2.3.4")
fmt.Println(weight)
```

## 测试

运行所有测试：
```bash
go test -v ./ping
```

运行快速失败相关测试：
```bash
go test -v -run TestFastFail ./ping
```

## 文件清单

| 文件 | 说明 |
|------|------|
| `ip_failure_weight.go` | 核心逻辑：RecordFastFail、GetWeight |
| `ping_test_methods.go` | 探测逻辑：pingIP 中的快速失败 |
| `ping.go` | 公共接口：RecordIPFastFail |
| `fast_fail_test.go` | 测试用例（6 个） |
| `FAST_FAIL_MECHANISM.md` | 详细设计文档 |
| `IMPLEMENTATION_SUMMARY.md` | 实现总结 |

## 向后兼容性

✅ 完全向后兼容
- 新字段默认为 0
- 旧数据可直接加载
- 无需迁移

## 常见问题

**Q: 快速失败会不会误判？**
A: 不会。只有第一次探测彻底超时才会触发，这表示 IP 确实有严重问题。

**Q: 快速失败的 IP 能恢复吗？**
A: 能。权重会随时间衰减（7 天周期），连续成功 3 次也会降低失效计数。

**Q: 如何禁用快速失败？**
A: 快速失败是内置机制，无法禁用。但可以通过增加 `count` 值来减少其影响。

**Q: 快速失败对排序有什么影响？**
A: 快速失败的 IP 会被大幅降权（+500ms），排序时会排在后面。
