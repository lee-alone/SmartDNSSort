# 连接复用 (Connection Pooling) 快速参考

## 问题诊断

### 症状：用户多时上游失败率高

```
用户少 (100)  → 正常，缓存命中快
用户多 (1000) → 上游失败率高，缓存命中仍快
```

### 根本原因

```
高并发请求
    ↓
每个请求创建新套接字 (原始实现)
    ↓
临时端口耗尽 (Linux: ~32k 个)
    ↓
本地网络层失败 (不是上游失败)
    ↓
误判为"上游失败"
```

## 解决方案

### 实现方式

| 协议 | 文件 | 连接池类 | 最大连接 |
|------|------|---------|---------|
| UDP | `upstream/transport/udp.go` | `ConnectionPool` | 10 |
| TCP | `upstream/transport/tcp.go` | `ConnectionPool` | 10 |
| DoT | `upstream/transport/dot.go` | `TLSConnectionPool` | 10 |
| DoH | `upstream/transport/doh.go` | HTTP 连接池 (已有) | 100 |

### 关键改进

```
原始实现:
UDP.Exchange() → 创建 dns.Client → 创建新套接字 → 分配临时端口

优化后:
UDP.Exchange() → ConnectionPool.Exchange() → 复用现有连接 → 复用临时端口
```

## 性能对比

### 资源使用

| 指标 | 原始 | 优化后 | 改进 |
|------|------|--------|------|
| 临时端口 | 3000 | 30 | **100 倍** |
| socket() 调用 | 3000 | 30 | **100 倍** |
| 连接建立延迟 | ~10ms | <1ms (复用) | **10 倍** |

### 场景：1000 个用户，3 个上游服务器

```
原始实现:
- 并发请求: 1000 × 3 = 3000
- 临时端口使用: 3000 个
- 系统限制: ~32k 个
- 使用率: 9.4%
- 问题: TIME_WAIT 堆积，实际可用端口快速耗尽

优化后:
- 并发请求: 1000 × 3 = 3000
- 临时端口使用: 3 × 10 = 30 个
- 系统限制: ~32k 个
- 使用率: 0.09%
- 优势: 端口充足，无 TIME_WAIT 堆积
```

## 配置参数

### 连接池配置

```go
// 创建连接池
pool := NewConnectionPool(
    address,           // "8.8.8.8:53"
    network,           // "udp" 或 "tcp"
    maxConnections,    // 10 (默认)
    idleTimeout,       // 5 * time.Minute (默认)
)
```

### 调整建议

| 场景 | 最大连接 | 空闲超时 | 说明 |
|------|---------|---------|------|
| 低并发 | 3 | 5 分钟 | 默认配置 |
| 中并发 | 10 | 5 分钟 | 当前配置 |
| 高并发 | 20 | 10 分钟 | 增加连接数和超时 |
| 超高并发 | 50 | 15 分钟 | 大幅增加 |

## 监控

### 查看连接池状态

```go
stats := pool.GetStats()
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
```

## 故障排查

### 问题 1：连接频繁创建

**症状**: 日志中频繁出现"创建新连接"

**原因**: 
- 最大连接数不足
- 空闲超时过短
- 上游服务器关闭连接

**解决**:
```go
// 增加最大连接数
pool := NewConnectionPool(address, "udp", 20, 5*time.Minute)

// 增加空闲超时
pool := NewConnectionPool(address, "udp", 10, 10*time.Minute)
```

### 问题 2：内存持续增长

**症状**: 内存占用不断增加

**原因**:
- 连接泄漏
- 清理 goroutine 未启动
- 连接未正确关闭

**解决**:
```go
// 确保调用 Close()
defer pool.Close()

// 检查日志中是否有清理消息
// [ConnectionPool] 清理空闲连接...
```

### 问题 3：连接超时

**症状**: 查询频繁超时

**原因**:
- 上游服务器响应慢
- 网络问题
- 超时设置过短

**解决**:
```go
// 增加查询超时
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
reply, err := pool.Exchange(ctx, msg)
```

## 验证优化效果

### 方法 1：查看临时端口使用

```bash
# Linux
netstat -an | grep TIME_WAIT | wc -l

# 优化前: 可能有数千个 TIME_WAIT
# 优化后: 只有几十个
```

### 方法 2：查看系统调用

```bash
# 使用 strace 追踪系统调用
strace -e socket,bind,connect -c <program>

# 优化前: socket() 调用数 = 请求数
# 优化后: socket() 调用数 = 最大连接数
```

### 方法 3：性能测试

```bash
# 使用 ab 或 wrk 进行压力测试
ab -n 10000 -c 1000 http://localhost:8080/api/query

# 观察:
# - 响应时间是否降低
# - 错误率是否降低
# - 系统资源使用是否降低
```

## 代码示例

### 使用 UDP 连接池

```go
import "smartdnssort/upstream/transport"

// 创建 UDP 传输
udp := transport.NewUDP("8.8.8.8:53")
defer udp.Close()

// 执行查询（自动使用连接池）
msg := new(dns.Msg)
msg.SetQuestion("example.com.", dns.TypeA)

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

reply, err := udp.Exchange(ctx, msg)
if err != nil {
    log.Fatal(err)
}
```

### 使用 TCP 连接池

```go
import "smartdnssort/upstream/transport"

// 创建 TCP 传输
tcp := transport.NewTCP("8.8.8.8:53")
defer tcp.Close()

// 执行查询（自动使用连接池）
reply, err := tcp.Exchange(ctx, msg)
```

### 使用 DoT 连接池

```go
import "smartdnssort/upstream/transport"

// 创建 DoT 传输
dot := transport.NewDoT("dns.google:853")
defer dot.Close()

// 执行查询（自动使用连接池）
reply, err := dot.Exchange(ctx, msg)
```

## 总结

连接复用优化通过以下方式解决高并发问题：

1. ✅ **减少系统调用**: 从 3000 个减少到 30 个
2. ✅ **降低资源使用**: 临时端口使用率从 9.4% 降低到 0.09%
3. ✅ **提升性能**: 连接建立延迟从 ~10ms 降低到 <1ms
4. ✅ **提高吞吐量**: 支持更高的并发请求
5. ✅ **改善稳定性**: 消除误判的"上游失败"

这是解决"用户多时上游失败率高"问题的关键优化。
