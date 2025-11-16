# SmartDNSSort 项目概览

## 📋 项目简介

**SmartDNSSort** 是一个用 Go 实现的智能 DNS 服务器，具有以下核心功能：

1. **获取 IP** - 并发查询多个上游 DNS 服务器获取域名对应的 IP 列表
2. **测试 IP** - 使用 TCP Ping 对返回的每个 IP 进行延迟和可用性测试
3. **排序 IP** - 根据测试结果（RTT、可用性）智能排序 IP 列表
4. **缓存优化** - 将排序结果缓存，避免重复查询和测试
5. **返回结果** - 返回排序后的 IP 列表给 DNS 客户端

## 🎯 核心特性

✅ **高性能** - 使用 goroutine 并发处理，充分利用多核 CPU
✅ **智能排序** - TCP Ping 测试，按实际可用性和响应速度排序
✅ **内存缓存** - 避免重复查询，提高响应速度
✅ **灵活配置** - YAML 配置文件，所有参数可调
✅ **IPv4/IPv6** - 同时支持 IPv4 和 IPv6 查询
✅ **详细统计** - 提供查询计数、缓存命中率、失败追踪等信息

## 📁 项目结构

```
SmartDNSSort/
├── cmd/
│   └── main.go                 # 程序入口
├── dnsserver/
│   └── server.go              # DNS 服务器核心实现
├── upstream/
│   └── upstream.go            # 上游 DNS 查询模块
├── ping/
│   ├── ping.go                # IP 测试和排序模块
│   └── ping_test.go           # 单元测试
├── cache/
│   ├── cache.go               # 缓存管理模块
│   └── cache_test.go          # 单元测试
├── config/
│   └── config.go              # 配置解析模块
├── stats/
│   └── stats.go               # 运行统计模块
├── internal/
│   └── util.go                # 工具函数
├── config.yaml                # 配置文件（编辑此文件调整参数）
├── go.mod                      # Go 模块依赖
├── go.sum                      # 依赖锁定文件
├── README.md                   # 快速开始指南
├── DEVELOP.md                  # 开发文档
├── Makefile                    # 构建脚本
├── run.bat                     # Windows 启动脚本
├── run.sh                      # Linux/macOS 启动脚本
└── .gitignore                  # Git 忽略文件
```

## 🚀 快速开始

### 环境要求
- Go 1.21 或更新版本
- Windows / Linux / macOS

### 安装步骤

1. **克隆或下载项目**
   ```powershell
   # 如果使用 Git
   git clone <项目地址>
   cd SmartDNSSort
   ```

2. **运行启动脚本**
   
   **Windows:**
   ```powershell
   .\run.bat
   ```
   
   **Linux/macOS:**
   ```bash
   chmod +x run.sh
   ./run.sh
   ```

3. **或者手动编译运行**
   ```powershell
   go mod tidy
   go run ./cmd/main.go
   ```

## ⚙️ 配置文件说明

编辑 `config.yaml` 来调整各项参数：

```yaml
dns:
  listen_port: 53              # DNS 监听端口（默认 53）
  enable_tcp: true             # 启用 TCP DNS（DNS over TCP）
  enable_ipv6: true            # 启用 IPv6 支持

upstream:
  servers:                     # 上游 DNS 服务器列表
    - "8.8.8.8"               # Google DNS
    - "1.1.1.1"               # Cloudflare DNS
    - "208.67.222.222"        # OpenDNS
  strategy: "random"           # 查询策略：random（随机）或 parallel（并行）
  timeout_ms: 3000            # 查询超时（毫秒）
  concurrency: 4              # 最大并发查询数

ping:
  count: 3                    # 每个 IP 的 ping 次数
  timeout_ms: 500             # ping 超时（毫秒）
  concurrency: 16             # 最大并发 ping 数
  strategy: "min"             # 排序策略：min（最小 RTT）或 avg（平均 RTT）

cache:
  fast_response_ttl: 60       # 快速响应 TTL（秒）
  min_ttl_seconds: 3600       # 最小缓存 TTL（秒）
  max_ttl_seconds: 84600      # 最大缓存 TTL（秒）

webui:
  enabled: true               # WebUI 是否启用
  listen_port: 8080           # WebUI 监听端口

adblock:
  enabled: false              # 广告拦截是否启用
  rule_file: "rules.txt"
```

## 💡 工作原理

### 查询流程

```
1. DNS 客户端发送查询请求
   ↓
2. SmartDNSSort 接收查询，记录统计数据
   ↓
3. 检查内存缓存
   ├→ 缓存命中 → 返回排序后的 IP 列表 → 完成
   └→ 缓存未命中 → 继续步骤 4
   ↓
4. 并发查询上游 DNS 服务器获取 IP
   ↓
5. 对返回的 IP 进行 TCP Ping 测试
   ├→ TCP Ping 连接 80/443 端口
   ├→ 测试每个 IP 的响应时间（RTT）
   └→ 记录可用性
   ↓
6. 按 RTT 和可用性排序 IP 列表
   ↓
7. 将结果缓存到内存
   ↓
8. 构造 DNS 响应，返回排序后的 IP
   ↓
9. 完成
```

### TCP Ping 特点

- **比 ICMP Ping 更真实** - 模拟实际应用连接
- **防火墙友好** - 不依赖 ICMP 协议
- **应用相关** - 测试 HTTP/HTTPS 可用性
- **可靠性高** - 更能反映实际用户体验

## 📊 示例使用场景

### 场景 1：CDN 加速
在 CDN 环境中使用 SmartDNSSort，自动返回最快的 CDN 节点 IP。

### 场景 2：多运营商线路选择
配置多个运营商的 DNS，SmartDNSSort 自动测试并返回最快的线路。

### 场景 3：全球负载均衡
多个地区服务器，SmartDNSSort 根据延迟自动返回最优线路。

## 🔧 开发和构建

### 运行测试
```powershell
go test ./...
go test -v ./cache
go test -v ./ping
```

### 代码质量检查
```powershell
go fmt ./...
go vet ./...
```

### 编译二进制
```powershell
# Windows
go build -o smartdnssort.exe ./cmd

# Linux
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o smartdnssort ./cmd

# macOS
$env:GOOS="darwin"; $env:GOARCH="amd64"; go build -o smartdnssort ./cmd
```

## 📈 性能指标

- **DNS 响应时间**
  - 缓存命中：< 5ms
  - 缓存未命中：~ 500ms（取决于网络和上游 DNS 速度）

- **Ping 能力**
  - 可同时 ping 数百个 IP
  - 可配置并发数量

- **内存占用**
  - 轻量级，取决于缓存大小
  - 100 个缓存项约占用 < 1MB 内存

- **CPU 利用率**
  - 充分利用多核 CPU
  - 并发数可配置

## 🛠️ 故障排查

| 问题 | 原因 | 解决方案 |
|------|------|--------|
| 启动时 "端口被占用" | 53 端口已被其他应用使用 | 修改 config.yaml 中的 listen_port，或关闭占用端口的应用 |
| DNS 查询返回失败 | 上游 DNS 服务器无法连接 | 检查网络连接，或更换上游 DNS 地址 |
| Ping 超时较多 | 网络延迟大或目标 IP 无法连接 | 增加 ping.timeout_ms，或检查网络 |
| 缓存未生效 | TTL 时间太短 | 增加 cache.ttl_seconds 参数 |
| 并发 Ping 过多导致卡顿 | 并发数设置过大 | 降低 ping.concurrency 参数 |

## 📚 模块详解

详见 [DEVELOP.md](./DEVELOP.md) 文件，包含：
- 各模块详细说明
- 核心概念解释
- 开发指南
- 代码示例

## 🔄 开发进度

| 阶段 | 状态 | 功能 |
|------|------|------|
| 1 | ✅ 完成 | DNS Server + 上游查询 |
| 2 | ✅ 完成 | Ping 排序 + 缓存机制 |
| 3 | 🔄 进行中 | 性能优化 + 配置增强 |
| 4 | 📅 计划中 | WebUI + 统计信息展示 |
| 5 | 📅 计划中 | 广告拦截 + 高级功能 |

## 📄 许可证

MIT License

## 💬 联系和反馈

欢迎提交 Issue 和 Pull Request！

---

**项目更新时间**：2025 年 11 月 14 日

**下一步**：
1. 编辑 `config.yaml` 配置您的上游 DNS
2. 运行 `run.bat`（Windows）或 `run.sh`（Linux/macOS）
3. 使用 `nslookup` 或 `dig` 测试 DNS
