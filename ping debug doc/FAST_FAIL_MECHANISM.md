# 快速失败（Fast-Fail）机制

## 问题背景

在原有的探测逻辑中，如果一个 IP 的第一次探测就超时了，系统依然会坚持完成剩下的 `count-1` 次探测。这导致：

- **无效等待时间浪费**：如果 `count=3`，第一次探测超时（比如 800ms），该 IP 已经背负了 2000ms 的分数
- **资源浪费**：继续探测一个明显"坏"的 IP 是没有意义的
- **排序不准确**：根据"阶梯评分"逻辑，第一次就超时的 IP 几乎不可能排到前面，继续探测只是浪费时间

## 解决方案

引入**快速失败（Fast-Fail）机制**：

**如果第一次探测彻底超时，直接判定该 IP 丢包严重，取消后续探测。**

### 核心改动

#### 1. 数据结构扩展（`ip_failure_weight.go`）

在 `IPFailureRecord` 中添加新字段：

```go
FastFailCount int `json:"fast_fail_count"` // 快速失败次数（第一次探测就超时）
```

#### 2. 快速失败记录方法（`ip_failure_weight.go`）

新增 `RecordFastFail()` 方法：

```go
func (m *IPFailureWeightManager) RecordFastFail(ip string) {
    // 记录快速失败，并施加更强的惩罚
    record.FastFailCount++
    record.FailureCount++
    // ...
}
```

#### 3. 权重计算优化（`ip_failure_weight.go`）

在 `GetWeight()` 中对快速失败施加更强的惩罚：

```go
// 快速失败惩罚：每次快速失败增加 500ms（比普通失效强 10 倍）
weight += record.FastFailCount * 500
```

**惩罚逻辑**：
- 普通失效：每次 +50ms
- 快速失败：每次 +500ms（强 10 倍）

这反映了快速失败 IP 的严重性：第一次就超时，说明网络质量极差。

#### 4. 探测逻辑改进（`ping_test_methods.go`）

在 `pingIP()` 函数中实现快速失败：

```go
for i := 0; i < p.count; i++ {
    rtt, method := p.smartPingWithMethod(ctx, ip, domain)
    if rtt >= 0 {
        // 成功处理
    } else {
        // 第一次探测就失败，触发快速失败机制
        if i == 0 {
            p.RecordIPFastFail(ip)
            // 直接返回完全失败的结果，不再进行后续探测
            return &Result{IP: ip, RTT: 999999, Loss: 100, ProbeMethod: "none"}
        }
    }
}
```

#### 5. 公共接口（`ping.go`）

新增 `RecordIPFastFail()` 方法供外部调用。

## 性能提升

### 时间节省

假设 `count=3`，`timeoutMs=800`：

**原有逻辑**：
- 第一次超时：800ms
- 第二次超时：800ms
- 第三次超时：800ms
- **总耗时**：2400ms

**快速失败**：
- 第一次超时：800ms
- 直接返回，不再探测
- **总耗时**：800ms

**节省**：66% 的时间（2400ms → 800ms）

### 排序准确性

快速失败的 IP 会被标记并施加 500ms 的权重惩罚，确保它们排在后面，不会被误选。

## 使用示例

```go
// 创建 Pinger 实例
pinger := NewPinger(3, 800, 10)

// 执行 ping 测试
results := pinger.PingAndSort(ctx, ips, domain)

// 查看 IP 的失效记录
record := pinger.GetIPFailureRecord("1.2.3.4")
fmt.Printf("快速失败次数: %d\n", record.FastFailCount)
fmt.Printf("总失效次数: %d\n", record.FailureCount)
```

## 监控和调试

可以通过 `IPFailureRecord` 的 `FastFailCount` 字段监控快速失败的 IP：

- `FastFailCount > 0`：该 IP 曾经快速失败过
- `FastFailCount` 越高：该 IP 越不稳定

## 向后兼容性

- 新增字段 `FastFailCount` 在 JSON 序列化时会被保存
- 旧的记录文件加载时，`FastFailCount` 默认为 0
- 完全向后兼容，无需迁移数据
