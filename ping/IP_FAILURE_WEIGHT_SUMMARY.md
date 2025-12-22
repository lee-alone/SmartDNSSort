# IP失效权重系统实现总结

## 功能概述

实现了一个IP失效权重系统，用于在多WAN口环境下，记录域名解析出来的IP在实际使用中的失效情况，并在排序时自动降低失效IP的优先级。

## 核心实现

### 1. IPFailureWeightManager（ip_failure_weight.go）

管理IP的失效记录和权重计算：

- **RecordFailure(ip)** - 记录IP失效
- **RecordSuccess(ip)** - 记录IP成功
- **GetWeight(ip)** - 获取IP的权重值
- **GetRecord(ip)** - 获取IP的失效记录
- **SaveToDisk()** - 保存到磁盘
- **loadFromDisk()** - 从磁盘加载

### 2. Pinger集成

修改Pinger结构体和相关方法：

- 添加 `failureWeightMgr` 字段
- 添加 `RecordIPFailure()` 和 `RecordIPSuccess()` 方法
- 修改 `sortResults()` 加入失效权重
- 修改 `NewPinger()` 添加持久化文件参数

### 3. 权重计算算法

```
基础权重 = 失效次数 × 50ms
时间衰减 = 线性衰减（7天周期）
最终权重 = 基础权重 × (1 - 衰减因子)
```

### 4. 排序规则

```
最终得分 = RTT + Loss×18 + IP失效权重
得分越低排序越靠前
```

## 关键特性

| 特性 | 实现 |
|------|------|
| 失效计数 | 每次失效+1，超过100自动限制 |
| 权重衰减 | 7天线性衰减，超期清零 |
| 自我修复 | 连续成功3次失效计数-1 |
| 失效率 | 自动计算失效率（失效/总数） |
| 持久化 | JSON格式保存到磁盘 |
| 多WAN支持 | 失效计数全局汇总 |

## 文件清单

### 新增文件

1. **ping/ip_failure_weight.go** - IP失效权重管理器核心实现
2. **ping/ip_failure_weight_test.go** - 完整的单元测试
3. **ping/IP_FAILURE_WEIGHT_GUIDE.md** - 详细使用指南
4. **ping/IP_FAILURE_WEIGHT_INTEGRATION.md** - 集成说明
5. **ping/IP_FAILURE_WEIGHT_SUMMARY.md** - 本文件

### 修改文件

1. **ping/ping.go** - 添加failureWeightMgr字段和相关方法
2. **ping/ping_init.go** - NewPinger添加failureWeightPersistFile参数
3. **ping/ping_concurrent.go** - sortResults加入失效权重
4. **ping/ping_test.go** - 更新NewPinger调用
5. **dnsserver/server_init.go** - 更新NewPinger调用
6. **dnsserver/server_config.go** - 更新NewPinger调用

## 使用流程

### 初始化

```go
pinger := ping.NewPinger(
    3, 800, 8, 0, 300, false,
    "adblock_cache/ip_failure_weights.json",
)
```

### 应用层记录

```go
// 使用IP时记录结果
if err != nil {
    pinger.RecordIPFailure(ip)
} else {
    pinger.RecordIPSuccess(ip)
}
```

### 自动排序

```go
// 调用PingAndSort时失效权重自动应用
results := pinger.PingAndSort(ctx, ips, domain)
```

### 定期保存

```go
pinger.SaveIPFailureWeights()
```

## 测试覆盖

所有测试已通过（9个测试）：

- ✅ TestIPFailureWeightManager - 基础功能
- ✅ TestIPFailureWeightDecay - 权重衰减
- ✅ TestSortResultsWithFailureWeight - 排序调整
- ✅ TestMaxFailureCount - 上限限制
- ✅ TestFailureRateCalculation - 失效率计算
- ✅ TestGetAllRecords - 批量查询
- ✅ TestPinger - Pinger初始化
- ✅ TestPingAndSort - 完整流程
- ✅ TestSortResults - 排序验证

## 配置参数

在 `IPFailureWeightManager` 中可调整：

```go
decayDays       int // 权重衰减周期（默认7天）
maxFailureCount int // 最大失效计数（默认100）
```

## 多WAN场景示例

假设有两个WAN口（WAN1和WAN2），域名解析出IP池：[8.8.8.8, 1.1.1.1]

**场景**：8.8.8.8在WAN2下不通

1. 应用层检测到8.8.8.8不通，调用 `RecordIPFailure("8.8.8.8")`
2. 失效计数增加，权重增加
3. 下次排序时，8.8.8.8排序靠后
4. 应用优先使用1.1.1.1
5. 如果8.8.8.8恢复，连续成功3次后权重自动降低

## 性能影响

- 权重计算：O(1)，仅在排序时执行
- 内存占用：每个IP约200字节
- 磁盘I/O：仅在显式调用SaveIPFailureWeights时执行
- 无额外网络开销

## 向后兼容性

- 新参数为可选（可传空字符串禁用）
- 现有代码需更新NewPinger调用（已完成）
- 不影响现有的RTT和Loss权重计算

## 后续优化方向

1. 支持按WAN口分别记录失效
2. 支持自定义权重衰减周期
3. 支持按域名分别记录IP失效
4. 支持导出失效统计报告
5. 支持Web界面查看失效记录
