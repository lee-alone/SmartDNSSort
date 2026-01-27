# DNS 上游查询性能瓶颈代码级分析 - 第一部分

## 1. 并行查询的信号量排队问题

### 问题代码位置
`upstream/manager_parallel.go` - queryParallel 函数

### 问题代码
```go
// 创建信号量控制并发
sem := make(chan struct{}, u.concurrency)
var wg sync.WaitGroup

// 并发查询所有服务器
for _, server := range u.servers {
    wg.Add(1)
    go func(srv Upstream) {
        defer wg.Done()

        // 获取信号量 - 这里可能阻塞！
        sem <- struct{}{}
        defer func() { <-sem }()

        // 执行查询...
        reply, err := srv.Exchange(queryCtx, msg)
        // ...
    }(server)
}
```

### 问题分析

**排队场景**
```
假设：
- 服务器数：10 个
- concurrency：5
- 每个查询耗时：100ms

时间线：
T=0ms:   goroutine 1-5 获取信号量，开始查询
T=0ms:   goroutine 6-10 等待信号量（排队）
T=100ms: goroutine 1-5 完成，释放信号量
T=100ms: goroutine 6-10 获取信号量，开始查询
T=200ms: goroutine 6-10 完成

总耗时：200ms（而不是 100ms）
```

**性能影响**
- 响应时间增加 100%
- 无法充分利用所有服务器的并行能力
- 在高并发场景下，排队延迟会更严重

### 优化方案

**方案 1：动态并发数（推荐）**
```go
// 在 NewManager 中
if concurrency < len(servers) {
    concurrency = len(servers)  // 至少等于服务器数
}

// 这样可以确保所有服务器同时发起查询
```

**方案 2：工作池模式**
```go
// 使用固定大小的工作池
type WorkerPool struct {
    tasks chan Task
    workers int
}

// 避免 goroutine 爆炸，同时保证并发
```

**方案 3：优先级队列**
```go
// 重要查询优先获取信号量
type PriorityQueue struct {
    high chan Task
    low  chan Task
}
```

---

## 2. 连接池耗尽导致的快速失败

### 问题代码位置
`upstream/transport/connection_pool.go` - Exchange 函数

### 问题代码
```go
func (p *ConnectionPool) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
    p.mu.Lock()
    p.totalRequests++
    p.mu.Unlock()

    // 尝试获取连接
    select {
    case poolConn = <-p.idleConns:
        // 获取到空闲连接
    default:
        // 池中没有空闲连接，尝试创建新连接
        p.mu.Lock()
        if p.activeCount < p.maxConnections {
            p.activeCount++
            p.mu.Unlock()
            poolConn, err = p.createConnection(ctx)
            // ...
        } else {
            // 达到上限，进入弹性等待机制
            waiting := atomic.AddInt32(&p.waitingCount, 1)
            defer atomic.AddInt32(&p.waitingCount, -1)
            p.mu.Unlock()

            // 计算弹性等待时间
            waitDuration := p.getAdaptiveWaitTime()

            // 如果启用 fastFailMode 且排队人数过多，直接降级
            if p.fastFailMode && waiting > 20 {
                p.recordCongestion()
                return nil, ErrRequestThrottled  // 快速失败！
            }

            timer := time.NewTimer(waitDuration)
            defer timer.Stop()

            select {
            case poolConn = <-p.idleConns:
                // 获取到连接
            case <-timer.C:
                p.recordCongestion()
                return nil, ErrPoolExhausted  // 等待超时，快速失败！
            case <-ctx.Done():
                return nil, ctx.Err()
            }
        }
    }
    // ...
}
```

### 问题分析

**耗尽场景**
```
假设：
- maxConnections：10
- 并发查询数：20
- 每个查询耗时：500ms

时间线：
T=0ms:   请求 1-10 创建连接，开始查询
T=0ms:   请求 11-20 进入弹性等待（waitingCount=10）
T=100ms: waitingCount > 20 时，触发 fastFailMode
         请求 11-20 返回 ErrRequestThrottled
T=500ms: 请求 1-10 完成，释放连接

结果：请求 11-20 全部失败
```

**性能影响**
- 请求失败率高达 50%
- 客户端需要重试
- 总体吞吐量下降

### 优化方案

**方案 1：增加连接池大小（最简单）**
```go
// 当前配置
maxConnections: 10

// 优化后
maxConnections: 50  // 根据并发数调整

// 计算公式
maxConnections = max(10, expectedConcurrency * 1.5)
```

**方案 2：动态连接数**
```go
// 根据负载动态调整
func (p *ConnectionPool) adjustMaxConnections() {
    utilization := float64(p.activeCount) / float64(p.maxConnections)
    
    if utilization > 0.8 {
        // 增加连接数
        p.maxConnections = min(p.maxConnections * 2, MaxConnectionsLimit)
    } else if utilization < 0.3 {
        // 减少连接数
        p.maxConnections = max(p.maxConnections / 2, MinConnections)
    }
}
```

**方案 3：优化等待时间**
```go
// 当前等待时间：平均延迟的 10%（10-200ms）
// 优化后：平均延迟的 5%（5-100ms）

func (p *ConnectionPool) getAdaptiveWaitTime() time.Duration {
    // 当前实现
    waitTime := p.avgLatency / 10
    
    // 优化实现
    waitTime := p.avgLatency / 20  // 减少等待时间
    
    // 限制范围
    if waitTime < 5*time.Millisecond {
        waitTime = 5 * time.Millisecond
    }
    if waitTime > 100*time.Millisecond {
        waitTime = 100 * time.Millisecond
    }
    
    return waitTime
}
```

---

## 3. 熔断状态恢复延迟

### 问题代码位置
`upstream/health.go` - ServerHealth 结构体

### 问题代码
```go
// 熔断配置
type HealthCheckConfig struct {
    FailureThreshold        int  // 3 次失败进入降级
    CircuitBreakerThreshold int  // 5 次失败进入熔断
    CircuitBreakerTimeout   int  // 30 秒后尝试恢复
    SuccessThreshold        int  // 2 次成功恢复健康
}

// 标记失败
func (h *ServerHealth) MarkFailure() {
    h.mu.Lock()
    defer h.mu.Unlock()

    h.consecutiveFailures++
    h.consecutiveSuccesses = 0
    h.lastFailureTime = time.Now()

    // 根据失败次数更新状态
    if h.consecutiveFailures >= h.config.CircuitBreakerThreshold {
        if h.status != HealthStatusUnhealthy {
            h.status = HealthStatusUnhealthy
            h.circuitBreakerStartTime = time.Now()
        }
    }
}

// 判断是否应该跳过
func (h *ServerHealth) ShouldSkipTemporarily() bool {
    h.mu.RLock()
    defer h.mu.RUnlock()

    if h.status != HealthStatusUnhealthy {
        return false
    }

    // 熔断 30 秒后尝试恢复
    elapsed := time.Since(h.circuitBreakerStartTime)
    if elapsed > time.Duration(h.config.CircuitBreakerTimeout)*time.Second {
        return false  // 允许尝试恢复
    }

    return true  // 继续跳过
}
```

### 问题分析

**恢复延迟场景**
```
假设：
- 服务器 A 连续失败 5 次
- 进入熔断状态，被跳过
- 30 秒内，所有查询都跳过服务器 A
- 30 秒后，才尝试恢复

问题：
- 如果服务器 A 已经恢复，但需要等待 30 秒
- 这 30 秒内，所有查询都无法使用服务器 A
- 可能导致其他服务器过载
```

**性能影响**
- 服务器恢复延迟高达 30 秒
- 可能导致其他服务器过载
- 无法快速响应服务器恢复

### 优化方案

**方案 1：降低熔断阈值**
```go
// 当前配置
CircuitBreakerThreshold: 5

// 优化后
CircuitBreakerThreshold: 3  // 更快进入熔断，也更快尝试恢复
```

**方案 2：指数退避恢复**
```go
// 实现指数退避
func (h *ServerHealth) ShouldSkipTemporarily() bool {
    h.mu.RLock()
    defer h.mu.RUnlock()

    if h.status != HealthStatusUnhealthy {
        return false
    }

    elapsed := time.Since(h.circuitBreakerStartTime)
    
    // 指数退避：第一次 10s，第二次 20s，第三次 30s
    recoveryAttempts := h.consecutiveRecoveryAttempts
    backoffDuration := time.Duration(10 * (1 << uint(recoveryAttempts))) * time.Second
    
    if elapsed > backoffDuration {
        return false  // 允许尝试恢复
    }

    return true
}
```

**方案 3：主动健康检查**
```go
// 定期向熔断的服务器发送探针
func (h *ServerHealth) ProbeHealth(ctx context.Context) {
    // 发送 DNS 查询探针
    msg := new(dns.Msg)
    msg.SetQuestion("example.com.", dns.TypeA)
    
    reply, err := h.upstream.Exchange(ctx, msg)
    
    if err == nil && reply.Rcode == dns.RcodeSuccess {
        // 服务器已恢复，立即标记为健康
        h.MarkSuccess()
        h.consecutiveRecoveryAttempts = 0
    }
}
```

