# Parallel DNS 查询快速响应机制优化

## 问题描述

在使用 `parallel` 模式查询上游 DNS 服务器时，原有实现存在以下问题：

1. **首次响应延迟过大**：程序会等待所有上游服务器返回结果（或超时）后才向用户发送第一次响应
2. **无法充分利用并发优势**：虽然并发查询多个服务器，但用户感知不到速度提升

## 解决方案

实现了真正的**三段式快速响应机制**：

### 阶段一：Fast Response（快速响应）
- 同时向所有上游服务器发送 DNS 查询请求
- **当第一个服务器返回成功结果时，立即向用户发送响应**
- 在后台继续等待其他服务器的响应

### 阶段二：Background Collection（后台收集）
- 继续收集其他服务器的响应结果
- 将所有成功响应的 IP 地址去重合并
- 通过回调机制更新缓存中的完整 IP 池
- 触发异步 Ping 排序

### 阶段三：Sorted Response（排序后响应）
- 用户第二次查询时，返回经过 Ping 测试排序后的最优 IP

## 代码改动

### 1. `upstream/manager.go`

#### 新增字段
```go
type Manager struct {
    // ...
    // 缓存更新回调函数，用于在 parallel 模式下后台收集完所有响应后更新缓存
    cacheUpdateCallback func(domain string, qtype uint16, ips []string, cname string, ttl uint32)
}
```

#### 新增方法
```go
// SetCacheUpdateCallback 设置缓存更新回调函数
func (u *Manager) SetCacheUpdateCallback(callback func(domain string, qtype uint16, ips []string, cname string, ttl uint32))
```

#### 重构 `queryParallel` 函数
- 添加 `fastResponseChan` 通道用于快速响应
- 使用 `sync.Once` 确保只发送一次快速响应
- 第一个成功的响应立即返回给用户
- 启动后台 goroutine 继续收集剩余响应

#### 新增 `collectRemainingResponses` 函数
- 在后台收集剩余服务器的响应
- 汇总并去重所有 IP 地址
- 通过回调函数更新缓存

### 2. `dnsserver/server.go`

#### 设置缓存更新回调
在 `NewServer` 函数中添加：
```go
server.upstream.SetCacheUpdateCallback(func(domain string, qtype uint16, ips []string, cname string, ttl uint32) {
    // 更新原始缓存中的IP列表
    server.cache.SetRaw(domain, qtype, ips, cname, ttl)
    
    // 触发异步排序，更新排序缓存
    go server.sortIPsAsync(domain, qtype, ips, ttl, time.Now())
})
```

## 工作流程示例

假设配置了 3 个上游 DNS 服务器（A、B、C），查询 `www.example.com`：

```
时间轴：
T0: 用户发起查询
T1: 同时向 A、B、C 发送 DNS 查询
T2: 服务器 B 首先返回结果 [1.1.1.1, 2.2.2.2]
    ✅ 立即向用户返回这 2 个 IP（Fast Response）
    🔄 后台继续等待 A 和 C 的响应
T3: 服务器 A 返回结果 [1.1.1.1, 3.3.3.3]
    📝 记录到后台收集列表
T4: 服务器 C 超时
    📝 记录失败
T5: 所有服务器响应完成
    🔄 合并去重：[1.1.1.1, 2.2.2.2, 3.3.3.3]
    📝 更新缓存
    🎯 触发 Ping 排序

用户第二次查询：
    ✅ 返回排序后的最优 IP
```

## 优势

1. **响应速度提升**：用户感知到的延迟大幅降低（从最慢服务器的响应时间降低到最快服务器的响应时间）
2. **IP 池更完整**：后台收集所有服务器的响应，获得更全面的 IP 列表
3. **智能排序**：第二次查询时返回经过 Ping 测试的最优 IP
4. **架构清晰**：通过回调机制实现模块间解耦

## 日志示例

```
[queryParallel] 并行查询 3 个服务器,查询 www.example.com (type=A),并发数=3
[queryParallel] 🚀 快速响应: 服务器 8.8.8.8:53 第一个返回成功结果，立即响应用户
[queryParallel] ✅ 收到快速响应: 服务器 8.8.8.8:53 返回 2 个IP, CNAME= (TTL=300秒): [1.1.1.1 2.2.2.2]
[collectRemainingResponses] 🔄 开始后台收集剩余响应: www.example.com (type=A)
[collectRemainingResponses] 服务器 1.1.1.1:53 查询成功(第2个成功),返回 2 个IP, CNAME= (TTL=300秒): [1.1.1.1 3.3.3.3]
[collectRemainingResponses] ✅ 后台收集完成: 从 2 个服务器收集到 3 个唯一IP (快速响应: 2 个IP, 汇总后: 3 个IP), CNAME=, TTL=300秒
[collectRemainingResponses] 完整IP池: [1.1.1.1 2.2.2.2 3.3.3.3]
[CacheUpdateCallback] 更新缓存: www.example.com (type=A), IP数量=3, CNAME=, TTL=300秒
```

## 注意事项

1. **统计准确性**：快速响应的服务器统计在主流程中记录，其他服务器的统计在后台收集中记录
2. **缓存一致性**：后台收集完成后会更新缓存，可能会覆盖快速响应的结果（通常是扩充 IP 池）
3. **并发控制**：仍然使用 `concurrency` 参数控制同时查询的服务器数量
4. **错误处理**：如果所有服务器都失败，会等待所有结果后返回错误

## 未来优化方向

1. **可配置策略**：允许用户选择是否启用快速响应模式
2. **智能选择**：根据服务器历史响应时间优先查询快速服务器
3. **增量更新**：支持增量更新缓存，而不是完全覆盖
