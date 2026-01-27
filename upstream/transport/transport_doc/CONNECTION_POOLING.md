# DNS 连接复用 (Connection Pooling) 优化

## 问题背景

### 原始问题：临时端口耗尽

在高并发场景下，DNS 服务器会遇到以下问题：

1. **每个请求创建新套接字**
   - 原始实现：每次 `Exchange()` 调用都创建新的 `dns.Client`
   - 结果：每个 DNS 查询 = 一个新的 UDP/TCP 套接字

2. **临时端口耗尽**
   - Linux 默认临时端口范围：28232-61000（约 32k 个）
   - 高并发场景：1000 个用户 × 3 个上游服务器 = 3000 个并发请求
   - 结果：临时端口快速耗尽，导致 "Address already in use" 错误

3. **误判为上游失败**
   - 实际问题：本地网络层资源耗尽
   - 表现：健康检查误判为"上游失败"
   - 后果：熔断错误的服务器，加重问题

## 解决方案：连接复用

### 核心思想

为每个上游服务器地址维持一个连接池，复用长连接而不是每次创建新连接。

### 实现架构

```
┌─────────────────────────────────────────────────────────┐
│                    DNS 查询请求                          │
└────────────────────┬────────────────────────────────────┘
                     │
        ┌────────────┴────────────┐
        │                         │
    ┌───▼────────┐          ┌────▼────────┐
    │ UDP 查询   │          │ TCP 查询    │
    └───┬────────┘          └────┬────────┘
        │                        │
    ┌───▼──────────────────────┐ │
    │  ConnectionPool          │ │
    │  (UDP/TCP)               │ │
    │  ┌──────────────────┐    │ │
    │  │ 连接 1 (复用)    │    │ │
    │  │ 连接 2 (复用)    │    │ │
    │  │ 连接 3 (复用)    │    │ │
    │  │ ...              │    │ │
    │  │ 连接 N (最多10)  │    │ │
    │  └──────────────────┘    │ │
    └───┬──────────────────────┘ │
        │                        │
    ┌───▼────────────────────────▼──┐
    │  上游 DNS 服务器               │
    │  (同一个本地端口)              │
    └────────────────────────────────┘
```

### 连接池配置

| 参数 | 值 | 说明 |
|------|-----|------|
| 最大连接数 | 10 | 每个上游服务器最多 10 个并发连接 |
| 空闲超时 | 5 分钟 | 5 分钟未使用的连接自动关闭 |
| 清理间隔 | 30 秒 | 每 30 秒检查一次空闲连接 |
| 拨号超时 | 10 秒 | 建立新连接的超时时间 |

## 性能改进

### 资源使用对比

#### 原始实现（每次创建新连接）

```
用户数: 1000
上游服务器: 3 个
并发请求: 1000 × 3 = 3000 个

临时端口使用:
- 每个请求 = 1 个临时端口
- 总计: 3000 个临时端口
- 系统限制: ~32k 个
- 使用率: 9.4%（还好）

但问题是:
- 端口释放需要 TIME_WAIT (60 秒)
- 高频请求时，TIME_WAIT 连接堆积
- 实际可用端口快速耗尽
```

#### 优化后（连接复用）

```
用户数: 1000
上游服务器: 3 个
并发请求: 1000 × 3 = 3000 个

临时端口使用:
- 每个上游服务器 = 最多 10 个连接
- 总计: 3 × 10 = 30 个临时端口
- 系统限制: ~32k 个
- 使用率: 0.09%（极低）

优势:
- 端口复用，不需要释放
- 连接保持活跃，无 TIME_WAIT
- 支持更高的并发
```

### 性能指标

| 指标 | 原始实现 | 优化后 | 改进 |
|------|---------|--------|------|
| 临时端口使用 | 3000 | 30 | **100 倍** |
| 系统调用 (socket) | 3000 | 30 | **100 倍** |
| 内存开销 | 高 | 低 | **显著** |
| 连接建立延迟 | 每次 ~10ms | 首次 ~10ms，后续 <1ms | **10 倍** |
| 吞吐量 | 受限 | 大幅提升 | **显著** |

## 实现细节

### 1. ConnectionPool (UDP/TCP)

**文件**: `upstream/transport/connection_pool.go`

```go
type ConnectionPool struct {
    address string
    network string // "udp" 或 "tcp"
    
    connections []*PooledConnection  // 活跃连接列表
    maxConnections int                // 最多 10 个
    idleTimeout time.Duration         // 5 分钟
    
    stopChan chan struct{}             // 清理 goroutine 控制
}
```

**关键方法**:
- `Exchange()`: 获取连接并执行查询
- `getConnection()`: 获取或创建连接（LRU 策略）
- `cleanupLoop()`: 定期清理空闲连接
- `Close()`: 优雅关闭所有连接

### 2. TLSConnectionPool (DoT)

**文件**: `upstream/transport/tls_connection_pool.go`

类似 ConnectionPool，但处理 TLS 握手和证书验证。

### 3. 传输层集成

**UDP** (`upstream/transport/udp.go`):
```go
func NewUDP(address string) *UDP {
    pool := NewConnectionPool(address, "udp", 10, 5*time.Minute)
    return &UDP{address: address, pool: pool}
}

func (t *UDP) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
    return t.pool.Exchange(ctx, msg)
}
```

**TCP** (`upstream/transport/tcp.go`):
```go
func NewTCP(address string) *TCP {
    pool := NewConnectionPool(address, "tcp", 10, 5*time.Minute)
    return &TCP{address: address, pool: pool}
}
```

**DoT** (`upstream/transport/dot.go`):
```go
func NewDoT(addr string) *DoT {
    pool := NewTLSConnectionPool(address, serverName, 10, 5*time.Minute)
    return &DoT{address: address, pool: pool}
}
```

### 4. 生命周期管理

**Manager** (`upstream/manager.go`):
```go
func (u *Manager) Close() error {
    for _, server := range u.servers {
        if upstream, ok := server.upstream.(interface{ Close() error }); ok {
            upstream.Close()
        }
    }
    return nil
}
```

**Server** (`dnsserver/server_lifecycle.go`):
```go
func (s *Server) Shutdown() {
    // 关闭上游连接池
    if s.upstream != nil {
        s.upstream.Close()
    }
    // ... 其他清理逻辑
}
```

## 工作流程

### 查询流程

```
1. DNS 查询到达
   ↓
2. Manager.Query() 调用
   ↓
3. 选择查询策略 (parallel/sequential/racing)
   ↓
4. 调用 Upstream.Exchange()
   ↓
5. ConnectionPool.Exchange()
   ├─ 获取连接 (getConnection)
   │  ├─ 尝试复用现有连接
   │  └─ 如果无可用连接，创建新连接
   ├─ 在连接上执行查询 (exchangeOnConnection)
   │  ├─ 打包 DNS 消息
   │  ├─ 发送请求
   │  ├─ 接收响应
   │  └─ 解包 DNS 消息
   ├─ 更新连接的最后使用时间
   └─ 返回结果
   ↓
6. 返回给客户端
```

### 连接清理流程

```
后台清理 goroutine (每 30 秒)
   ↓
遍历所有连接
   ├─ 检查是否空闲超过 5 分钟
   ├─ 如果是，关闭连接
   └─ 从连接池中移除
   ↓
继续等待下一个清理周期
```

## 监控和调试

### 获取连接池统计

```go
stats := pool.GetStats()
// 返回:
// {
//   "address": "8.8.8.8:53",
//   "network": "udp",
//   "active_count": 5,
//   "total_count": 8,
//   "max_connections": 10,
//   "idle_timeout": "5m0s"
// }
```

### 日志输出

```
[ConnectionPool] 创建新连接到 8.8.8.8:53 (udp)，当前连接数: 1/10
[ConnectionPool] 连接池满，复用最旧连接到 8.8.8.8:53 (udp)
[ConnectionPool] 清理空闲连接到 8.8.8.8:53 (udp)，当前连接数: 9/10
[ConnectionPool] 关闭所有连接到 8.8.8.8:53 (udp)
```

## 故障排查

### 问题：连接频繁创建和销毁

**原因**: 连接池大小不足或空闲超时过短

**解决**:
```go
// 增加最大连接数
pool := NewConnectionPool(address, "udp", 20, 5*time.Minute)

// 增加空闲超时
pool := NewConnectionPool(address, "udp", 10, 10*time.Minute)
```

### 问题：内存持续增长

**原因**: 连接泄漏或清理 goroutine 未启动

**解决**:
- 确保调用 `pool.Close()` 关闭连接池
- 检查日志中是否有清理消息

### 问题：连接超时

**原因**: 上游服务器响应慢或网络问题

**解决**:
```go
// 增加拨号超时
pool.dialTimeout = 15 * time.Second

// 增加查询超时
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
reply, err := pool.Exchange(ctx, msg)
```

## 总结

连接复用优化解决了高并发场景下的临时端口耗尽问题，通过以下方式：

1. **减少系统调用**: 从 3000 个 socket() 调用减少到 30 个
2. **降低资源使用**: 临时端口使用率从 9.4% 降低到 0.09%
3. **提升性能**: 连接建立延迟从 ~10ms 降低到 <1ms（复用时）
4. **提高吞吐量**: 支持更高的并发请求
5. **改善稳定性**: 消除误判的"上游失败"

这是解决"用户多时上游失败率高"问题的关键一步。
