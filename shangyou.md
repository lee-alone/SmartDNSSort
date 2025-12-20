
## 6. 进一步优化建议

在解决了核心的超时问题后，我们可以进一步优化服务器的选择逻辑，使其更加智能和自适应。

### 6.1. 问题点：缺少基于性能的动态服务器排序

当前的 `racing` 和 `parallel` 策略都依赖于 `getSortedHealthyServers` 函数来获取上游服务器列表。通过分析 `upstream/manager_utils.go`，我们发现该函数的实现非常基础：

```go
func (u *Manager) getSortedHealthyServers() []*HealthAwareUpstream {
    // ...
    for _, server := range u.servers {
        if !server.ShouldSkipTemporarily() {
            healthy = append(healthy, server)
        } else {
            unhealthy = append(unhealthy, server)
        }
    }
    return append(healthy, unhealthy...)
}
```

它仅仅是将服务器分为“未熔断”和“已熔断”两个组，**在“未熔断”组内部，服务器的顺序完全取决于它们在配置文件中的原始顺序，并不会根据实际查询性能（如响应延迟）进行动态调整。**

这导致了次优的服务器选择：
- **对于 `racing` 策略**：如果配置文件中的第一个服务器并非最快，系统每次都会优先查询它，并固定产生 `raceDelay`（100ms）的额外延迟，然后才能轮到更快的服务器。
- **对于 `parallel` 策略**：第一批并发查询会发往配置文件中最靠前的几个服务器。如果这几个服务器很慢，获取“快速响应”的时间就会被不必要地延长。

系统没有从过往的查询中“学习”到哪个服务器更快。

### 6.2. 解决方案：引入基于延迟的动态排序

为了让服务器选择更加智能，我们建议引入基于**指数加权移动平均 (EWMA)** 的延迟作为排序依据。这比简单的算术平均值更能反映近期的网络波动。

**第 1 步：在 `ServerHealth` 中追踪延迟**

修改 `upstream/health.go` 中的 `ServerHealth` 结构体，增加延迟相关的字段。

```go
// 在 ServerHealth 结构体中
type ServerHealth struct {
    // ... a lot of fields
    
    // 平均延迟（使用 EWMA 计算）
    latency time.Duration
    
    // EWMA 的 alpha 因子，例如 0.2
    latencyAlpha float64
}
```
在 `NewServerHealth` 中初始化 `latencyAlpha` 和初始 `latency`（可以设为一个较高的默认值）。

然后添加一个方法来记录和更新延迟：

```go
// RecordLatency 记录一次成功的查询延迟，并更新 EWMA 值
func (h *ServerHealth) RecordLatency(d time.Duration) {
    h.mu.Lock()
    defer h.mu.Unlock()

    if h.latency == 0 { // 首次记录
        h.latency = d
    } else {
        // EWMA 公式: new_avg = alpha * new_value + (1 - alpha) * old_avg
        h.latency = time.Duration(h.latencyAlpha*float64(d) + (1.0-h.latencyAlpha)*float64(h.latency))
    }
}
```

**第 2 步：在 `HealthAwareUpstream` 中调用延迟记录**

修改 `upstream/health_aware.go` 中的 `Exchange` 方法，在查询成功后记录延迟。

```go
func (h *HealthAwareUpstream) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
    startTime := time.Now()
    reply, err := h.upstream.Exchange(ctx, msg)
    latency := time.Since(startTime)

    if err != nil {
        h.health.MarkFailure()
        return nil, err
    }

    if reply.Rcode != dns.RcodeSuccess && reply.Rcode != dns.RcodeNameError {
        h.health.MarkFailure()
    } else {
        // 查询成功，记录延迟
        h.health.RecordLatency(latency)
        h.health.MarkSuccess()
    }

    return reply, nil
}
```

**第 3 步：更新服务器排序逻辑**

修改 `upstream/manager_utils.go` 中的 `getSortedHealthyServers` 函数，使用 `sort.Slice` 对“未熔断”的服务器列表按延迟进行升序排序。

```go
import "sort"

func (u *Manager) getSortedHealthyServers() []*HealthAwareUpstream {
    healthy := make([]*HealthAwareUpstream, 0, len(u.servers))
    unhealthy := make([]*HealthAwareUpstream, 0)

    for _, server := range u.servers {
        if !server.ShouldSkipTemporarily() {
            healthy = append(healthy, server)
        } else {
            unhealthy = append(unhealthy, server)
        }
    }

    // 核心改动：对“健康”列表按延迟升序排序
    sort.Slice(healthy, func(i, j int) bool {
        // GetHealth() 返回 ServerHealth 指针
        // 需要添加 GetLatency() 方法到 ServerHealth
        return healthy[i].GetHealth().GetLatency() < healthy[j].GetHealth().GetLatency()
    })

    return append(healthy, unhealthy...)
}
```
*(注意: `GetLatency()` 是需要在 `ServerHealth` 上添加的一个简单的 getter 方法，用于返回当前的 `latency` 值)*

### 6.3. 收益

通过上述改动，系统将能够：
1.  **动态自适应**：自动将近期表现更好（延迟更低）的服务器排在前面。
2.  **提升策略效率**：`racing` 和 `parallel` 策略将总是从最快的服务器开始，从而更快地获得响应，更有效地利用网络资源。
3.  **改善用户体验**：在网络波动时，系统能更快地切换到备用服务器，减少用户感知的延迟。

### 6.4. 实现注意事项

在实现基于延迟的动态排序时，有几个关键细节需要注意：

-   **线程安全**：`ServerHealth` 结构体中的 `latency` 和 EWMA 相关的字段（如 `latencyAlpha`）是共享资源。在 `RecordLatency` 方法中更新这些字段时，必须使用互斥锁（`sync.Mutex`）进行保护，确保线程安全。在 `GetLatency` 读取这些字段时，也应使用读写锁（`sync.RWMutex`）的读锁进行保护。
-   **排序频率**：`getSortedHealthyServers()` 每次被调用时都会对服务器列表进行排序。对于几十个服务器的规模，Go 语言的 `sort.Slice` 效率很高，通常不是性能瓶颈。但如果服务器数量非常庞大（例如数百或上千），或者 `getSortedHealthyServers()` 被调用的频率极高，可以考虑引入缓存机制，定期（例如每秒一次）重新排序并缓存结果，而不是每次都即时排序。
-   **EWMA 参数调优**：EWMA 的 `alpha` 因子（`latencyAlpha`）需要根据实际网络环境进行调优。
    -   `alpha` 值越大（接近 1.0），EWMA 越重视最新的查询延迟，对网络变化的响应更灵敏，但可能对瞬时波动也更敏感。
    -   `alpha` 值越小（接近 0），EWMA 越平稳，对历史数据权重更大，但对网络变化的滞后性更强。
    建议可以从 `0.1` 到 `0.3` 之间选择一个值进行测试。
-   **延迟初值处理**：当服务器首次启动或长时间没有查询记录时，其 `latency` 字段可能为 0 或未初始化。为了避免排序时出现不合理的“最优”情况，建议在 `NewServerHealth` 初始化时，将 `latency` 字段设置为一个合理的默认值（例如 `100ms` 或 `200ms`），这样新的或沉寂的服务器在首次参与排序时能有一个公平的基准。
