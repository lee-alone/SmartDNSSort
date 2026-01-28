# 上游查询策略优化建议（代码级别）

## 一、Parallel 策略优化

### 问题：资源浪费和上游限流风险

**当前代码问题：**
```go
// 后台收集响应，最多等待 2 秒
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
```

- 即使第一个成功，仍继续等待 2 秒
- 所有服务器同时接收请求
- 可能导致上游限流

**优化方案 1：快速中止机制**

```go
// 添加一个 done 通道，第一个成功后立即关闭
done := make(chan struct{})

// 在 fastResponseSent.Do 中关闭 done
fastResponseSent.Do(func() {
    select {
    case fastResponseChan <- result:
        close(done)  // 立即关闭，停止后台收集
    default:
    }
})

// 后台收集时检查 done
select {
case <-done:
    return  // 立即返回，不再等待
case <-ctx.Done():
    // 超时处理
}
```

**优化方案 2：采样并发机制**

```go
// 不是并发所有服务器，而是分阶段并发
const (
    initialConcurrency = 2      // 初始并发数
    samplingInterval   = 50 * time.Millisecond  // 采样间隔
)

// 第一阶段：并发前 2 个
for i := 0; i < min(initialConcurrency, len(u.servers)); i++ {
    go queryServer(u.servers[i])
}

// 第二阶段：如果 50ms 内没有响应，并发第 3 个
timer := time.NewTimer(samplingInterval)
select {
case result := <-fastResponseChan:
    return result
case <-timer.C:
    if len(u.servers) > initialConcurrency {
        go queryServer(u.servers[initialConcurrency])
    }
}
```

**优化方案 3：配置化控制**

```go
// 在 config 中添加
type UpstreamConfig struct {
    // ...
    ParallelMaxConcurrent *int  // 最大并发数（默认：所有）
    ParallelBackgroundTimeout *int  // 后台收集超时（默认：2000ms）
    ParallelSamplingInterval *int  // 采样间隔（默认：50ms）
}

// 在 Manager 中使用
if u.parallelMaxConcurrent > 0 && len(u.servers) > u.parallelMaxConcurrent {
    // 限制并发数
}
```

---

## 二、Sequential 策略优化

### 问题：响应速度不稳定

**当前代码问题：**
```go
// 如果第一个服务器慢，整体响应慢
for i, server := range sortedServers {
    reply, err := server.Exchange(attemptCtx, msg)
    // 如果这个超时，要等待 attemptTimeout 才能尝试下一个
}
```

**优化方案 1：快速失败机制**

```go
// 添加一个"快速失败"超时，比总超时短
const fastFailRatio = 0.5  // 50% 的超时时间

fastFailTimeout := attemptTimeout * fastFailRatio

// 第一次尝试使用快速失败超时
if i == 0 {
    attemptCtx, cancel := context.WithTimeout(ctx, fastFailTimeout)
    reply, err := server.Exchange(attemptCtx, msg)
    cancel()
    
    if err == context.DeadlineExceeded {
        // 快速失败，立即尝试下一个
        continue
    }
}
```

**优化方案 2：混合 Racing 策略**

```go
// 如果 Sequential 的第一个服务器超时，自动启动 Racing
if i == 0 && err == context.DeadlineExceeded {
    logger.Debugf("[querySequential] 第一个服务器超时，启动 Racing 备选")
    return u.queryRacing(ctx, domain, qtype, r, dnssec)
}
```

**优化方案 3：动态超时调整**

```go
// 根据最近的成功率调整超时
successRate := u.GetRecentSuccessRate()  // 最近 100 次查询的成功率

if successRate > 0.95 {
    // 成功率高，缩短超时
    attemptTimeout = attemptTimeout * 80 / 100
} else if successRate < 0.80 {
    // 成功率低，延长超时
    attemptTimeout = attemptTimeout * 120 / 100
}
```

---

## 三、Racing 策略优化

### 问题：延迟参数不够智能

**当前代码问题：**
```go
// 固定延迟 50-200ms，不够灵活
raceDelay := u.GetAdaptiveRacingDelay()  // 基于平均延迟的 10%
```

**优化方案 1：基于百分位数的延迟**

```go
// 维护最近 N 次查询的延迟列表
type LatencyStats struct {
    latencies []time.Duration
    mu        sync.RWMutex
}

func (ls *LatencyStats) AddLatency(latency time.Duration) {
    ls.mu.Lock()
    defer ls.mu.Unlock()
    
    ls.latencies = append(ls.latencies, latency)
    if len(ls.latencies) > 100 {
        ls.latencies = ls.latencies[1:]  // 保持最近 100 次
    }
}

func (ls *LatencyStats) GetPercentile(p float64) time.Duration {
    ls.mu.RLock()
    defer ls.mu.RUnlock()
    
    if len(ls.latencies) == 0 {
        return 100 * time.Millisecond
    }
    
    // 排序并计算百分位数
    sorted := make([]time.Duration, len(ls.latencies))
    copy(sorted, ls.latencies)
    sort.Slice(sorted, func(i, j int) bool {
        return sorted[i] < sorted[j]
    })
    
    idx := int(float64(len(sorted)) * p)
    return sorted[idx]
}

// 使用百分位数计算延迟
p50 := u.latencyStats.GetPercentile(0.5)
p95 := u.latencyStats.GetPercentile(0.95)

// Racing 延迟 = P50 + 10ms（给第一个服务器优势）
raceDelay := p50 + 10*time.Millisecond

// 限制范围
if raceDelay < 20*time.Millisecond {
    raceDelay = 20 * time.Millisecond
}
if raceDelay > 200*time.Millisecond {
    raceDelay = 200 * time.Millisecond
}
```

**优化方案 2：动态调整并发数**

```go
// 根据成功率调整并发数
successRate := u.GetRecentSuccessRate()

if successRate > 0.95 {
    // 成功率高，减少并发数（节省资源）
    maxConcurrent = max(2, maxConcurrent-1)
} else if successRate < 0.80 {
    // 成功率低，增加并发数（提高可靠性）
    maxConcurrent = min(len(u.servers), maxConcurrent+1)
}
```

**优化方案 3：分层 Racing**

```go
// 第一层：只查询最佳服务器
reply, err := bestServer.Exchange(ctx, msg)
if err == nil {
    return result
}

// 第二层：延迟后查询前 2 个
time.Sleep(raceDelay)
for i := 1; i < min(2, len(sortedServers)); i++ {
    go queryServer(sortedServers[i])
}

// 第三层：再延迟后查询剩余的
time.Sleep(raceDelay)
for i := 2; i < len(sortedServers); i++ {
    go queryServer(sortedServers[i])
}
```

---

## 四、Auto 策略优化

### 问题：选择逻辑过于简单

**当前代码问题：**
```go
switch {
case numServers <= 1:
    strategy = "sequential"
case numServers <= 3:
    strategy = "racing"
default:
    strategy = "parallel"
}
```

**优化方案 1：考虑网络条件**

```go
// 获取网络质量指标
networkQuality := u.GetNetworkQuality()  // 基于最近的成功率和延迟

switch {
case numServers <= 1:
    strategy = "sequential"
case numServers <= 3:
    if networkQuality == "poor" {
        strategy = "parallel"  // 网络差，用 parallel 提高可靠性
    } else {
        strategy = "racing"
    }
default:
    if networkQuality == "excellent" {
        strategy = "sequential"  // 网络好，用 sequential 节省资源
    } else {
        strategy = "racing"  // 网络一般，用 racing 平衡
    }
}
```

**优化方案 2：考虑服务器特性**

```go
// 分析服务器特性
avgLatency := u.GetAverageLatency()
latencyVariance := u.GetLatencyVariance()  // 延迟方差

switch {
case numServers <= 1:
    strategy = "sequential"
case numServers <= 3:
    if latencyVariance > 100*time.Millisecond {
        strategy = "racing"  // 延迟波动大，用 racing
    } else {
        strategy = "sequential"  // 延迟稳定，用 sequential
    }
default:
    if avgLatency > 500*time.Millisecond {
        strategy = "sequential"  // 延迟高，用 sequential 节省资源
    } else {
        strategy = "racing"  // 延迟低，用 racing 提高速度
    }
}
```

**优化方案 3：支持用户覆盖**

```go
// 在配置中添加
type UpstreamConfig struct {
    Strategy string  // "auto", "parallel", "sequential", "racing", "random"
    
    // 自动策略的覆盖规则
    AutoStrategyRules *AutoStrategyRules
}

type AutoStrategyRules struct {
    // 如果网络质量差，使用此策略
    PoorNetworkStrategy string
    
    // 如果延迟波动大，使用此策略
    HighVarianceStrategy string
    
    // 如果服务器数量多，使用此策略
    ManyServersStrategy string
}
```

---

## 五、通用优化

### 优化 1：实现"性能监控"

```go
type PerformanceMonitor struct {
    mu sync.RWMutex
    
    // 最近 N 次查询的性能数据
    recentQueries []QueryMetrics
    
    // 策略性能统计
    strategyStats map[string]*StrategyStats
}

type QueryMetrics struct {
    Strategy  string
    Latency   time.Duration
    Success   bool
    Timestamp time.Time
}

// 定期输出性能报告
func (pm *PerformanceMonitor) PrintReport() {
    pm.mu.RLock()
    defer pm.mu.RUnlock()
    
    for strategy, stats := range pm.strategyStats {
        logger.Infof("[Performance] Strategy: %s, Success: %.1f%%, Avg Latency: %v",
            strategy, stats.SuccessRate*100, stats.AvgLatency)
    }
}
```

### 优化 2：实现"故障转移"

```go
// 如果当前策略失败率过高，自动切换
func (u *Manager) CheckAndSwitchStrategy() {
    stats := u.GetStrategyMetrics()
    
    currentStats := stats[u.strategy]
    if currentStats.SuccessRate < 0.80 {
        // 成功率低于 80%，尝试切换
        optimalStrategy := u.SelectOptimalStrategy()
        if optimalStrategy != u.strategy {
            logger.Warnf("[Manager] 策略切换: %s -> %s (成功率: %.1f%%)",
                u.strategy, optimalStrategy, currentStats.SuccessRate*100)
            u.strategy = optimalStrategy
        }
    }
}
```

### 优化 3：实现"连接复用"

```go
// 在 Parallel 策略中复用连接
type ConnectionPool struct {
    mu          sync.RWMutex
    connections map[string]*dns.Conn
}

// 查询前检查是否有可复用的连接
conn := pool.GetConnection(server.Address())
if conn != nil {
    reply, err := conn.WriteMsg(msg)
} else {
    // 创建新连接
    conn, err := dns.Dial("tcp", server.Address())
    pool.StoreConnection(server.Address(), conn)
}
```

### 优化 4：实现"请求去重"

```go
// 避免重复查询相同的域名
type QueryDeduplicator struct {
    mu       sync.RWMutex
    pending  map[string]chan *QueryResultWithTTL
}

func (qd *QueryDeduplicator) Query(domain string, qtype uint16) *QueryResultWithTTL {
    key := fmt.Sprintf("%s:%d", domain, qtype)
    
    qd.mu.Lock()
    if ch, exists := qd.pending[key]; exists {
        qd.mu.Unlock()
        // 等待已有的查询完成
        return <-ch
    }
    
    // 创建新的查询
    ch := make(chan *QueryResultWithTTL, 1)
    qd.pending[key] = ch
    qd.mu.Unlock()
    
    // 执行查询
    result := u.Query(ctx, msg, dnssec)
    ch <- result
    
    // 清理
    qd.mu.Lock()
    delete(qd.pending, key)
    qd.mu.Unlock()
    
    return result
}
```

---

## 六、配置示例

### 场景 1：高可靠性（ISP DNS）

```yaml
upstream:
  servers:
    - "223.5.5.5:53"
    - "223.6.6.6:53"
    - "8.8.8.8:53"
  strategy: "parallel"
  timeout_ms: 3000
  concurrency: 3
  parallel_max_concurrent: 3
  parallel_background_timeout: 1000
```

### 场景 2：低延迟（公共 DNS）

```yaml
upstream:
  servers:
    - "1.1.1.1:53"
    - "8.8.8.8:53"
    - "9.9.9.9:53"
  strategy: "racing"
  timeout_ms: 2000
  racing_delay: 50
  racing_max_concurrent: 2
```

### 场景 3：资源受限（嵌入式设备）

```yaml
upstream:
  servers:
    - "223.5.5.5:53"
    - "223.6.6.6:53"
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  strategy: "sequential"
  timeout_ms: 5000
  sequential_timeout: 1500
```

### 场景 4：自动优化

```yaml
upstream:
  servers:
    - "223.5.5.5:53"
    - "223.6.6.6:53"
    - "8.8.8.8:53"
  strategy: "auto"
  timeout_ms: 3000
  dynamic_param_optimization:
    ewma_alpha: 0.2
    max_step_ms: 10
```

---

## 七、实现检查清单

- [ ] 实现"快速中止"机制（Parallel）
- [ ] 实现"采样并发"机制（Parallel）
- [ ] 实现"快速失败"机制（Sequential）
- [ ] 实现"基于百分位数的延迟"（Racing）
- [ ] 实现"动态并发数调整"（Racing）
- [ ] 改进"Auto 策略"的选择逻辑
- [ ] 实现"性能监控"
- [ ] 实现"故障转移"
- [ ] 实现"连接复用"
- [ ] 实现"请求去重"
- [ ] 添加配置项支持
- [ ] 添加单元测试
- [ ] 添加性能基准测试
- [ ] 更新文档

---

## 八、预期收益

| 优化项 | 响应速度 | 资源消耗 | 可靠性 | 复杂度 |
|--------|---------|---------|--------|--------|
| 快速中止 | ↑ 5% | ↓ 30% | → | 低 |
| 采样并发 | → | ↓ 20% | ↓ 5% | 中 |
| 快速失败 | ↑ 10% | → | → | 低 |
| 百分位数延迟 | ↑ 8% | → | ↑ 5% | 中 |
| 动态并发 | ↑ 5% | ↓ 10% | ↑ 10% | 中 |
| 改进 Auto | ↑ 10% | ↓ 15% | ↑ 8% | 低 |
| 性能监控 | → | → | ↑ 5% | 低 |
| 故障转移 | → | → | ↑ 15% | 中 |

**总体预期：** 响应速度 ↑ 15-20%，资源消耗 ↓ 20-30%，可靠性 ↑ 20-30%
