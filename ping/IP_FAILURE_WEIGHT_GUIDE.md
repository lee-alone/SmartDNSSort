# IP失效权重系统使用指南

## 概述

IP失效权重系统用于在多WAN口环境下，记录域名解析出来的IP在实际使用中的失效情况，并在排序时降低失效IP的优先级。

## 核心特性

1. **失效计数** - 记录每个IP的失效次数
2. **权重计算** - 失效次数越多，权重越高，排序越靠后
3. **时间衰减** - 距离最后失效越久，权重越低（7天衰减周期）
4. **成功恢复** - 连续成功3次后，失效计数降低1
5. **失效率统计** - 记录IP的失效率（失效次数/总尝试次数）
6. **持久化** - 失效记录可保存到磁盘，重启后恢复

## 使用方式

### 1. 初始化Pinger

```go
import "smartdnssort/ping"

// 创建Pinger实例，指定失效权重持久化文件
pinger := ping.NewPinger(
    3,                                  // count: 每个IP测试3次
    800,                                // timeoutMs: 超时800ms
    8,                                  // concurrency: 并发8个
    0,                                  // maxTestIPs: 测试所有IP
    300,                                // rttCacheTtlSeconds: 缓存300秒
    false,                              // enableHttpFallback: 不启用HTTP备选
    "adblock_cache/ip_failure_weights.json", // failureWeightPersistFile: 失效权重文件路径
)
defer pinger.Stop()
```

### 2. 应用层记录IP失效

当应用层在实际使用IP时，如果发现IP不可用，调用：

```go
// 记录IP失效
pinger.RecordIPFailure("8.8.8.8")

// 记录IP成功
pinger.RecordIPSuccess("8.8.8.8")
```

### 3. 权重自动应用到排序

当调用 `PingAndSort()` 时，失效权重会自动应用到排序中：

```go
// 获取排序后的IP列表
results := pinger.PingAndSort(ctx, ips, domain)
// 返回的结果已经根据失效权重调整了排序
```

### 4. 保存失效权重到磁盘

```go
// 定期保存失效权重（例如在关闭前）
if err := pinger.SaveIPFailureWeights(); err != nil {
    log.Printf("Failed to save IP failure weights: %v", err)
}
```

### 5. 查询失效统计

```go
// 获取单个IP的失效记录
record := pinger.GetIPFailureRecord("8.8.8.8")
fmt.Printf("IP: %s, Failures: %d, Success: %d, Rate: %.2f%%\n", 
    record.IP, record.FailureCount, record.SuccessCount, record.FailureRate*100)

// 获取所有IP的失效记录
allRecords := pinger.GetAllIPFailureRecords()
for _, record := range allRecords {
    fmt.Printf("IP: %s, Failures: %d, Rate: %.2f%%\n", 
        record.IP, record.FailureCount, record.FailureRate*100)
}
```

## 权重计算公式

```
基础权重 = 失效次数 × 50ms

时间衰减因子 = (距离最后失效的天数) / 7天

最终权重 = 基础权重 × (1 - 时间衰减因子)

如果超过7天未失效，权重清零
```

## 排序规则

```
最终得分 = RTT + Loss×18 + IP失效权重

得分越低，排序越靠前
```

## 示例场景

假设有两个IP，都从DNS解析出来：

**IP A**: RTT=50ms, Loss=0%, 失效次数=5, 最后失效=1天前
- 基础权重 = 5 × 50 = 250ms
- 时间衰减 = 1/7 ≈ 0.14
- 最终权重 = 250 × (1 - 0.14) = 215ms
- 最终得分 = 50 + 0 + 215 = 265

**IP B**: RTT=100ms, Loss=0%, 失效次数=0
- 基础权重 = 0
- 最终权重 = 0
- 最终得分 = 100 + 0 + 0 = 100

结果：IP B排序在前，因为虽然RTT较高，但没有失效记录。

## 配置参数

在 `IPFailureWeightManager` 中可调整：

- `decayDays` - 权重衰减周期（默认7天）
- `maxFailureCount` - 最大失效计数（默认100，防止溢出）

## 应用集成示例

```go
// 应用层使用示例
func handleIPUsage(pinger *ping.Pinger, ip string, err error) {
    if err != nil {
        // IP不可用，记录失效
        pinger.RecordIPFailure(ip)
        log.Printf("IP %s failed: %v", ip, err)
    } else {
        // IP可用，记录成功
        pinger.RecordIPSuccess(ip)
    }
}

// 定期保存
func periodicSave(pinger *ping.Pinger, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()
    
    for range ticker.C {
        if err := pinger.SaveIPFailureWeights(); err != nil {
            log.Printf("Failed to save: %v", err)
        }
    }
}
```

## 注意事项

1. 失效权重文件应存储在可持久化的目录（如 `adblock_cache/`）
2. 建议定期调用 `SaveIPFailureWeights()` 保存失效权重
3. 失效计数是全局的，不区分WAN口（多WAN口环境下会自动汇总）
4. 连续成功3次后会自动降低失效计数，实现自我修复
5. 超过7天未失效的IP权重会自动清零
