# 递归DNS解析器使用示例

本文档提供了递归DNS解析器的实际使用示例，涵盖配置、启动、查询等常见场景。

## 目录

1. [基础配置示例](#基础配置示例)
2. [启动和停止](#启动和停止)
3. [查询示例](#查询示例)
4. [工作模式示例](#工作模式示例)
5. [混合模式配置](#混合模式配置)
6. [性能优化](#性能优化)
7. [错误处理](#错误处理)
8. [监控和统计](#监控和统计)

## 基础配置示例

### 示例1: 创建默认配置

```go
package main

import (
    "log"
    "smartdnssort/resolver"
)

func main() {
    // 获取默认配置
    cfg := resolver.GetDefaults()
    
    // 保存到文件
    if err := resolver.SaveConfig("resolver.yaml", cfg); err != nil {
        log.Fatalf("Failed to save config: %v", err)
    }
    
    log.Println("Default configuration created successfully")
}
```

### 示例2: 加载和修改配置

```go
package main

import (
    "log"
    "smartdnssort/resolver"
)

func main() {
    // 加载配置（不存在时自动创建）
    cfg, err := resolver.LoadConfig("resolver.yaml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    // 修改配置
    cfg.Server.Mode = "hybrid"
    cfg.Resolver.Cache.Size = 20000
    cfg.Performance.Workers = 8
    cfg.Logging.Level = "debug"
    
    // 验证配置
    if err := cfg.Validate(); err != nil {
        log.Fatalf("Invalid configuration: %v", err)
    }
    
    // 保存修改
    if err := resolver.SaveConfig("resolver.yaml", cfg); err != nil {
        log.Fatalf("Failed to save config: %v", err)
    }
    
    log.Println("Configuration updated successfully")
}
```

### 示例3: 配置验证

```go
package main

import (
    "log"
    "smartdnssort/resolver"
)

func main() {
    cfg, err := resolver.LoadConfig("resolver.yaml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    // 验证配置
    if err := cfg.Validate(); err != nil {
        log.Printf("Configuration validation failed: %v", err)
        log.Println("Using default configuration instead")
        cfg = resolver.GetDefaults()
    } else {
        log.Println("Configuration is valid")
    }
}
```

## 启动和停止

### 示例4: 启动服务器

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

### 示例5: 优雅关闭

```go
package main

import (
    "log"
    "os"
    "os/signal"
    "syscall"
    "smartdnssort/resolver"
)

func main() {
    cfg, err := resolver.LoadConfig("resolver.yaml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    server, err := resolver.NewServer(cfg)
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }
    
    if err := server.Start(); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
    
    log.Println("Server started. Press Ctrl+C to stop...")
    
    // 监听信号
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    
    // 等待信号
    <-sigChan
    
    log.Println("Shutting down server...")
    if err := server.Stop(); err != nil {
        log.Printf("Error stopping server: %v", err)
    }
    
    log.Println("Server stopped")
}
```

## 查询示例

### 示例6: 基本查询

```go
package main

import (
    "context"
    "log"
    "smartdnssort/resolver"
)

func main() {
    cfg := &resolver.ServerConfig{
        Transport: "tcp",
        TCP: resolver.TCPConfig{
            ListenAddr: "127.0.0.1",
            ListenPort: 5353,
        },
        TimeoutMs: 5000,
    }
    
    client, err := resolver.NewClient(cfg)
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()
    
    // 查询A记录
    response, err := client.QueryA("example.com.")
    if err != nil {
        log.Fatalf("Query failed: %v", err)
    }
    
    log.Printf("Response: %v", response)
}
```

### 示例7: 查询多种记录类型

```go
package main

import (
    "log"
    "smartdnssort/resolver"
)

func main() {
    cfg := &resolver.ServerConfig{
        Transport: "tcp",
        TCP: resolver.TCPConfig{
            ListenAddr: "127.0.0.1",
            ListenPort: 5353,
        },
        TimeoutMs: 5000,
    }
    
    client, err := resolver.NewClient(cfg)
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()
    
    // 查询A记录
    a, err := client.QueryA("example.com.")
    if err != nil {
        log.Printf("A query failed: %v", err)
    } else {
        log.Printf("A records: %v", a)
    }
    
    // 查询AAAA记录
    aaaa, err := client.QueryAAAA("example.com.")
    if err != nil {
        log.Printf("AAAA query failed: %v", err)
    } else {
        log.Printf("AAAA records: %v", aaaa)
    }
    
    // 查询MX记录
    mx, err := client.QueryMX("example.com.")
    if err != nil {
        log.Printf("MX query failed: %v", err)
    } else {
        log.Printf("MX records: %v", mx)
    }
    
    // 查询TXT记录
    txt, err := client.QueryTXT("example.com.")
    if err != nil {
        log.Printf("TXT query failed: %v", err)
    } else {
        log.Printf("TXT records: %v", txt)
    }
    
    // 查询NS记录
    ns, err := client.QueryNS("example.com.")
    if err != nil {
        log.Printf("NS query failed: %v", err)
    } else {
        log.Printf("NS records: %v", ns)
    }
}
```

### 示例8: 带超时的查询

```go
package main

import (
    "context"
    "log"
    "time"
    "smartdnssort/resolver"
)

func main() {
    cfg := &resolver.ServerConfig{
        Transport: "tcp",
        TCP: resolver.TCPConfig{
            ListenAddr: "127.0.0.1",
            ListenPort: 5353,
        },
        TimeoutMs: 5000,
    }
    
    client, err := resolver.NewClient(cfg)
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()
    
    // 创建带超时的上下文
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    
    // 执行查询
    response, err := client.QueryWithContext(ctx, "example.com.", "A")
    if err != nil {
        log.Fatalf("Query failed: %v", err)
    }
    
    log.Printf("Response: %v", response)
}
```

## 工作模式示例

### 示例9: 递归模式配置

```yaml
# resolver.yaml
server:
  transport: auto
  timeout_ms: 5000
  mode: recursive

resolver:
  cache:
    size: 10000
    expiry: true
  max_depth: 30

optimization:
  enabled: true

performance:
  workers: 4
  max_concurrent: 100

logging:
  level: info
  file: logs/resolver.log
```

### 示例10: 转发模式配置

```yaml
# resolver.yaml
server:
  transport: auto
  timeout_ms: 5000
  mode: forwarding

resolver:
  cache:
    size: 10000
    expiry: true

optimization:
  enabled: true

performance:
  workers: 4
  max_concurrent: 100

logging:
  level: info
  file: logs/resolver.log
```

## 混合模式配置

### 示例11: 混合模式 - 特定域名递归

```yaml
# resolver.yaml
server:
  transport: auto
  timeout_ms: 5000
  mode: hybrid

resolver:
  cache:
    size: 10000
    expiry: true

hybrid_rules:
  # 这些域名使用递归解析
  recursive_domains:
    - example.com
    - test.org
    - "*.internal.company.com"
  
  # 这些域名使用转发模式
  forward_domains:
    - google.com
    - github.com
  
  # 默认行为
  default: recursive

optimization:
  enabled: true

performance:
  workers: 4
  max_concurrent: 100

logging:
  level: info
  file: logs/resolver.log
```

### 示例12: 混合模式 - 特定域名转发

```yaml
# resolver.yaml
server:
  transport: auto
  timeout_ms: 5000
  mode: hybrid

resolver:
  cache:
    size: 10000
    expiry: true

hybrid_rules:
  # 这些域名使用递归解析
  recursive_domains:
    - "*.internal.company.com"
  
  # 这些域名使用转发模式
  forward_domains:
    - google.com
    - github.com
    - "*.cdn.example.com"
  
  # 默认行为：不匹配规则时使用转发
  default: forwarding

optimization:
  enabled: true

performance:
  workers: 4
  max_concurrent: 100

logging:
  level: info
  file: logs/resolver.log
```

## 性能优化

### 示例13: 高性能配置

```yaml
# resolver.yaml - 针对高并发场景
server:
  transport: unix  # 使用Unix Domain Socket获得最佳性能
  timeout_ms: 3000
  mode: recursive

resolver:
  cache:
    size: 50000  # 增加缓存大小
    expiry: true

optimization:
  enabled: true

performance:
  workers: 16  # 增加工作协程数
  max_concurrent: 500  # 增加最大并发数

logging:
  level: warn  # 降低日志级别以提高性能
  file: logs/resolver.log
```

### 示例14: 低资源配置

```yaml
# resolver.yaml - 针对低资源环境
server:
  transport: tcp
  timeout_ms: 10000
  mode: recursive

resolver:
  cache:
    size: 1000  # 减少缓存大小
    expiry: true

optimization:
  enabled: false  # 禁用IP优选以节省资源

performance:
  workers: 2  # 减少工作协程数
  max_concurrent: 20  # 减少最大并发数

logging:
  level: error  # 仅记录错误
  file: logs/resolver.log
```

## 错误处理

### 示例15: 完整的错误处理

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
        log.Printf("Failed to load config: %v", err)
        log.Println("Using default configuration")
        cfg = resolver.GetDefaults()
    }
    
    // 验证配置
    if err := cfg.Validate(); err != nil {
        log.Printf("Configuration validation failed: %v", err)
        return
    }
    
    // 创建服务器
    server, err := resolver.NewServer(cfg)
    if err != nil {
        log.Printf("Failed to create server: %v", err)
        return
    }
    
    // 启动服务器
    if err := server.Start(); err != nil {
        log.Printf("Failed to start server: %v", err)
        return
    }
    
    defer server.Stop()
    
    // 创建客户端
    clientCfg := &resolver.ServerConfig{
        Transport: cfg.Server.Transport,
        TCP: cfg.Server.TCP,
        TimeoutMs: cfg.Server.TimeoutMs,
    }
    
    client, err := resolver.NewClient(clientCfg)
    if err != nil {
        log.Printf("Failed to create client: %v", err)
        return
    }
    defer client.Close()
    
    // 执行查询
    response, err := client.QueryA("example.com.")
    if err != nil {
        log.Printf("Query failed: %v", err)
        return
    }
    
    log.Printf("Query successful: %v", response)
}
```

### 示例16: 重试机制

```go
package main

import (
    "log"
    "time"
    "smartdnssort/resolver"
)

func queryWithRetry(client *resolver.Client, domain string, maxRetries int) (interface{}, error) {
    var lastErr error
    
    for i := 0; i < maxRetries; i++ {
        response, err := client.QueryA(domain)
        if err == nil {
            return response, nil
        }
        
        lastErr = err
        log.Printf("Query attempt %d failed: %v", i+1, err)
        
        if i < maxRetries-1 {
            // 等待后重试
            time.Sleep(time.Duration(i+1) * time.Second)
        }
    }
    
    return nil, lastErr
}

func main() {
    cfg := &resolver.ServerConfig{
        Transport: "tcp",
        TCP: resolver.TCPConfig{
            ListenAddr: "127.0.0.1",
            ListenPort: 5353,
        },
        TimeoutMs: 5000,
    }
    
    client, err := resolver.NewClient(cfg)
    if err != nil {
        log.Fatalf("Failed to create client: %v", err)
    }
    defer client.Close()
    
    // 最多重试3次
    response, err := queryWithRetry(client, "example.com.", 3)
    if err != nil {
        log.Fatalf("Query failed after retries: %v", err)
    }
    
    log.Printf("Query successful: %v", response)
}
```

## 监控和统计

### 示例17: 获取统计信息

```go
package main

import (
    "log"
    "smartdnssort/resolver"
)

func main() {
    cfg, err := resolver.LoadConfig("resolver.yaml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    server, err := resolver.NewServer(cfg)
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }
    
    if err := server.Start(); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
    defer server.Stop()
    
    // 获取统计信息
    stats := server.GetStats()
    
    // 打印统计信息
    log.Printf("Server Statistics:")
    for key, value := range stats {
        log.Printf("  %s: %v", key, value)
    }
}
```

### 示例18: 定期监控

```go
package main

import (
    "log"
    "time"
    "smartdnssort/resolver"
)

func main() {
    cfg, err := resolver.LoadConfig("resolver.yaml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    server, err := resolver.NewServer(cfg)
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }
    
    if err := server.Start(); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
    defer server.Stop()
    
    // 定期输出统计信息
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        stats := server.GetStats()
        log.Printf("Server Statistics: %v", stats)
    }
}
```

### 示例19: 缓存管理

```go
package main

import (
    "log"
    "smartdnssort/resolver"
)

func main() {
    cfg, err := resolver.LoadConfig("resolver.yaml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    server, err := resolver.NewServer(cfg)
    if err != nil {
        log.Fatalf("Failed to create server: %v", err)
    }
    
    if err := server.Start(); err != nil {
        log.Fatalf("Failed to start server: %v", err)
    }
    defer server.Stop()
    
    // 获取解析器
    resolver := server.GetResolver()
    
    // 清空缓存
    resolver.ClearCache()
    log.Println("Cache cleared")
    
    // 获取统计信息
    stats := server.GetStats()
    log.Printf("Cache stats: %v", stats)
}
```

## 总结

这些示例涵盖了递归DNS解析器的主要使用场景：

1. **配置管理** - 创建、加载、修改和验证配置
2. **启动和停止** - 启动服务器和优雅关闭
3. **查询操作** - 执行各种类型的DNS查询
4. **工作模式** - 配置不同的工作模式
5. **混合模式** - 配置混合模式规则
6. **性能优化** - 针对不同场景的性能配置
7. **错误处理** - 完整的错误处理和重试机制
8. **监控统计** - 获取和监控统计信息

更多信息请参考 `README.md` 和 `QUICK_START.md`。

</content>
