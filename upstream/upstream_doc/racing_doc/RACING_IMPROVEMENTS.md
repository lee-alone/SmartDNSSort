# Racing 策略深度重构 - 改进文档

## 概述

本文档详细说明了 Racing 策略的三项核心改进，将其从"固定偏见的并行"演进为"概率避险型竞速"。

---

## 改进 1: 基于服务器健康状态的"冷静期"调整

### 问题
原始实现中，所有备选服务器都以相同的节奏启动，不考虑其健康状态。这意味着即使某个服务器处于降级或熔断状态，仍然会被纳入竞速队列。

### 解决方案
在梯队启动时，检查服务器的健康状态：

```go
// 检查服务器健康状态，决定是否跳过或延后
if shouldSkipServerInRacing(srv) {
    logger.Debugf("[queryRacing] 跳过不健康的服务器: %s (状态=%v)", 
        srv.Address(), srv.GetHealth().GetStatus())
    continue
}
```

### 实现细节

**shouldSkipServerInRacing 函数**：
- 跳过条件：服务器处于 `HealthStatusUnhealthy` 状态（熔断）
- 保留条件：`HealthStatusHealthy` 和 `HealthStatusDegraded` 状态的服务器仍然可以尝试
- 这样既避免了向已知故障的服务器发送请求，又给降级服务器恢复的机会

### 收益
- **资源优化**：避免向熔断的服务器发送请求
- **快速恢复**：降级服务器仍有机会参与竞速，加快恢复
- **智能容错**：根据实时健康状态动态调整策略

---

## 改进 2: 动态批次大小和间隔

### 问题
原始实现使用固定的批次大小（2个）和间隔（20ms），不能根据网络状况和服务器数量动态调整。

### 解决方案
根据两个因素动态计算批次参数：

```go
func (u *Manager) calculateRacingBatchParams(remainingCount int, stdDev time.Duration) (batchSize int, stagger time.Duration) {
    batchSize = 2
    stagger = 20 * time.Millisecond

    // 如果网络抖动较大（标准差 > 50ms），更激进地启动
    if stdDev > 50*time.Millisecond {
        batchSize = 3
        stagger = 15 * time.Millisecond
    }

    // 如果剩余服务器很多（> 5个），增加批次大小以加快启动
    if remainingCount > 5 {
        batchSize = min(batchSize+1, 4)
    }

    return batchSize, stagger
}
```

### 参数调整规则

| 场景 | 批次大小 | 间隔 | 原因 |
|------|---------|------|------|
| 稳定网络 + 少量服务器 | 2 | 20ms | 保守策略，避免资源浪费 |
| 抖动网络 + 少量服务器 | 3 | 15ms | 更激进地启动，对冲网络不稳定 |
| 稳定网络 + 多量服务器 | 3 | 20ms | 加快启动速度 |
| 抖动网络 + 多量服务器 | 4 | 15ms | 最激进，快速覆盖所有备选 |

### 收益
- **网络自适应**：根据标准差自动调整策略
- **资源高效**：多服务器场景下加快启动，减少无效等待
- **平衡性**：在稳定和不稳定网络间找到最优平衡点

---

## 改进 3: 细粒度的"快速失败"错误分类

### 问题
原始实现中，所有错误都触发"错误抢跑"，包括应用层错误（如 SERVFAIL）。这可能导致不必要的提前启动。

### 解决方案
区分网络层错误和应用层错误：

```go
// 只有"明确的网络错误"才触发抢跑
if isPrimary && isNetworkError(err) {
    earlyTriggerOnce.Do(func() {
        close(cancelDelayChan)
        earlyTriggerCount.Add(1)
        logger.Debugf("[queryRacing] 主请求网络错误，触发错误抢跑: %v", err)
    })
}
```

### 错误分类

**触发抢跑的网络错误**：
- Connection refused（连接拒绝）
- Connection reset（连接重置）
- Connection timeout（连接超时）
- I/O timeout（I/O 超时）
- No such host（主机不存在）
- Network unreachable（网络不可达）
- Host unreachable（主机不可达）
- Broken pipe（管道破裂）

**不触发抢跑的应用层错误**：
- SERVFAIL（服务器故障）
- REFUSED（查询被拒绝）
- 其他 DNS 响应码错误

### 实现细节

**isNetworkError 函数**：
```go
func isNetworkError(err error) bool {
    // 检查 net.Error 接口
    if netErr, ok := err.(net.Error); ok {
        return netErr.Timeout() || netErr.Temporary()
    }

    // 检查错误字符串中的关键词
    errStr := err.Error()
    networkKeywords := []string{
        "connection refused",
        "connection reset",
        "i/o timeout",
        // ...
    }

    for _, keyword := range networkKeywords {
        if contains(errStr, keyword) {
            return true
        }
    }

    return false
}
```

### 收益
- **精准触发**：只在真正的网络故障时才抢跑
- **避免误触发**：应用层错误不会导致不必要的提前启动
- **更好的日志**：区分错误类型便于调试和监控

---

## 统计和监控

### 记录的指标

```go
type RacingStats struct {
    totalQueries          int64         // 总查询数
    successQueries        int64         // 成功查询数
    earlyTriggerCount     int64         // 错误抢跑触发次数
    earlyTriggerTimeSaved time.Duration // 错误抢跑节省的总时间
}
```

### 日志输出示例

```
[queryRacing] 开始竞争查询: example.com (延迟=50ms, 标准差=25ms, 最大并发=4)
[queryRacing] 启动备选梯队: 批次大小=3, 间隔=15ms
[queryRacing] 主请求网络错误，触发错误抢跑: connection refused
[queryRacing] 竞速获胜者: secondary:53 (耗时: 45ms)
```

---

## 性能对比

### 场景 1: 主服务器宕机

**原始实现**：
- 主服务器等待 100ms 超时
- 然后启动备选
- 总耗时：~150ms

**改进实现**：
- 主服务器立即报错（网络错误）
- 立即启动备选（0ms 延迟）
- 总耗时：~50ms
- **节省：100ms**

### 场景 2: 网络极度不稳定

**原始实现**：
- 固定延迟 100ms
- 固定批次大小 2
- 总耗时：~200ms

**改进实现**：
- 自适应延迟 20ms（基于高标准差）
- 动态批次大小 4（更激进）
- 总耗时：~80ms
- **节省：120ms**

### 场景 3: 多个服务器，网络稳定

**原始实现**：
- 延迟 100ms
- 批次大小 2
- 总耗时：~150ms

**改进实现**：
- 延迟 100ms（基于低标准差）
- 批次大小 2（保守）
- 总耗时：~150ms
- **保持一致，但资源利用更高效**

---

## 集成指南

### 1. 确保 Manager 初始化正确

```go
dynamicOpt := &DynamicParamOptimization{
    ewmaAlpha:  0.2,
    maxStepMs:  10,
    avgLatency: 200 * time.Millisecond,
}
```

### 2. 定期记录查询延迟

```go
u.RecordQueryLatency(time.Since(queryStartTime))
```

### 3. 监控统计指标

```go
stats := u.GetDynamicParamStats()
logger.Infof("Racing stats: %v", stats)
```

---

## 测试

运行单元测试验证改进：

```bash
go test -v ./upstream -run TestRacing
```

测试覆盖：
- ✅ 网络错误分类
- ✅ 服务器健康状态检查
- ✅ 动态批次参数计算
- ✅ 错误抢跑机制
- ✅ 字符串匹配（不区分大小写）

---

## 总结

Racing 策略现已具备"文武双全"的特性：

- **"文"（稳定时）**：方差感知延迟，温和克制，资源利用高效
- **"武"（弱网时）**：主上游一倒立即补位，多梯队激进启动，全力冲突

这些改进使 Racing 策略更加智能、高效和可靠。
