# SmartDNSSort 测试指南

## 🧪 单元测试

### 运行所有测试
```powershell
go test ./...
```

### 运行特定模块测试
```powershell
# 缓存模块测试
go test -v ./cache

# Ping 模块测试
go test -v ./ping

# 详细输出和竞态条件检测
go test -v -race ./...
```

### 测试覆盖率
```powershell
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## 🔍 手动功能测试

### 前置准备
1. 编辑 `config.yaml`，确保配置正确
2. 编译项目：`go build -o smartdnssort.exe ./cmd`
3. 启动服务：`.\smartdnssort.exe`

### 测试 1: DNS A 记录查询

**使用 nslookup（Windows）：**
```powershell
nslookup example.com 127.0.0.1:53
nslookup google.com 127.0.0.1:53
nslookup cloudflare.com 127.0.0.1:53
```

**使用 dig（Linux/macOS）：**
```bash
dig @127.0.0.1 example.com
dig @127.0.0.1 google.com +short
```

**预期结果：**
- 首次查询时间较长（包含 ping 测试），约 500ms+
- 后续相同域名查询应该很快（< 5ms）
- 返回的 IP 应该按延迟排序

### 测试 2: 缓存生效验证

**步骤：**
```powershell
# 第一次查询（缓存未命中）
nslookup example.com 127.0.0.1:53
# 观察响应时间 - 应该较慢

# 第二次查询（缓存命中）
nslookup example.com 127.0.0.1:53
# 观察响应时间 - 应该很快

# 等待 TTL 过期（config.yaml 中配置，默认 300 秒）
# 再查询 - 应该回到较慢的响应时间
```

### 测试 3: 多个上游 DNS 的可用性

**配置多个上游 DNS：**
```yaml
upstream:
  servers:
    - "8.8.8.8"           # Google
    - "1.1.1.1"           # Cloudflare
    - "208.67.222.222"    # OpenDNS
    - "9.9.9.9"           # Quad9
```

**测试：**
```powershell
# 关闭一个上游 DNS，观察是否能从其他 DNS 获取结果
nslookup google.com 127.0.0.1:53
```

### 测试 4: 并发查询压力测试

**创建简单的 PowerShell 脚本 `stress_test.ps1`：**
```powershell
# 并发发送 100 个 DNS 查询
$domains = @("google.com", "github.com", "cloudflare.com", "example.com")

$tasks = @()
for ($i = 0; $i -lt 100; $i++) {
    $domain = $domains[$i % $domains.Count]
    $tasks += Start-Job -ScriptBlock {
        nslookup $args[0] 127.0.0.1:53
    } -ArgumentList $domain
}

Wait-Job $tasks
Get-Job | Remove-Job
Write-Host "Stress test completed"
```

运行：
```powershell
.\stress_test.ps1
```

### 测试 5: IPv6 支持（如果配置启用）

```powershell
# 查询 IPv6 地址
nslookup -type=AAAA google.com 127.0.0.1:53
```

---

## 📊 日志和诊断

### 查看运行时输出

启动时会看到日志输出：
```
SmartDNSSort DNS Server started on port 53
Upstream servers: [8.8.8.8 1.1.1.1 208.67.222.222]
Ping concurrency: 16, timeout: 500ms
```

查询时的日志示例：
```
Query: google.com (type=A)
Upstream query: google.com -> [142.251.48.14 142.251.48.46]
Sorted IPs: google.com -> [142.251.48.14 142.251.48.46]
Cache hit: github.com -> [140.82.114.3 140.82.114.4]
```

### 监控缓存效率

通过日志观察：
- `Cache hit` - 缓存命中（好）
- `Upstream query` - 缓存未命中，进行查询（可以优化缓存 TTL）
- `Ping` 失败次数 - 网络问题指示器

---

## 🐛 故障排查

### 问题 1: "address already in use" 错误

**原因**：53 端口被占用

**解决**：
```powershell
# 检查占用 53 端口的进程
netstat -ano | findstr :53

# 或修改 config.yaml 中的 listen_port 为其他端口（如 8053）
```

### 问题 2: DNS 查询返回空结果

**原因**：上游 DNS 无法访问或配置错误

**解决**：
```powershell
# 测试上游 DNS 是否可访问
nslookup google.com 8.8.8.8

# 更新 config.yaml 中的 upstream servers
```

### 问题 3: Ping 超时过多

**原因**：网络延迟大或防火墙阻止

**解决**：
```yaml
# 增加超时时间
ping:
  timeout_ms: 1000  # 从 500 改为 1000

# 或减少 ping 次数
ping:
  count: 1          # 从 3 改为 1
```

### 问题 4: 内存占用过高

**原因**：缓存项过多导致内存使用

**解决**：
```yaml
# 缩短 TTL 时间，更频繁清理过期项
cache:
  ttl_seconds: 60   # 从 300 改为 60

# 或减少 ping 并发数
ping:
  concurrency: 8    # 从 16 改为 8
```

---

## 📈 性能基准测试

### 准备脚本 `benchmark.ps1`：
```powershell
# 性能基准测试

$results = @()

# 测试 100 个不同域名的查询性能
$domains = @(
    "google.com", "github.com", "cloudflare.com", "example.com", "stackoverflow.com"
)

foreach ($domain in $domains) {
    $stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
    nslookup $domain 127.0.0.1:53 | Out-Null
    $stopwatch.Stop()
    
    $results += [PSCustomObject]@{
        Domain = $domain
        TimeMsFirst = $stopwatch.ElapsedMilliseconds
    }
    
    Start-Sleep -Milliseconds 100
}

# 再查询一次（测试缓存）
foreach ($domain in $domains) {
    $stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
    nslookup $domain 127.0.0.1:53 | Out-Null
    $stopwatch.Stop()
    
    $results | Where-Object Domain -eq $domain | Add-Member -Name "TimeMs2nd" -Value $stopwatch.ElapsedMilliseconds -MemberType NoteProperty
}

# 显示结果
$results | Format-Table -AutoSize
$results | Measure-Object TimeMsFirst -Average -Minimum -Maximum | 
    Format-Table @{N="Metric";E={$_.Property}}, @{N="First Query (ms)";E={$_.Average}},
                 @{N="Min";E={$_.Minimum}}, @{N="Max";E={$_.Maximum}} -AutoSize
```

运行：
```powershell
.\benchmark.ps1
```

---

## ✅ 完整测试检查清单

- [ ] 单元测试全部通过 (`go test ./...`)
- [ ] DNS A 记录查询正常
- [ ] DNS AAAA（IPv6）查询正常（如启用）
- [ ] 缓存机制生效
- [ ] 缓存过期清理正常
- [ ] 多上游 DNS 故障转移正常
- [ ] Ping 测试和 IP 排序正常
- [ ] 并发查询无崩溃或错误
- [ ] 内存占用稳定
- [ ] 响应时间符合预期
- [ ] 日志输出清晰正确

---

## 🎯 测试用例示例

### UC1: 域名首次查询
```
输入：google.com
预期：
1. 查询上游 DNS 获取 IP
2. 对 IP 进行 ping 测试
3. 按 RTT 排序 IP
4. 缓存结果
5. 返回排序后的 IP
```

### UC2: 域名缓存命中
```
输入：google.com（第 2 次查询）
预期：
1. 直接返回缓存结果
2. 响应时间 < 5ms
```

### UC3: 缓存过期重新查询
```
输入：google.com（TTL 秒后）
预期：
1. 缓存过期，进行新查询
2. 重复 UC1 流程
```

### UC4: 部分上游 DNS 故障
```
输入：域名查询，某个上游 DNS 不可用
预期：
1. 并发查询多个上游 DNS
2. 使用第一个成功的响应
3. 查询继续成功
```

---

## 📞 调试技巧

### 启用详细日志（开发时）
修改 `dnsserver/server.go` 中的 log 输出，或在 `main.go` 中设置：
```go
log.SetFlags(log.LstdFlags | log.Lshortfile)
```

### 监控 goroutine 数量
```go
// 在 dnsserver/server.go 中添加
import "runtime"

func (s *Server) PrintStats() {
    fmt.Printf("Goroutines: %d\n", runtime.NumGoroutine())
}
```

### 使用 pprof 分析性能
```go
import "net/http/pprof"

// 在 main.go 中添加
go http.ListenAndServe(":6060", nil)

// 访问 http://localhost:6060/debug/pprof
```

---

**最后更新**：2025 年 11 月 14 日
