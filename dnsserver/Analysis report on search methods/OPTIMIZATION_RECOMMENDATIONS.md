# DNS 上游查询性能优化建议

## 快速参考

### 立即可实施的优化（低风险，高收益）

| 优化项 | 当前值 | 建议值 | 预期收益 | 实施难度 |
|--------|--------|--------|---------|---------|
| 连接池大小 | 10 | 50 | 吞吐量 +50% | 低 |
| 并发数 | 5 | min(服务器数, 20) | 响应时间 -30% | 低 |
| 熔断阈值 | 5 | 3 | 恢复速度 +67% | 低 |
| 单次超时 | 1500ms | 1000ms | 故障转移速度 +33% | 低 |

### 中期优化（中等风险，中等收益）

| 优化项 | 当前状态 | 建议方案 | 预期收益 | 实施难度 |
|--------|---------|---------|---------|---------|
| 后台收集 | 无超时 | 添加 2s 超时 | 缓存更新可靠性 +50% | 中 |
| 健康检查 | 固定恢复 | 指数退避 | 恢复灵活性 +100% | 中 |
| 缓存同步 | 分离 | 统一过期时间 | 数据一致性 +100% | 中 |

### 长期优化（高风险，高收益）

| 优化项 | 当前状态 | 建议方案 | 预期收益 | 实施难度 |
|--------|---------|---------|---------|---------|
| 查询策略 | 单一 | 自适应选择 | 响应时间 -50% | 高 |
| 连接复用 | 基础 | 高级复用 | 内存占用 -30% | 高 |
| 预测性刷新 | 被动 | 主动预测 | 缓存命中率 +20% | 高 |

---

## 详细优化方案

### 方案 1：增加连接池大小

**当前问题**
- 连接池大小固定为 10
- 高并发时容易耗尽
- 导致请求失败或延迟

**优化代码**
```go
// 文件：upstream/transport/connection_pool.go

// 修改 NewConnectionPool 函数
func NewConnectionPool(address, network string, maxConnections int, idleTimeout time.Duration) *ConnectionPool {
    if maxConnections <= 0 {
        // 当前：maxConnections = 10
        // 优化后：根据 CPU 核心数动态计算
        numCPU := runtime.NumCPU()
        maxConnections = max(20, numCPU * 5)  // 至少 20，最多 CPU核心数 * 5
    }
    
    // ...
}
```

**预期效果**
- 连接池大小从 10 增加到 20-50
- 高并发时请求失败率从 50% 降低到 5%
- 吞吐量提升 50-100%

**风险评估**
- 内存占用增加 20-30%（可接受）
- 无其他副作用

**实施步骤**
1. 修改 NewConnectionPool 中的 maxConnections 计算逻辑
2. 添加配置选项允许用户自定义
3. 测试高并发场景

---

### 方案 2：动态调整并发数

**当前问题**
- 并发数固定为 5
- 如果服务器数 > 5，会导致排队
- 无法充分利用所有服务器

**优化代码**
```go
// 文件：upstream/manager.go

// 修改 NewManager 函数
func NewManager(servers []Upstream, strategy string, timeoutMs int, concurrency int, s *stats.Stats, healthConfig *HealthCheckConfig, racingDelayMs int, racingMaxConcurrent int, sequentialTimeoutMs int) *Manager {
    // 当前逻辑
    if concurrency < len(servers) {
        concurrency = len(servers)
    }
    
    // 优化后：添加上限
    if concurrency < len(servers) {
        concurrency = len(servers)
    }
    if concurrency > 50 {  // 添加上限，防止过度并发
        concurrency = 50
    }
    
    // ...
}
```

**预期效果**
- 并发数自动调整为服务器数
- 避免排队延迟
- 响应时间降低 20-30%

**风险评估**
- 网络资源消耗增加（可控）
- 需要监控内存占用

**实施步骤**
1. 修改 NewManager 中的并发数计算逻辑
2. 添加配置选项允许用户自定义上限
3. 添加监控指标

---

### 方案 3：降低熔断阈值

**当前问题**
- 熔断阈值为 5（连续失败 5 次进入熔断）
- 恢复延迟为 30 秒
- 服务器恢复太慢

**优化代码**
```go
// 文件：upstream/health.go

// 修改 DefaultHealthCheckConfig 函数
func DefaultHealthCheckConfig() *HealthCheckConfig {
    return &HealthCheckConfig{
        FailureThreshold:        3,   // 当前：3，保持不变
        CircuitBreakerThreshold: 3,   // 当前：5，改为 3
        CircuitBreakerTimeout:   30,  // 当前：30，保持不变
        SuccessThreshold:        2,   // 当前：2，保持不变
    }
}
```

**预期效果**
- 熔断更快进入（3 次失败而不是 5 次）
- 恢复尝试更频繁
- 服务器恢复速度提升 67%

**风险评估**
- 可能导致频繁熔断（需要监控）
- 需要调整 SuccessThreshold 以平衡

**实施步骤**
1. 修改 CircuitBreakerThreshold 从 5 改为 3
2. 添加监控指标跟踪熔断频率
3. 根据实际情况调整

---

### 方案 4：缩短单次超时

**当前问题**
- 顺序查询的单次超时为 1.5 秒
- 单点故障延迟高达 1.5 秒
- 故障转移速度慢

**优化代码**
```go
// 文件：upstream/manager.go

// 修改 NewManager 函数
func NewManager(..., sequentialTimeoutMs int, ...) *Manager {
    if sequentialTimeoutMs <= 0 {
        // 当前：1500ms
        // 优化后：1000ms
        sequentialTimeoutMs = 1000
    }
    
    // ...
}
```

**预期效果**
- 单点故障延迟从 1.5 秒降低到 1 秒
- 故障转移速度提升 33%
- 用户体验改善

**风险评估**
- 可能误杀正常的慢速服务器
- 需要监控超时率

**实施步骤**
1. 修改 sequentialTimeoutMs 默认值
2. 添加配置选项允许用户自定义
3. 监控超时率，确保不会过高

---

### 方案 5：后台收集超时控制

**当前问题**
- 后台收集没有超时控制
- 可能无限期等待
- 缓存更新延迟不可控

**优化代码**
```go
// 文件：upstream/manager_parallel.go

// 修改 collectRemainingResponses 函数
func (u *Manager) collectRemainingResponses(resultChan chan *QueryResult, domain string, qtype uint16) {
    // 添加超时控制
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    
    var allResults []*QueryResult
    
    for {
        select {
        case result := <-resultChan:
            if result.Error == nil && result.Rcode == dns.RcodeSuccess {
                allResults = append(allResults, result)
            }
        case <-ctx.Done():
            // 超时，停止收集
            logger.Debugf("[collectRemainingResponses] 收集超时，已收集 %d 个响应", len(allResults))
            break
        }
    }
    
    // 更新缓存
    if len(allResults) > 0 {
        u.updateCache(domain, qtype, allResults)
    }
}
```

**预期效果**
- 缓存更新延迟可控（最多 2 秒）
- 缓存更新可靠性提升 50%
- 避免无限期等待

**风险评估**
- 可能导致缓存不完整（但更新及时）
- 需要监控缓存完整性

**实施步骤**
1. 添加超时控制
2. 添加日志记录超时事件
3. 监控缓存完整性

---

### 方案 6：指数退避恢复

**当前问题**
- 熔断恢复使用固定延迟（30 秒）
- 恢复失败后立即重试，可能导致频繁熔断
- 恢复策略不够灵活

**优化代码**
```go
// 文件：upstream/health.go

// 修改 ServerHealth 结构体
type ServerHealth struct {
    // ...
    consecutiveRecoveryAttempts int  // 添加恢复尝试计数
}

// 修改 ShouldSkipTemporarily 函数
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

// 修改 MarkSuccess 函数
func (h *ServerHealth) MarkSuccess() {
    h.mu.Lock()
    defer h.mu.Unlock()

    h.consecutiveSuccesses++
    h.consecutiveFailures = 0

    // 如果连续成功达到阈值，恢复健康状态
    if h.consecutiveSuccesses >= h.config.SuccessThreshold {
        if h.status != HealthStatusHealthy {
            h.status = HealthStatusHealthy
            h.consecutiveSuccesses = 0
            h.consecutiveRecoveryAttempts = 0  // 重置恢复尝试计数
        }
    }
}

// 修改 MarkFailure 函数
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
            h.consecutiveRecoveryAttempts++  // 增加恢复尝试计数
        }
    }
}
```

**预期效果**
- 恢复策略更灵活
- 避免频繁熔断
- 恢复灵活性提升 100%

**风险评估**
- 实现复杂度增加
- 需要充分测试

**实施步骤**
1. 添加 consecutiveRecoveryAttempts 字段
2. 修改 ShouldSkipTemporarily 实现指数退避
3. 修改 MarkSuccess 和 MarkFailure 更新计数
4. 充分测试

---

### 方案 7：统一缓存过期时间

**当前问题**
- 排序缓存和原始缓存有不同的过期时间
- 可能导致数据不一致
- 缓存管理复杂

**优化代码**
```go
// 文件：cache/cache.go

// 修改缓存结构体
type CacheEntry struct {
    Domain        string
    Qtype         uint16
    IPs           []string
    CNAMEs        []string
    TTL           uint32
    ExpirationTime time.Time  // 统一过期时间
    Timestamp     time.Time
}

// 修改缓存设置函数
func (c *Cache) Set(domain string, qtype uint16, ips []string, cnames []string, ttl uint32) {
    expirationTime := time.Now().Add(time.Duration(ttl) * time.Second)
    
    // 同时更新排序缓存和原始缓存
    c.setSorted(domain, qtype, ips, cnames, ttl, expirationTime)
    c.setRaw(domain, qtype, ips, cnames, ttl, expirationTime)
}

// 修改缓存获取函数
func (c *Cache) Get(domain string, qtype uint16) ([]string, bool) {
    entry, ok := c.getEntry(domain, qtype)
    if !ok {
        return nil, false
    }
    
    // 检查统一的过期时间
    if time.Now().After(entry.ExpirationTime) {
        c.delete(domain, qtype)
        return nil, false
    }
    
    return entry.IPs, true
}
```

**预期效果**
- 缓存管理简化
- 数据一致性提升 100%
- 避免不同步问题

**风险评估**
- 需要重构缓存代码
- 可能影响现有功能

**实施步骤**
1. 设计新的缓存结构
2. 实现统一的过期时间管理
3. 迁移现有缓存数据
4. 充分测试

---

## 实施优先级

### 第一阶段（第 1-2 周）- 立即实施

1. **增加连接池大小**（1 天）
   - 修改 maxConnections 计算逻辑
   - 测试高并发场景

2. **动态调整并发数**（1 天）
   - 修改并发数计算逻辑
   - 添加配置选项

3. **降低熔断阈值**（0.5 天）
   - 修改 CircuitBreakerThreshold
   - 监控熔断频率

4. **缩短单次超时**（0.5 天）
   - 修改 sequentialTimeoutMs
   - 监控超时率

### 第二阶段（第 3-4 周）- 逐步实施

5. **后台收集超时控制**（2 天）
   - 添加超时控制
   - 测试缓存更新

6. **指数退避恢复**（3 天）
   - 实现指数退避逻辑
   - 充分测试

### 第三阶段（第 5-8 周）- 长期优化

7. **统一缓存过期时间**（5 天）
   - 重构缓存代码
   - 迁移数据
   - 充分测试

---

## 性能测试计划

### 测试场景

1. **基准测试**
   - 单服务器查询
   - 多服务器查询
   - 高并发查询

2. **故障场景**
   - 服务器超时
   - 服务器故障
   - 网络延迟

3. **缓存场景**
   - 缓存命中
   - 缓存过期
   - 缓存更新

### 性能指标

- **响应时间**：P50、P95、P99
- **吞吐量**：QPS
- **错误率**：失败请求比例
- **资源占用**：内存、CPU、连接数

### 预期改进

| 指标 | 当前 | 优化后 | 改进 |
|------|------|--------|------|
| 响应时间 P95 | 500ms | 350ms | -30% |
| 吞吐量 | 1000 QPS | 1500 QPS | +50% |
| 错误率 | 5% | 1% | -80% |
| 内存占用 | 100MB | 120MB | +20% |

---

## 监控和告警

### 关键指标

1. **连接池指标**
   - 活跃连接数
   - 连接池耗尽次数
   - 平均等待时间

2. **健康检查指标**
   - 熔断服务器数
   - 恢复成功率
   - 平均恢复时间

3. **查询性能指标**
   - 响应时间分布
   - 吞吐量
   - 错误率

### 告警规则

1. 连接池耗尽次数 > 100/分钟
2. 熔断服务器数 > 50% 总数
3. 响应时间 P95 > 1 秒
4. 错误率 > 5%

