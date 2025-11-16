# SmartDNSSort 快速参考

## 🚀 快速启动

### Windows
```powershell
.\run.bat
```

### Linux/macOS
```bash
./run.sh
```

### 手动启动
```bash
go mod tidy && go run ./cmd/main.go
```

---

## ⚙️ 配置快速调整

### 编辑 `config.yaml`

#### 更改 DNS 端口
```yaml
dns:
  listen_port: 8053  # 改为任意可用端口
```

#### 更换上游 DNS
```yaml
upstream:
  servers:
    - "1.1.1.1"        # Cloudflare
    - "8.8.8.8"        # Google
    - "9.9.9.9"        # Quad9
```

#### 调整 Ping 测试参数
```yaml
ping:
  count: 1             # 更快的响应（更少测试）
  timeout_ms: 200      # 更短的超时
  concurrency: 32      # 更高的并发
```

#### 调整缓存时间
```yaml
cache:
  ttl_seconds: 60      # 1 分钟
```

---

## 🧪 测试命令

```powershell
# 运行所有测试
go test ./...

# 详细输出
go test -v ./...

# 测试特定模块
go test -v ./cache
go test -v ./ping

# 竞态检测
go test -race ./...
```

---

## 🔍 DNS 查询测试

### Windows (nslookup)
```powershell
# 查询 A 记录
nslookup example.com 127.0.0.1:53

# 查询 IPv6 记录
nslookup -type=AAAA example.com 127.0.0.1:53
```

### Linux/macOS (dig)
```bash
# 查询 A 记录
dig @127.0.0.1 example.com

# 简短输出
dig @127.0.0.1 example.com +short

# 查询 IPv6
dig @127.0.0.1 example.com AAAA
```

---

## 📊 关键参数说明

| 参数 | 含义 | 推荐值 | 范围 |
|------|------|-------|------|
| `dns.listen_port` | DNS 监听端口 | 53 | 1-65535 |
| `upstream.timeout_ms` | 上游查询超时 | 300 | 100-5000 |
| `upstream.concurrency` | 上游并发数 | 4 | 1-16 |
| `ping.count` | 每个 IP ping 次数 | 3 | 1-10 |
| `ping.timeout_ms` | 单次 ping 超时 | 500 | 100-2000 |
| `ping.concurrency` | 并发 ping 数 | 16 | 4-64 |
| `cache.ttl_seconds` | 缓存过期时间 | 300 | 10-3600 |

---

## 🎯 常见场景配置

### ⚡ 低延迟优先
```yaml
ping:
  count: 1
  timeout_ms: 200
  concurrency: 32
cache:
  ttl_seconds: 600
```

### 🛡️ 稳定性优先
```yaml
ping:
  count: 5
  timeout_ms: 1000
  concurrency: 8
cache:
  ttl_seconds: 600
```

### 🌐 全局负载均衡
```yaml
upstream:
  concurrency: 8
ping:
  concurrency: 32
cache:
  ttl_seconds: 300
```

---

## 📁 项目结构速览

```
SmartDNSSort/
├── cmd/main.go              ← 程序入口
├── config/config.go         ← 配置解析
├── upstream/upstream.go     ← 上游 DNS 查询
├── ping/ping.go             ← IP 测试排序
├── cache/cache.go           ← 缓存管理
├── dnsserver/server.go      ← DNS 服务器
├── stats/stats.go           ← 统计模块
├── internal/util.go         ← 工具函数
├── config.yaml              ← 配置文件
└── README.md               ← 使用指南
```

---

## 🔧 编译命令

```powershell
# 仅编译（不运行）
go build -o smartdnssort.exe ./cmd

# Linux/macOS
go build -o smartdnssort ./cmd

# 交叉编译为 Linux
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o smartdnssort ./cmd

# 交叉编译为 macOS
$env:GOOS="darwin"; $env:GOARCH="amd64"; go build -o smartdnssort ./cmd
```

---

## 📈 性能指标参考

| 指标 | 值 |
|------|-----|
| 缓存命中响应时间 | < 5ms |
| 首次查询响应时间 | ~ 500ms |
| 最大并发 Ping 数 | 数百个 |
| 缓存 100 项内存占用 | ~ 1MB |
| 启动内存占用 | ~ 5MB |

---

## 🐛 常见问题速解

| 问题 | 快速解决 |
|------|--------|
| 53 端口被占用 | 改 `config.yaml` 的 `listen_port` |
| DNS 查询失败 | 检查 `upstream.servers` 配置 |
| 响应很慢 | 增加 `ping.timeout_ms` |
| 缓存无效 | 检查 `cache.ttl_seconds` 是否过短 |

---

## 📚 文档导航

- **README.md** - 快速开始（新手必读）
- **OVERVIEW.md** - 项目全面概览
- **DEVELOP.md** - 开发文档（开发者必读）
- **TESTING.md** - 测试指南
- **COMPLETION_REPORT.md** - 完成报告

---

## 🔗 常用命令速查

```powershell
# 启动
.\run.bat

# 编译
go build -o smartdnssort.exe ./cmd

# 测试
go test ./...

# 查看配置
cat config.yaml

# 修改配置
notepad config.yaml

# 清理
go clean
```

---

## ✨ 关键特性

✅ DNS 服务器 - 监听 53 端口，处理 A/AAAA 查询
✅ 上游查询 - 并发查询多个 DNS
✅ IP 排序 - TCP Ping 测试，按延迟排序
✅ 智能缓存 - TTL 自动过期，缓存命中率统计
✅ 监控统计 - 查询计数、失败追踪、性能指标

---

**版本**: 1.0.0
**Go 版本**: 1.21+
**更新日期**: 2025-11-14
