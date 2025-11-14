# SmartDNSSort - 所有 IP 返回测试指南

## 核心特性

✅ **返回所有 IP** - 包括 IPv4 和 IPv6
✅ **Ping 通的 IP 排在前** - 按 RTT 从低到高排序  
✅ **Ping 不通的 IP 排在后** - RTT 设为 999999，排在最后

## 工作流程

```
查询域名
    ↓
获取所有 A + AAAA 记录
    ↓
对所有 IP 进行 Ping 测试
    ├→ Ping 通: 记录 RTT
    └→ Ping 失败: RTT = 999999
    ↓
按规则排序
    ├→ 按成功率排序（成功率高优先）
    └→ 按 RTT 排序（RTT 低优先）
    ↓
返回所有 IP
    ├→ DNS: 按类型过滤返回
    └→ API: 返回完整列表 + RTT
```

## 测试方法

### 环境准备

1. 编辑 `config.yaml`，设置上游 DNS：
```yaml
upstream:
  servers:
    - "8.8.8.8"      # Google DNS
    - "1.1.1.1"      # Cloudflare DNS
    - "208.67.222.222" # OpenDNS
  strategy: "parallel"
```

2. 启动服务：
```bash
go run ./cmd/main.go
# 或
.\run.bat
```

### 测试 1: IPv4 查询（返回所有 A 记录）

```powershell
nslookup google.com 127.0.0.1
```

**预期结果：**
```
Server:  localhost
Address: 127.0.0.1

Name:    google.com
Addresses: 142.251.48.14      (Ping 通，RTT 最短)
          142.251.48.46      (Ping 通，RTT 较短)
          142.251.48.78      (Ping 通，RTT 较长)
          192.0.2.1          (Ping 不通，排最后)
```

### 测试 2: IPv6 查询（返回所有 AAAA 记录）

```bash
dig @127.0.0.1 google.com AAAA
```

**预期结果：**
```
google.com.     300 IN  AAAA    2607:f8b0:4004:80b::200e  (Ping 通)
google.com.     300 IN  AAAA    2607:f8b0:4004:809::200e  (Ping 不通，排后)
```

### 测试 3: Web API 查询（获取完整信息含 RTT）

```bash
# IPv4
curl "http://localhost:8080/api/query?domain=google.com&type=A"

# IPv6
curl "http://localhost:8080/api/query?domain=google.com&type=AAAA"
```

**预期返回 (JSON)：**
```json
{
  "domain": "google.com",
  "type": "A",
  "ips": [
    {
      "ip": "142.251.48.14",
      "rtt": 45
    },
    {
      "ip": "142.251.48.46",
      "rtt": 52
    },
    {
      "ip": "142.251.48.78",
      "rtt": 68
    },
    {
      "ip": "192.0.2.1",
      "rtt": 999999
    }
  ],
  "status": "success"
}
```

### 测试 4: 验证 Ping 失败的 IP 排在最后

1. 运行查询并记录日志：
```
Query: google.com (type=A)
Upstream query: google.com -> [142.251.48.14 142.251.48.46 192.0.2.1]
Ping results for google.com: 
  - 142.251.48.14: RTT=45ms, Loss=0%
  - 142.251.48.46: RTT=52ms, Loss=0%
  - 192.0.2.1: RTT=999999ms, Loss=100%  ← Ping 失败排最后
Building DNS response with IPs in order: [142.251.48.14 142.251.48.46 192.0.2.1]
```

2. 查看返回顺序：
```bash
nslookup google.com 127.0.0.1
# 结果中最快的 IP 第一个，最慢/Ping 不通的排最后
```

## 日志分析

### 正常情况
```
Query: example.com (type=A)
Upstream query: example.com -> [1.2.3.4 1.2.3.5 1.2.3.6]
Ping results for example.com: [1.2.3.4 1.2.3.5 1.2.3.6] with RTTs: [45 52 68]
Building DNS response for example.com (type=A) with IPs: [1.2.3.4 1.2.3.5 1.2.3.6]
```

### 包含失败 IP
```
Query: example.com (type=A)
Upstream query: example.com -> [1.2.3.4 1.2.3.5 192.0.2.100]
Ping results for example.com: [1.2.3.4 1.2.3.5 192.0.2.100] with RTTs: [45 52 999999]
Building DNS response for example.com (type=A) with IPs: [1.2.3.4 1.2.3.5 192.0.2.100]
                                                          ↑ Ping 不通的排最后
```

## 关键代码改动

### 1. ping 模块 - 返回所有 IP

**改动：**
- `pingIP()` 不再返回 `nil`
- Ping 失败的 IP 设置 RTT = 999999
- 排序时失败的 IP 自动排到最后

```go
if successCount == 0 {
    avgRTT = 999999  // 失败的 IP 设置最大值
}
// 总是返回结果
return &Result{IP: ip, RTT: avgRTT, Loss: lossRate}
```

### 2. upstream 模块 - 获取所有 IP

**新增方法：**
- `QueryAll()` 并发查询 A 和 AAAA 记录
- 返回混合的 IP 列表（可能包含 IPv4 和 IPv6）

```go
func (u *Upstream) QueryAll(ctx context.Context, domain string) ([]string, error)
```

### 3. dnsserver 模块 - 支持混合 IP

**改动：**
- 优先使用 `QueryAll()` 获取所有 IP
- `buildDNSResponse()` 按类型过滤返回

## 配置优化建议

### 高可用场景
```yaml
ping:
  count: 5           # 多次 ping，提高失败判定准确度
  timeout_ms: 1000   # 增加超时，避免网络抖动误判
  strategy: "avg"    # 使用平均 RTT
```

### 快速响应场景
```yaml
ping:
  count: 2
  timeout_ms: 300
  strategy: "min"
```

## 验证检查清单

- [ ] A 查询返回所有 IPv4
- [ ] AAAA 查询返回所有 IPv6
- [ ] Ping 通的 IP 排在前面
- [ ] Ping 不通的 IP 排在后面
- [ ] Web API 返回 RTT = 999999 表示失败
- [ ] 日志显示完整的 IP 列表和 RTT
- [ ] 缓存包含所有 IP + RTT 信息

## 故障排查

### Q: 一些 IP 没有返回？
A: 检查 ping 模块是否仍有过滤。查看日志中的 "Ping results" 是否包含所有 IP。

### Q: 失败的 IP 没有排在最后？
A: 确保 `pingIP()` 返回 RTT=999999 的结果，检查 `sortResults()` 排序逻辑。

### Q: Web API 返回的 RTT 值不对？
A: 检查缓存中是否正确存储了 RTT 数组。确保 `RTTs` 长度与 `IPs` 一致。

---

**现在 SmartDNSSort 会返回所有 IP，包括 Ping 不通的！** ✅
