# SmartDNSSort 开发指南

## 项目概述

SmartDNSSort 是一个 Go 实现的智能 DNS 服务器，核心特性：

1. **获取 IP**：从上游 DNS 服务器并发查询域名 IP
2. **IP 测试**：使用 TCP Ping 对返回的 IP 进行延迟测试
3. **IP 排序**：按延迟（RTT）和可用性排序 IP
4. **缓存优化**：缓存排序结果，减少重复查询和测试
5. **后端查询**：返回排序后的 IP 给客户端

## 核心流程

```
DNS 查询 → 检查缓存 → 命中返回 
         ↓ 未命中
      上游DNS查询 → Ping 测试排序 → 缓存结果 → 返回客户端
```

## 模块详解

### 1. config（配置模块）
**文件**：`config/config.go`

功能：
- 解析 YAML 配置文件
- 提供配置结构体
- 设置默认值

关键结构：
- `Config`：主配置对象
- `DNSConfig`：DNS 服务配置
- `UpstreamConfig`：上游 DNS 配置
- `PingConfig`：Ping 测试配置
- `CacheConfig`：缓存配置

### 2. upstream（上游查询模块）
**文件**：`upstream/upstream.go`

功能：
- 向多个上游 DNS 服务器查询
- 支持并行和随机查询策略
- 并发控制与超时管理

核心方法：
- `Query()`：查询 A 记录
- `QueryIPv4()`：查询 IPv4
- `QueryIPv6()`：查询 IPv6
- `queryParallel()`：并行查询
- `queryRandom()`：随机查询

### 3. ping（IP 测试模块）
**文件**：`ping/ping.go`

功能：
- 使用 TCP Ping 测试 IP 响应时间
- 并发控制，防止连接数过多
- 按 RTT 和成功率排序

核心方法：
- `PingIPs()`：并发 ping 多个 IP
- `SortIPs()`：排序 IP 列表
- `tcpPing()`：单个 IP 的 TCP Ping

TCP Ping 原理：
- 尝试连接目标 IP 的 80 端口（HTTP）
- 如果失败，尝试 443 端口（HTTPS）
- 记录连接耗时作为 RTT
- 比 ICMP Ping 更贴近真实应用场景

### 4. cache（缓存模块）
**文件**：`cache/cache.go`

功能：
- 缓存已排序的 IP 列表
- 自动过期清理
- 命中率统计

数据结构：
- `CacheEntry`：缓存项（IP 列表 + RTT + 时间戳 + TTL）
- `Cache`：缓存管理器

核心方法：
- `Get()`：获取缓存
- `Set()`：设置缓存
- `CleanExpired()`：清理过期项
- `GetStats()`：获取统计信息

### 5. stats（统计模块）
**文件**：`stats/stats.go`

功能：
- 记录查询总数
- 统计缓存命中率
- 追踪失败节点
- 统计 Ping 成功率和平均 RTT

核心指标：
- `queries`：总查询数
- `cache_hits`：缓存命中数
- `cache_hit_rate`：缓存命中率
- `ping_successes`：Ping 成功数
- `average_rtt_ms`：平均 RTT
- `failed_nodes`：失败节点计数

### 6. dnsserver（DNS 服务模块）
**文件**：`dnsserver/server.go`

功能：
- 监听 UDP 和 TCP 53 端口
- 解析 DNS 请求
- 调用各个模块处理查询
- 构造 DNS 响应

核心方法：
- `Start()`：启动服务器
- `handleQuery()`：处理 DNS 查询
- `buildDNSResponse()`：构造响应
- `cleanCacheRoutine()`：定期清理缓存

### 7. internal（工具模块）
**文件**：`internal/util.go`

提供工具函数：
- `IsIPv4()`、`IsIPv6()`：IP 类型检查
- `FilterIPv4()`、`FilterIPv6()`：IP 筛选
- `NormalizeDomain()`：域名规范化
- `IsValidDomain()`：域名验证

## 配置说明

**config.yaml** 主要参数：

```yaml
# DNS 服务监听配置
dns:
  listen_port: 53        # 监听端口
  enable_tcp: true       # 启用 TCP（DNS over TCP）
  enable_ipv6: true      # 启用 IPv6 支持

# 上游 DNS 服务器配置
upstream:
  servers:               # 上游 DNS 列表
    - "8.8.8.8"          # Google DNS
    - "1.1.1.1"          # Cloudflare DNS
  strategy: "parallel"   # parallel（并行）或 random（随机）
  timeout_ms: 300        # 查询超时（毫秒）
  concurrency: 4         # 最大并发查询数

# IP Ping 测试配置
ping:
  count: 3               # 每个 IP Ping 次数
  timeout_ms: 500        # 单次 Ping 超时
  concurrency: 16        # 最大并发 Ping 数
  strategy: "min"        # min（最小 RTT）或 avg（平均 RTT）

# 缓存配置
cache:
  ttl_seconds: 300       # 缓存过期时间（秒）

# 其他模块配置...
```

## 开发建议

### 阶段 1：DNS + 上游查询（已完成）
- ✅ DNS 服务器基础框架
- ✅ 上游 DNS 并发查询
- ✅ 请求响应处理

### 阶段 2：Ping 排序 + 缓存（已完成）
- ✅ TCP Ping 测试实现
- ✅ IP 排序算法
- ✅ 内存缓存管理
- ✅ TTL 自动过期

### 阶段 3：性能优化（开发中）
- [ ] 并发度动态调整
- [ ] 缓存预热机制
- [ ] 性能监控和日志优化

### 阶段 4：WebUI（计划中）
- [ ] REST API 接口
- [ ] 统计信息展示
- [ ] 缓存管理功能

### 阶段 5：高级功能（计划中）
- [ ] 广告拦截（AdBlock）
- [ ] 自定义规则
- [ ] DNS 安全增强

## 并发模型

本项目使用 Go 的 goroutine 和 channel 实现高效并发：

1. **上游查询并发**：
   - 为每个上游服务器创建 goroutine
   - 使用 semaphore 限制并发数（`Concurrency` 参数）

2. **Ping 并发**：
   - 为每个 IP 创建 goroutine
   - 使用带缓冲 channel 作为 semaphore
   - 严格控制并发数防止资源耗尽

3. **缓存清理**：
   - 使用独立 goroutine + ticker 定期清理过期项
   - 使用 RWMutex 保护并发读写

## 性能指标

- **DNS 响应时间**：缓存命中 <5ms，未命中 ~500ms（取决于网络）
- **Ping 并发能力**：支持同时 ping 数百个 IP
- **内存占用**：轻量级，缓存大小可配置
- **CPU 利用率**：充分利用多核，自适应负载

## 测试

运行单元测试：
```bash
go test ./...
go test -v ./cache
go test -v ./ping
```

## 构建和发布

```bash
# 编译
go build -o smartdnssort.exe ./cmd

# 跨平台编译
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o smartdnssort ./cmd
```

## 故障排查

| 问题 | 原因 | 解决方案 |
|------|------|--------|
| 端口被占用 | 53 端口已被占用 | 修改 config.yaml 的 listen_port |
| DNS 查询失败 | 上游服务器无法连接 | 检查网络，更换上游 DNS |
| Ping 超时多 | 网络延迟大 | 增加 ping.timeout_ms 参数 |
| 缓存未生效 | TTL 过短 | 增加 cache.ttl_seconds 参数 |

## 贡献指南

欢迎提交 Issue 和 Pull Request！

---

**最后更新**：2025 年 11 月 14 日
