# 递归DNS解析器快速开始指南

## 快速概览

递归DNS解析器模块提供了完整的DNS递归查询功能，包括配置管理、通信层、缓存、统计和服务器实现。

## 基本使用

### 1. 创建配置

```go
package main

import (
    "log"
    "smartdnssort/resolver"
)

func main() {
    // 获取默认配置
    cfg := resolver.GetDefaults()
    
    // 或从文件加载配置（不存在时自动创建）
    cfg, err := resolver.LoadConfig("resolver.yaml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    // 修改配置
    cfg.Server.Mode = "recursive"
    cfg.Resolver.Cache.Size = 5000
    
    // 保存配置
    if err := resolver.SaveConfig("resolver.yaml", cfg); err != nil {
        log.Fatalf("Failed to save config: %v", err)
    }
}
```

### 2. 创建和启动服务器

```go
package main

import (
    "log"
    "smartdnssort/resolver"
)

func main() {
    // 加载配置
    cfg, err := resolver.LoadConfig("resolver.yaml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    // 创建服务器
    server, err := resolver.NewServer(cfg)
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }
    
    // 启动服务器
    if err := server.Start(); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
    
    log.Println("Server started successfully")
    
    // 获取统计信息
    stats := server.GetStats()
    log.Printf("Server stats: %v", stats)
    
    // 停止服务器
    defer server.Stop()
}
```

### 3. 创建客户端并执行查询

```go
package main

import (
    "context"
    "log"
    "smartdnssort/resolver"
)

func main() {
    // 创建客户端配置
    cfg := &resolver.ServerConfig{
        Transport: "tcp",
        TCP: resolver.TCPConfig{
            ListenAddr: "127.0.0.1",
            ListenPort: 5353,
        },
        TimeoutMs: 5000,
    }
    
    // 创建客户端
    client, err := resolver.NewClient(cfg)
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()
    
    // 执行查询
    ctx := context.Background()
    
    // 查询 A 记录
    response, err := client.QueryA("example.com.")
    if err != nil {
        log.Fatalf("Query failed: %v", err)
    }
    
    log.Printf("Response: %v", response)
    
    // 或使用其他便捷方法
    // response, err := client.QueryAAAA("example.com.")
    // response, err := client.QueryMX("example.com.")
    // response, err := client.QueryCNAME("example.com.")
    // response, err := client.QueryTXT("example.com.")
}
```

## 配置参数说明

### 服务器配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| transport | string | auto | 传输方式：auto/unix/tcp |
| timeout_ms | int | 5000 | 查询超时时间（毫秒） |
| mode | string | recursive | 工作模式：recursive/forwarding/hybrid |

### 解析器配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| cache.size | int | 10000 | 缓存大小 |
| cache.expiry | bool | true | 是否启用TTL过期 |
| max_depth | int | 30 | 最大递归深度 |

### 性能配置

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| workers | int | 4 | 工作协程数 |
| max_concurrent | int | 100 | 最大并发查询数 |

## 工作模式

### 递归模式 (recursive)

对所有查询使用递归解析，从根服务器开始迭代查询。

```yaml
server:
  mode: recursive
```

### 转发模式 (forwarding)

对所有查询使用转发模式，将查询转发到上游DNS服务器。

```yaml
server:
  mode: forwarding
```

### 混合模式 (hybrid)

根据域名规则选择递归或转发模式。

```yaml
server:
  mode: hybrid

hybrid_rules:
  recursive_domains:
    - example.com
    - "*.example.org"
  forward_domains:
    - google.com
  default: recursive
```

## 通信方式

### 自动选择 (auto)

系统自动检测操作系统并选择最优方式：
- Windows: TCP
- Linux/macOS: Unix Domain Socket（如果可用），否则TCP

```yaml
server:
  transport: auto
```

### Unix Domain Socket

高性能的进程间通信方式（仅在Linux/macOS上可用）。

```yaml
server:
  transport: unix
  unix_socket:
    path: /tmp/smartdns-resolver.sock
    permissions: "0600"
```

### TCP

跨平台的网络通信方式。

```yaml
server:
  transport: tcp
  tcp:
    listen_addr: 127.0.0.1
    listen_port: 5353
```

## 常见操作

### 检查连接状态

```go
client, _ := resolver.NewClient(cfg)

if client.IsConnected() {
    log.Println("Connected to resolver")
} else {
    log.Println("Not connected")
}
```

### 执行带超时的查询

```go
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()

response, err := client.QueryWithContext(ctx, msg)
```

### 获取统计信息

```go
stats := server.GetStats()

resolver_stats := stats["resolver"].(map[string]interface{})
log.Printf("Total queries: %v", resolver_stats["total_queries"])
log.Printf("Success rate: %v%%", resolver_stats["success_rate"])
log.Printf("Average latency: %v ms", resolver_stats["avg_latency_ms"])
```

### 清空缓存

```go
resolver := server.GetResolver()
resolver.ClearCache()
```

### 重置统计信息

```go
resolver := server.GetResolver()
resolver.ResetStats()
```

## 错误处理

所有操作都返回错误，应该进行适当的错误处理：

```go
cfg, err := resolver.LoadConfig("resolver.yaml")
if err != nil {
    // 处理错误
    log.Fatalf("Failed to load config: %v", err)
}

server, err := resolver.NewServer(cfg)
if err != nil {
    // 处理错误
    log.Fatalf("Failed to create server: %v", err)
}

if err := server.Start(); err != nil {
    // 处理错误
    log.Fatalf("Failed to start server: %v", err)
}
```

## 支持的DNS记录类型

客户端支持以下DNS记录类型的便捷查询方法：

- `QueryA()` - A记录（IPv4地址）
- `QueryAAAA()` - AAAA记录（IPv6地址）
- `QueryMX()` - MX记录（邮件交换）
- `QueryCNAME()` - CNAME记录（规范名称）
- `QueryTXT()` - TXT记录（文本记录）
- `QueryNS()` - NS记录（名称服务器）
- `QuerySOA()` - SOA记录（授权开始）
- `QuerySRV()` - SRV记录（服务记录）
- `QueryPTR()` - PTR记录（指针记录）
- `QueryCAA()` - CAA记录（认证机构授权）

## 测试

运行单元测试：

```bash
go test -v ./resolver
```

运行特定测试：

```bash
go test -v ./resolver -run TestNewServer
```

## 更多信息

- 详细文档: 查看 `resolver/README.md`
- 实现总结: 查看 `resolver/IMPLEMENTATION_SUMMARY.md`
- 源代码: 查看各个 `.go` 文件中的注释

## 常见问题

### Q: 如何在Windows上使用Unix Domain Socket？
A: Windows不支持Unix Domain Socket。系统会自动降级到TCP。

### Q: 如何提高查询性能？
A: 
1. 增加缓存大小
2. 增加工作协程数
3. 使用Unix Domain Socket（Linux/macOS）
4. 调整超时时间

### Q: 如何监控系统性能？
A: 使用 `GetStats()` 方法获取详细的统计信息，包括查询数、成功率、延迟等。

### Q: 如何处理查询超时？
A: 使用 `QueryWithContext()` 方法并设置合适的超时时间。

---

**更新时间**: 2026年1月5日
