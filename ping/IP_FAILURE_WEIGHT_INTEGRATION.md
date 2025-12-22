# IP失效权重系统集成说明

## 快速开始

### 1. 初始化

```go
pinger := ping.NewPinger(
    3, 800, 8, 0, 300, false,
    "adblock_cache/ip_failure_weights.json", // 新增参数：失效权重文件路径
)
defer pinger.Stop()
```

### 2. 应用层记录失效

在应用层使用IP时，记录其可用性：

```go
// IP可用
pinger.RecordIPSuccess(ip)

// IP不可用
pinger.RecordIPFailure(ip)
```

### 3. 排序自动应用权重

```go
// 调用PingAndSort时，失效权重自动应用到排序
results := pinger.PingAndSort(ctx, ips, domain)
```

### 4. 定期保存

```go
// 应用关闭前保存
pinger.SaveIPFailureWeights()
```

## 工作原理

### 权重计算

```
基础权重 = 失效次数 × 50ms
时间衰减 = 线性衰减（7天周期）
最终权重 = 基础权重 × (1 - 衰减因子)
```

### 排序公式

```
最终得分 = RTT + Loss×18 + IP失效权重
```

### 自我修复

- 连续成功3次后，失效计数自动降低1
- 超过7天未失效的IP权重自动清零

## 关键特性

| 特性 | 说明 |
|------|------|
| 失效计数 | 记录IP的失效次数 |
| 权重衰减 | 7天线性衰减，超期清零 |
| 自我修复 | 连续成功3次降低失效计数 |
| 失效率 | 自动计算失效率（失效/总数） |
| 持久化 | 支持保存到磁盘，重启恢复 |
| 多WAN | 失效计数全局汇总 |

## 查询接口

```go
// 获取单个IP的失效记录
record := pinger.GetIPFailureRecord(ip)
// record.FailureCount - 失效次数
// record.FailureRate - 失效率
// record.LastFailureTime - 最后失效时间

// 获取所有IP的失效记录
records := pinger.GetAllIPFailureRecords()
```

## 配置调整

在 `ip_failure_weight.go` 中修改：

```go
decayDays       int // 权重衰减周期（默认7天）
maxFailureCount int // 最大失效计数（默认100）
```

## 测试验证

```bash
go test -v ./ping -run "TestIPFailure"
```

所有测试已通过，包括：
- 失效计数
- 权重衰减
- 排序调整
- 持久化加载
- 失效率计算
