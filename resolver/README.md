# 递归DNS解析器模块

## 概述

`resolver` 模块提供了递归DNS解析功能，允许SmartDNSSort从根域名服务器开始执行完整的DNS递归查询。

## 功能特性

- **递归解析**: 从根服务器开始的完整DNS递归查询
- **多种工作模式**: 支持纯递归、纯转发和混合模式
- **灵活的通信方式**: 支持Unix Domain Socket和TCP通信
- **缓存管理**: 支持LRU淘汰和TTL过期管理
- **DNSSEC支持**: 可选的DNSSEC验证
- **IP优选**: 对返回的IP地址进行速度测试和排序
- **性能配置**: 可配置的工作协程数和并发限制

## 配置

### 主配置文件 (config.yaml)

```yaml
resolver:
  # 是否启用递归DNS解析器
  enabled: false
  # 递归解析器配置文件路径
  config_file: resolver.yaml
  # 传输方式：auto/unix/tcp
  transport: auto
```

### 递归解析器配置文件 (resolver.yaml)

```yaml
server:
  # 传输方式：auto（自动选择），unix（Unix Domain Socket），tcp（TCP）
  transport: auto
  # Unix Domain Socket 配置
  unix_socket:
    path: /tmp/smartdns-resolver.sock
    permissions: "0600"
  # TCP 配置
  tcp:
    listen_addr: 127.0.0.1
    listen_port: 5353
  # 查询超时时间（毫秒）
  timeout_ms: 5000
  # 工作模式：recursive/forwarding/hybrid
  mode: recursive

resolver:
  # 缓存配置
  cache:
    size: 10000
    expiry: true
  # 最大递归深度
  max_depth: 30
  # DNSSEC 配置
  dnssec:
    enabled: false
    validate: true

optimization:
  # 是否启用 IP 优选
  enabled: true

hybrid_rules:
  # 使用递归解析的域名列表
  recursive_domains: []
  # 使用转发模式的域名列表
  forward_domains: []
  # 默认行为
  default: recursive

performance:
  # 工作协程数
  workers: 4
  # 最大并发查询数
  max_concurrent: 100

logging:
  # 日志级别：debug/info/warn/error
  level: info
  # 日志文件路径
  file: logs/resolver.log
```

## 使用示例

### 加载配置

```go
package main

import (
    "log"
    "smartdnssort/resolver"
)

func main() {
    // 加载配置文件
    cfg, err := resolver.LoadConfig("resolver.yaml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }

    // 配置已加载并验证
    log.Printf("Resolver mode: %s", cfg.Server.Mode)
}
```

### 获取默认配置

```go
package main

import (
    "log"
    "smartdnssort/resolver"
)

func main() {
    // 获取默认配置
    cfg := resolver.GetDefaults()
    
    // 修改配置
    cfg.Server.Mode = "hybrid"
    
    // 保存配置
    if err := resolver.SaveConfig("resolver.yaml", cfg); err != nil {
        log.Fatalf("Failed to save config: %v", err)
    }
}
```

## 配置验证

所有配置都会在加载时自动验证。验证规则包括：

- **Transport**: 必须是 `auto`、`unix` 或 `tcp`
- **Mode**: 必须是 `recursive`、`forwarding` 或 `hybrid`
- **TimeoutMs**: 必须为正数
- **MaxDepth**: 必须为正数
- **Cache Size**: 必须为正数
- **Workers**: 必须为正数
- **MaxConcurrent**: 必须为正数
- **LogLevel**: 必须是 `debug`、`info`、`warn` 或 `error`

## 默认值

| 配置项 | 默认值 |
|--------|--------|
| Transport | auto |
| Mode | recursive |
| TimeoutMs | 5000 |
| Cache Size | 10000 |
| MaxDepth | 30 |
| Workers | 4 |
| MaxConcurrent | 100 |
| LogLevel | info |
| UnixSocket Path | /tmp/smartdns-resolver.sock |
| TCP ListenAddr | 127.0.0.1 |
| TCP ListenPort | 5353 |

## 工作模式

### Recursive（递归模式）

对所有查询使用递归解析，从根服务器开始迭代查询。

### Forwarding（转发模式）

对所有查询使用转发模式，将查询转发到上游DNS服务器。

### Hybrid（混合模式）

根据域名规则选择递归或转发模式：
- 匹配 `recursive_domains` 的域名使用递归解析
- 匹配 `forward_domains` 的域名使用转发模式
- 不匹配任何规则的域名使用默认行为

## 测试

运行单元测试：

```bash
go test -v ./resolver
```

## 相关文件

- `config.go`: 配置加载、保存和验证
- `config_test.go`: 配置系统的单元测试
- `README.md`: 本文件
