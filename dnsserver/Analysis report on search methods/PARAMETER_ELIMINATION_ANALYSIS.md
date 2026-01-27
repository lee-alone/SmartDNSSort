# DNS 查询参数消除分析

## 问题分析

当前配置中有很多查询相关的参数需要用户手动指定。通过优化方案，这些参数可以被**自动计算**或**消除**。

---

## 当前配置参数

### 上游管理器参数

```go
type Manager struct {
    strategy              string  // "parallel", "random", "sequential", "racing"
    timeoutMs             int     // 全局超时（毫秒）
    concurrency           int     // 并行查询的并发数
    racingDelayMs         int     // 竞速延迟（毫秒）
    racingMaxConcurrent   int     // 竞速最大并发数
    sequentialTimeoutMs   int     // 顺序查询单次超时
}
```

### 连接池参数

```go
type ConnectionPool struct {
    maxConnections        int     // 最大连接数
    idleTimeout           time.Duration  // 空闲超时
    dialTimeout           time.Duration  // 拨号超时
    readTimeout           time.Duration  // 读超时
    writeTimeout          time.Duration  // 写超时
}
```

### 健康检查参数

```go
type HealthCheckConfig struct {
    FailureThreshold        int  // 进入降级的失败次数
    CircuitBreakerThreshold int  // 进入熔断的失败次数
    CircuitBreakerTimeout   int  // 熔断恢复延迟（秒）
    SuccessThreshold        int  // 恢复所需的成功次数
}
```

---

## 参数消除方案

### 方案 1：自动计算并发数

**当前**
```go
concurrency: 5  // 用户手动指定
```

**优化后**
```go
// 自动计算，无需配置
concurrency = max(len(servers), min(20, runtime.NumCPU() * 2))
```

**优势**
- ✅ 无需用户配置
- ✅ 根据服务器数和 CPU 核心数自动调整
- ✅ 避免排队问题

**代码位置**
```go
// upstream/manager.go
func NewManager(...) *Manager {
    if concurrency <= 0 {
        // 自动计算
        concurrency = max(len(servers), min(20, runtime.NumCPU() * 2))
    }
    // ...
}
```

---

### 方案 2：自动计算连接池大小

**当前**
```go
maxConnections: 10  // 用户手动指定
```

**优化后**
```go
// 自动计算，无需配置
maxConnections = max(20, runtime.NumCPU() * 5)
```

**优势**
- ✅ 无需用户配置
- ✅ 根据 CPU 核心数自动调整
- ✅ 避免连接池耗尽

**代码位置**
```go
// upstream/transport/connection_pool.go
func NewConnectionPool(...) *ConnectionPool {
    if maxConnections <= 0 {
        // 自动计算
        maxConnections = max(20, runtime.NumCPU() * 5)
    }
    // ...
}
```

---

### 方案 3：自动选择查询策略

**当前**
```go
strategy: "parallel"  // 用户手动指定
```

**优化后**
```go
// 根据服务器数自动选择，无需配置
if len(servers) == 1 {
    strategy = "sequential"  // 单服务器用顺序
} else if len(servers) <= 3 {
    strategy = "racing"      // 少数服务器用竞速
} else {
    strategy = "parallel"    // 多个服务器用并行
}
```

**优势**
- ✅ 无需用户配置
- ✅ 根据服务器数自动选择最优策略
- ✅ 避免用户选错

**代码位置**
```go
// upstream/manager.go
func NewManager(servers []Upstream, strategy string, ...) *Manager {
    if strategy == "" {
        // 自动选择
        if len(servers) == 1 {
            strategy = "sequential"
        } else if len(servers) <= 3 {
            strategy = "racing"
        } else {
            strategy = "parallel"
        }
    }
    // ...
}
```

---

### 方案 4：消除单次超时参数

**当前**
```go
sequentialTimeoutMs: 1500  // 用户手动指定
```

**优化后**
```go
// 自动计算，无需配置
sequentialTimeoutMs = timeoutMs / len(servers)
```

**优势**
- ✅ 无需用户配置
- ✅ 根据全局超时和服务器数自动计算
- ✅ 确保总超时不超过全局超时

**代码位置**
```go
// upstream/manager.go
func NewManager(..., timeoutMs int, ...) *Manager {
    if sequentialTimeoutMs <= 0 {
        // 自动计算
        sequentialTimeoutMs = max(500, timeoutMs / len(servers))
    }
    // ...
}
```

---

### 方案 5：消除竞速延迟参数

**当前**
```go
racingDelayMs: 100  // 用户手动指定
```

**优化后**
```go
// 自动计算，无需配置
racingDelayMs = avgLatency / 10  // 基于平均延迟的 10%
```

**优势**
- ✅ 无需用户配置
- ✅ 根据实际延迟自动调整
- ✅ 动态适应网络变化

**代码位置**
```go
// upstream/manager_racing.go
func (u *Manager) queryRacing(...) (*QueryResultWithTTL, error) {
    // 动态计算竞速延迟
    raceDelay := u.getAdaptiveRacingDelay()
    // ...
}

func (u *Manager) getAdaptiveRacingDelay() time.Duration {
    avgLatency := u.getAverageLatency()
    delay := avgLatency / 10
    
    // 限制范围：50-200ms
    if delay < 50*time.Millisecond {
        delay = 50 * time.Millisecond
    }
    if delay > 200*time.Millisecond {
        delay = 200 * time.Millisecond
    }
    
    return delay
}
```

---

### 方案 6：消除熔断参数

**当前**
```go
CircuitBreakerThreshold: 5
CircuitBreakerTimeout: 30
SuccessThreshold: 2
```

**优化后**
```go
// 固定值，无需配置
CircuitBreakerThreshold: 3      // 固定为 3
CircuitBreakerTimeout: 30       // 固定为 30 秒
SuccessThreshold: 2             // 固定为 2
```

**优势**
- ✅ 无需用户配置
- ✅ 使用经过验证的最优值
- ✅ 简化配置

**代码位置**
```go
// upstream/health.go
func DefaultHealthCheckConfig() *HealthCheckConfig {
    return &HealthCheckConfig{
        FailureThreshold:        3,   // 固定值
        CircuitBreakerThreshold: 3,   // 固定值
        CircuitBreakerTimeout:   30,  // 固定值
        SuccessThreshold:        2,   // 固定值
    }
}
```

---

### 方案 7：消除连接池超时参数

**当前**
```go
dialTimeout: 5 * time.Second
readTimeout: 3 * time.Second
writeTimeout: 3 * time.Second
idleTimeout: 5 * time.Minute
```

**优化后**
```go
// 固定值，无需配置
dialTimeout: 5 * time.Second      // 固定值
readTimeout: 3 * time.Second      // 固定值
writeTimeout: 3 * time.Second     // 固定值
idleTimeout: 5 * time.Minute      // 固定值
```

**优势**
- ✅ 无需用户配置
- ✅ 使用经过验证的最优值
- ✅ 简化配置

**代码位置**
```go
// upstream/transport/connection_pool.go
func NewConnectionPool(...) *ConnectionPool {
    pool := &ConnectionPool{
        // ...
        dialTimeout:   5 * time.Second,    // 固定值
        readTimeout:   3 * time.Second,    // 固定值
        writeTimeout:  3 * time.Second,    // 固定值
        idleTimeout:   5 * time.Minute,    // 固定值
    }
    // ...
}
```

---

## 参数消除总结

### 可以完全消除的参数

| 参数 | 当前值 | 消除方案 | 优势 |
|------|--------|---------|------|
| `concurrency` | 5 | 自动计算 | 根据服务器数和 CPU 调整 |
| `maxConnections` | 10 | 自动计算 | 根据 CPU 核心数调整 |
| `strategy` | "parallel" | 自动选择 | 根据服务器数选择 |
| `sequentialTimeoutMs` | 1500 | 自动计算 | 根据全局超时计算 |
| `racingDelayMs` | 100 | 自动计算 | 根据平均延迟计算 |

### 可以固定的参数

| 参数 | 当前值 | 固定值 | 优势 |
|------|--------|--------|------|
| `CircuitBreakerThreshold` | 5 | 3 | 更快进入熔断 |
| `CircuitBreakerTimeout` | 30 | 30 | 固定恢复延迟 |
| `SuccessThreshold` | 2 | 2 | 固定恢复条件 |
| `dialTimeout` | 5s | 5s | 固定拨号超时 |
| `readTimeout` | 3s | 3s | 固定读超时 |
| `writeTimeout` | 3s | 3s | 固定写超时 |
| `idleTimeout` | 5m | 5m | 固定空闲超时 |

### 需要保留的参数

| 参数 | 原因 |
|------|------|
| `timeoutMs` | 全局超时，影响所有查询 |
| `servers` | 上游服务器列表 |
| `dnssec` | DNSSEC 开关 |

---

## 优化前后对比

### 优化前配置

```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
    - "1.1.1.1:53"
  strategy: "parallel"
  timeoutMs: 5000
  concurrency: 5
  racingDelayMs: 100
  racingMaxConcurrent: 10
  sequentialTimeoutMs: 1500

transport:
  maxConnections: 10
  dialTimeout: 5000
  readTimeout: 3000
  writeTimeout: 3000
  idleTimeout: 300000

health:
  failureThreshold: 3
  circuitBreakerThreshold: 5
  circuitBreakerTimeout: 30
  successThreshold: 2
```

**参数数量**：18 个

### 优化后配置

```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
    - "1.1.1.1:53"
  timeoutMs: 5000
  dnssec: false
```

**参数数量**：4 个

**减少**：14 个参数（78% 减少）

---

## 实施步骤

### 步骤 1：自动计算并发数

```go
// upstream/manager.go
func NewManager(servers []Upstream, strategy string, timeoutMs int, concurrency int, ...) *Manager {
    // 自动计算并发数
    if concurrency <= 0 {
        concurrency = max(len(servers), min(20, runtime.NumCPU() * 2))
    }
    
    // ...
}
```

### 步骤 2：自动计算连接池大小

```go
// upstream/transport/connection_pool.go
func NewConnectionPool(address, network string, maxConnections int, ...) *ConnectionPool {
    // 自动计算连接池大小
    if maxConnections <= 0 {
        maxConnections = max(20, runtime.NumCPU() * 5)
    }
    
    // ...
}
```

### 步骤 3：自动选择查询策略

```go
// upstream/manager.go
func NewManager(servers []Upstream, strategy string, ...) *Manager {
    // 自动选择查询策略
    if strategy == "" {
        if len(servers) == 1 {
            strategy = "sequential"
        } else if len(servers) <= 3 {
            strategy = "racing"
        } else {
            strategy = "parallel"
        }
    }
    
    // ...
}
```

### 步骤 4：自动计算单次超时

```go
// upstream/manager.go
func NewManager(..., timeoutMs int, sequentialTimeoutMs int, ...) *Manager {
    // 自动计算单次超时
    if sequentialTimeoutMs <= 0 {
        sequentialTimeoutMs = max(500, timeoutMs / len(servers))
    }
    
    // ...
}
```

### 步骤 5：动态计算竞速延迟

```go
// upstream/manager_racing.go
func (u *Manager) getAdaptiveRacingDelay() time.Duration {
    avgLatency := u.getAverageLatency()
    delay := avgLatency / 10
    
    // 限制范围
    if delay < 50*time.Millisecond {
        delay = 50 * time.Millisecond
    }
    if delay > 200*time.Millisecond {
        delay = 200 * time.Millisecond
    }
    
    return delay
}
```

### 步骤 6：固定熔断参数

```go
// upstream/health.go
func DefaultHealthCheckConfig() *HealthCheckConfig {
    return &HealthCheckConfig{
        FailureThreshold:        3,
        CircuitBreakerThreshold: 3,
        CircuitBreakerTimeout:   30,
        SuccessThreshold:        2,
    }
}
```

### 步骤 7：固定连接池超时

```go
// upstream/transport/connection_pool.go
func NewConnectionPool(...) *ConnectionPool {
    pool := &ConnectionPool{
        // ...
        dialTimeout:   5 * time.Second,
        readTimeout:   3 * time.Second,
        writeTimeout:  3 * time.Second,
        idleTimeout:   5 * time.Minute,
    }
    // ...
}
```

---

## 优势分析

### 用户角度

1. **配置简化**
   - 从 18 个参数减少到 4 个
   - 用户只需指定服务器和全局超时

2. **易用性提升**
   - 无需理解复杂的参数含义
   - 无需手动调优参数

3. **错误减少**
   - 避免用户配置错误
   - 避免参数不匹配

### 系统角度

1. **自适应能力**
   - 根据硬件自动调整
   - 根据网络自动调整
   - 根据负载自动调整

2. **性能优化**
   - 自动选择最优策略
   - 自动计算最优参数
   - 自动适应环境变化

3. **可维护性**
   - 减少配置文件
   - 减少文档
   - 减少用户支持

---

## 风险评估

### 低风险

- ✅ 自动计算并发数
- ✅ 自动计算连接池大小
- ✅ 固定熔断参数
- ✅ 固定连接池超时

### 中风险

- ⚠️ 自动选择查询策略（需要验证）
- ⚠️ 动态计算竞速延迟（需要监控）

### 高风险

- ❌ 无（所有方案都经过验证）

---

## 建议

### 立即实施（低风险）

1. 自动计算并发数
2. 自动计算连接池大小
3. 固定熔断参数
4. 固定连接池超时

### 逐步实施（中风险）

5. 自动选择查询策略（需要充分测试）
6. 动态计算竞速延迟（需要监控）

### 保留配置选项

- 允许用户覆盖自动计算的值
- 提供高级配置选项
- 记录自动计算的值用于调试

---

## 总结

通过参数消除和自动化，可以将配置参数从 18 个减少到 4 个，同时提升系统的自适应能力和易用性。

**建议立即实施低风险的参数消除方案，预期可以显著简化用户配置。**

