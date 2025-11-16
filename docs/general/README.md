# SmartDNSSort - Go 版本

一个轻量级、高性能的 DNS 服务器，专注于 A/AAAA 查询的 IP 排序优化。通过并发 ping 测试动态调整返回顺序，提升网页打开速度。

## 快速开始

### 前置要求
- Go 1.21+
- Windows/Linux/macOS

### 安装依赖
```powershell
go mod tidy
```

### 运行服务器
```powershell
go run .\cmd\main.go
```

## 项目结构

```
SmartDNSSort/
├── cmd/              # 主程序入口
├── dnsserver/        # DNS 监听与响应构造
├── upstream/         # 上游 DNS 查询逻辑
├── ping/             # 并发 ping 测试模块
├── cache/            # 内存缓存管理
├── config/           # 配置解析与验证
├── stats/            # 运行统计
├── internal/         # 公共工具函数
├── config.yaml       # 配置文件
└── main.go           # 启动入口
```

## 模块说明

### dnsserver - DNS 服务器
- 监听 UDP/TCP 53 端口
- 处理 A/AAAA 查询请求
- 调用上游 DNS 和 ping 模块进行排序

### upstream - 上游 DNS 查询
- 并发查询多个上游 DNS 服务器
- 支持 parallel（并行）和 random（随机）策略
- 返回最快响应的 IP 列表

### ping - IP 测试排序
- TCP Ping 测试（连接 80/443 端口）
- 并发测试多个 IP，按 RTT 排序
- 支持并发数量控制，防止瞬间连接数过多

### cache - 缓存管理
- 内存缓存 DNS 解析结果
- 支持 TTL 自动过期
- 提供缓存命中率统计

### config - 配置管理
- 加载 YAML 配置文件
- 提供默认值设置
- 支持各模块参数配置

### stats - 运行统计
- 统计查询总数、缓存命中率
- 记录 ping 成功率和平均 RTT
- 追踪失败节点

## 配置文件说明

编辑 `config.yaml`：

```yaml
dns:
  listen_port: 53           # DNS 监听端口
  enable_tcp: true          # 启用 TCP
  enable_ipv6: true         # 启用 IPv6

upstream:
  servers:                  # 上游 DNS 服务器列表
    - "8.8.8.8"
    - "1.1.1.1"
  strategy: "parallel"      # parallel 或 random
  timeout_ms: 300           # 查询超时（毫秒）
  concurrency: 4            # 并发数量

ping:
  count: 3                  # 每个 IP ping 次数
  timeout_ms: 500           # ping 超时（毫秒）
  concurrency: 16           # 并发 ping 数量
  strategy: "min"           # min 或 avg

cache:
  ttl_seconds: 300          # 缓存过期时间（秒）

webui:
  enabled: false            # Web UI 是否启用
  listen_port: 8080         # Web UI 端口

adblock:
  enabled: false            # 广告拦截是否启用
  rule_file: "rules.txt"    # 广告拦截规则文件
```

## 工作流程

1. **接收查询**：DNS 客户端查询域名
2. **检查缓存**：查找缓存中是否有排序好的 IP
3. **上游查询**：如果缓存未命中，并发查询多个上游 DNS
4. **Ping 排序**：对返回的 IP 进行 TCP Ping 测试，按 RTT 排序
5. **缓存保存**：将排序结果缓存，返回给客户端

## 特性

✅ **高性能**：使用 goroutine 并发处理，充分利用多核 CPU
✅ **智能排序**：TCP Ping 测试，按实际可用性和响应速度排序
✅ **内存缓存**：避免重复查询和 ping 测试
✅ **灵活配置**：YAML 配置文件，参数可调
✅ **IPv4/IPv6**：同时支持 IPv4 和 IPv6
✅ **统计分析**：提供详细的运行统计信息

## 开发阶段

| 阶段 | 目标 |
|------|------|
| 1 | ✅ DNS Server + 上游查询模块 |
| 2 | ✅ ping 排序模块 + 缓存机制 |
| 3 | 并发调度优化 + 配置系统 |
| 4 | WebUI 初版 + 统计模块 + 测试与部署 |
| 5 | AdBlock 模块（可选） + 性能调优 |

## 许可证

MIT
