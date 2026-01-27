# DNS 上游查询性能瓶颈 - 快速参考

## 🎯 核心问题速查

### 问题 1：响应时间慢

**可能原因**
- [ ] 连接池耗尽（ErrPoolExhausted）
- [ ] 信号量排队（并行查询）
- [ ] 单点故障延迟（顺序查询）
- [ ] 竞速固定延迟（竞争查询）

**快速诊断**
```bash
# 检查连接池状态
grep "ErrPoolExhausted" logs/

# 检查信号量排队
grep "waitingCount" logs/

# 检查超时
grep "DeadlineExceeded" logs/
```

**快速修复**
```go
// 增加连接池大小
maxConnections: 50  // 从 10 改为 50

// 动态调整并发数
concurrency: len(servers)  // 至少等于服务器数

// 缩短单次超时
sequentialTimeoutMs: 1000  // 从 1500 改为 1000
```

---

### 问题 2：错误率高

**可能原因**
- [ ] 连接池耗尽导致快速失败
- [ ] 熔断状态跳过服务器
- [ ] 服务器故障未及时转移
- [ ] 网络延迟过高

**快速诊断**
```bash
# 检查连接池耗尽
grep "ErrPoolExhausted\|ErrRequestThrottled" logs/

# 检查熔断状态
grep "ShouldSkipTemporarily" logs/

# 检查错误率
grep "IncUpstreamFailure" logs/
```

**快速修复**
```go
// 增加连接池大小
maxConnections: 50

// 降低熔断阈值
CircuitBreakerThreshold: 3  // 从 5 改为 3

// 改用并行查询
strategy: "parallel"
```

---

### 问题 3：缓存不更新

**可能原因**
- [ ] 后台收集无超时控制
- [ ] 后台收集失败
- [ ] 缓存过期时间不同步
- [ ] 排序缓存和原始缓存不同步

**快速诊断**
```bash
# 检查后台收集
grep "collectRemainingResponses" logs/

# 检查缓存更新
grep "cacheUpdateCallback" logs/

# 检查缓存过期
grep "IsExpired" logs/
```

**快速修复**
```go
// 添加后台收集超时
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

// 统一缓存过期时间
expirationTime := time.Now().Add(time.Duration(ttl) * time.Second)

// 添加错误处理
if err := u.updateCache(domain, qtype, allResults); err != nil {
    logger.Warnf("缓存更新失败: %v", err)
}
```

---

### 问题 4：服务器恢复慢

**可能原因**
- [ ] 熔断恢复延迟为 30 秒
- [ ] 熔断阈值过高（5 次）
- [ ] 没有主动健康检查
- [ ] 恢复策略不灵活

**快速诊断**
```bash
# 检查熔断状态
grep "HealthStatusUnhealthy" logs/

# 检查恢复尝试
grep "ShouldSkipTemporarily" logs/

# 检查恢复成功
grep "MarkSuccess" logs/
```

**快速修复**
```go
// 降低熔断阈值
CircuitBreakerThreshold: 3  // 从 5 改为 3

// 实现指数退避恢复
backoffDuration := time.Duration(10 * (1 << uint(recoveryAttempts))) * time.Second

// 添加主动健康检查
go func() {
    time.Sleep(10 * time.Second)
    h.ProbeHealth(ctx)
}()
```

---

## 📊 性能指标速查

### 响应时间基准

| 策略 | 正常 | 1 个故障 | 2 个故障 |
|------|------|---------|---------|
| Sequential | 100ms | 1600ms | 3100ms |
| Parallel | 100ms | 100ms | 100ms |
| Racing | 100ms | 100ms | 100ms |
| Random | 100ms | 1600ms | 3100ms |

### 吞吐量基准

| 并发数 | Sequential | Parallel | Racing | Random |
|--------|-----------|----------|--------|--------|
| 10 | 100 QPS | 100 QPS | 100 QPS | 100 QPS |
| 100 | 100 QPS | 150 QPS | 120 QPS | 100 QPS |
| 1000 | 100 QPS | 200 QPS | 150 QPS | 100 QPS |

### 错误率基准

| 故障数 | Sequential | Parallel | Racing | Random |
|--------|-----------|----------|--------|--------|
| 1 个 | 20% | 0.1% | 1% | 20% |
| 2 个 | 40% | 0.01% | 2% | 40% |
| 3 个 | 60% | 0.001% | 5% | 60% |

---

## 🔧 配置速查

### 高可用配置

```go
// 最大化可靠性和吞吐量
strategy: "parallel"
concurrency: 20
maxConnections: 50
timeoutMs: 5000

// 健康检查
CircuitBreakerThreshold: 3
CircuitBreakerTimeout: 30
SuccessThreshold: 2

// 缓存
CacheTTL: 300
NegativeTTLSeconds: 60
```

### 资源受限配置

```go
// 最小化资源消耗
strategy: "sequential"
concurrency: 1
maxConnections: 10
sequentialTimeoutMs: 1000

// 健康检查
CircuitBreakerThreshold: 5
CircuitBreakerTimeout: 60
SuccessThreshold: 3

// 缓存
CacheTTL: 600
NegativeTTLSeconds: 120
```

### 平衡配置

```go
// 平衡性能和资源
strategy: "racing"
concurrency: 10
maxConnections: 30
racingDelayMs: 50

// 健康检查
CircuitBreakerThreshold: 3
CircuitBreakerTimeout: 30
SuccessThreshold: 2

// 缓存
CacheTTL: 300
NegativeTTLSeconds: 60
```

---

## 📈 优化优先级

### 第 1 周（立即实施）

```go
// 1. 增加连接池大小
maxConnections: 50  // 从 10 改为 50

// 2. 动态调整并发数
concurrency: len(servers)  // 至少等于服务器数

// 3. 降低熔断阈值
CircuitBreakerThreshold: 3  // 从 5 改为 3

// 4. 缩短单次超时
sequentialTimeoutMs: 1000  // 从 1500 改为 1000
```

**预期收益**
- 响应时间 -20%
- 吞吐量 +50%
- 错误率 -50%

### 第 2-3 周（逐步实施）

```go
// 5. 后台收集超时
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

// 6. 指数退避恢复
backoffDuration := time.Duration(10 * (1 << uint(attempts))) * time.Second

// 7. 添加监控指标
metrics.RecordLatency(latency)
metrics.RecordError(err)
```

**预期收益**
- 缓存更新可靠性 +50%
- 恢复灵活性 +100%

### 第 4-8 周（长期优化）

```go
// 8. 统一缓存过期时间
expirationTime := time.Now().Add(time.Duration(ttl) * time.Second)

// 9. 预测性缓存刷新
if elapsed > ttl * 0.9 {
    s.RefreshDomain(domain, qtype)
}

// 10. 自适应查询策略
if failureRate > 0.1 {
    strategy = "parallel"
} else {
    strategy = "sequential"
}
```

**预期收益**
- 缓存命中率 +20%
- 数据一致性 +100%

---

## 🐛 常见问题排查

### Q1: 为什么响应时间突然变慢？

**检查清单**
- [ ] 连接池是否耗尽？`grep "ErrPoolExhausted"`
- [ ] 是否有服务器故障？`grep "MarkFailure"`
- [ ] 网络延迟是否增加？`grep "latency"`
- [ ] 缓存是否失效？`grep "IsExpired"`

**快速修复**
```bash
# 增加连接池大小
# 检查服务器状态
# 检查网络延迟
# 清空缓存重新加载
```

### Q2: 为什么错误率这么高？

**检查清单**
- [ ] 连接池是否耗尽？`grep "ErrPoolExhausted"`
- [ ] 是否有多个服务器故障？`grep "MarkFailure"`
- [ ] 熔断状态是否过多？`grep "ShouldSkipTemporarily"`
- [ ] 是否使用了 Random 策略？`grep "strategy.*random"`

**快速修复**
```bash
# 增加连接池大小
# 检查服务器健康状态
# 改用 Parallel 策略
# 降低熔断阈值
```

### Q3: 为什么缓存不更新？

**检查清单**
- [ ] 后台收集是否超时？`grep "collectRemainingResponses"`
- [ ] 缓存更新回调是否被调用？`grep "cacheUpdateCallback"`
- [ ] 缓存是否过期？`grep "IsExpired"`
- [ ] 排序缓存和原始缓存是否同步？`grep "GetSorted\|GetRaw"`

**快速修复**
```bash
# 添加后台收集超时
# 检查缓存更新回调
# 统一缓存过期时间
# 添加缓存一致性检查
```

### Q4: 为什么服务器恢复这么慢？

**检查清单**
- [ ] 熔断阈值是否过高？`grep "CircuitBreakerThreshold"`
- [ ] 熔断恢复延迟是否过长？`grep "CircuitBreakerTimeout"`
- [ ] 是否有主动健康检查？`grep "ProbeHealth"`
- [ ] 恢复策略是否灵活？`grep "ShouldSkipTemporarily"`

**快速修复**
```bash
# 降低熔断阈值（从 5 改为 3）
# 实现指数退避恢复
# 添加主动健康检查
# 添加恢复探针
```

---

## 📋 检查清单

### 部署前检查

- [ ] 连接池大小是否合理？（建议 50）
- [ ] 并发数是否等于服务器数？
- [ ] 熔断阈值是否合理？（建议 3）
- [ ] 单次超时是否合理？（建议 1000ms）
- [ ] 后台收集是否有超时？（建议 2s）
- [ ] 缓存过期时间是否同步？
- [ ] 监控指标是否完整？
- [ ] 告警规则是否配置？

### 上线后检查

- [ ] 响应时间是否在预期范围内？
- [ ] 错误率是否低于 5%？
- [ ] 吞吐量是否达到预期？
- [ ] 内存占用是否在预期范围内？
- [ ] 连接池是否频繁耗尽？
- [ ] 熔断状态是否过多？
- [ ] 缓存命中率是否达到预期？
- [ ] 是否有异常日志？

---

## 🚀 快速优化步骤

### 步骤 1：诊断（5 分钟）

```bash
# 收集性能数据
grep "latency\|error\|timeout" logs/ | tail -1000

# 检查连接池状态
grep "ErrPoolExhausted\|activeCount" logs/

# 检查熔断状态
grep "HealthStatusUnhealthy" logs/
```

### 步骤 2：优化（10 分钟）

```go
// 修改配置
maxConnections: 50
concurrency: len(servers)
CircuitBreakerThreshold: 3
sequentialTimeoutMs: 1000
```

### 步骤 3：验证（5 分钟）

```bash
# 重启服务
systemctl restart dns-server

# 监控性能
tail -f logs/ | grep "latency\|error"

# 检查指标
curl http://localhost:8080/metrics
```

### 步骤 4：监控（持续）

```bash
# 设置告警
# 响应时间 P95 > 1s
# 错误率 > 5%
# 连接池耗尽 > 100/分钟
```

---

## 📞 获取帮助

### 相关文档

1. **PERFORMANCE_BOTTLENECK_ANALYSIS.md** - 详细分析
2. **BOTTLENECK_CODE_ANALYSIS_PART1.md** - 代码级分析（第一部分）
3. **BOTTLENECK_CODE_ANALYSIS_PART2.md** - 代码级分析（第二部分）
4. **OPTIMIZATION_RECOMMENDATIONS.md** - 优化建议
5. **STRATEGY_COMPARISON.md** - 策略对比

### 常用命令

```bash
# 查看日志
tail -f logs/dns-server.log

# 查看性能指标
curl http://localhost:8080/metrics

# 查看连接池状态
curl http://localhost:8080/debug/pprof/heap

# 查看 goroutine 数量
curl http://localhost:8080/debug/pprof/goroutine
```

### 联系方式

- 性能问题：检查 PERFORMANCE_BOTTLENECK_ANALYSIS.md
- 代码问题：检查 BOTTLENECK_CODE_ANALYSIS_PART1/2.md
- 优化建议：检查 OPTIMIZATION_RECOMMENDATIONS.md
- 策略选择：检查 STRATEGY_COMPARISON.md

