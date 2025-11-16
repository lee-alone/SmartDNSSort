# SmartDNSSort 使用指南 - 获取排序后的 IP 及 RTT 信息

## 核心工作流程

SmartDNSSort 现在完全按照你的需求工作：

```
1. 获取 IP
   └─ 从上游 DNS 服务器并发查询域名，获取所有 IP

2. 测试 IP
   └─ 使用 TCP Ping 对所有 IP 进行延迟测试

3. 排序 IP
   └─ 按 RTT（延迟时间）从低到高排序所有 IP

4. 返回结果
   ├─ DNS 查询：按排序顺序返回所有 IP（DNS 标准协议）
   └─ Web API：返回 IP + RTT 信息（JSON 格式）
```

## 使用方式

### 方式 1: 标准 DNS 查询（仅返回 IP，按 RTT 排序）

最快的 IP 会被放在返回结果的最前面。

**Windows:**
```powershell
nslookup google.com 127.0.0.1
nslookup -type=AAAA google.com 127.0.0.1
```

**Linux/macOS:**
```bash
dig @127.0.0.1 google.com
dig @127.0.0.1 google.com AAAA
```

**输出示例：**
```
Address: 142.251.48.14    (RTT 最短，返回第 1 个)
Address: 142.251.48.46    (RTT 较短，返回第 2 个)
Address: 142.251.48.78    (RTT 较长，返回第 3 个)
```

### 方式 2: Web API 查询（返回 IP + RTT 信息）

启用 WebAPI 后，可以获取完整的 RTT 信息。

**启用 WebAPI 步骤：**
1. 编辑 `config.yaml`：
```yaml
webui:
  enabled: true
  listen_port: 8080
```

2. 启动服务器：`.\run.bat` 或 `go run ./cmd/main.go`

3. 查询 API：
```bash
# 查询 A 记录（IPv4）
curl "http://localhost:8080/api/query?domain=google.com&type=A"

# 查询 AAAA 记录（IPv6）
curl "http://localhost:8080/api/query?domain=google.com&type=AAAA"

# 查看统计信息
curl "http://localhost:8080/api/stats"

# 健康检查
curl "http://localhost:8080/health"
```

**API 返回示例：**
```json
{
  "domain": "google.com",
  "type": "A",
  "ips": [
    {"ip": "142.251.48.14", "rtt": 45},
    {"ip": "142.251.48.46", "rtt": 52},
    {"ip": "142.251.48.78", "rtt": 68}
  ],
  "status": "success"
}
```

## 转发给后端

如果你需要将排序后的 IP 及 RTT 转发给后端服务，可以：

### 方案 1: 直接调用 Web API

后端直接调用 Web API 获取排序后的 IP 和 RTT：

```python
import requests

# 查询排序后的 IP
response = requests.get("http://localhost:8080/api/query?domain=google.com&type=A")
result = response.json()

print(f"域名: {result['domain']}")
print(f"查询类型: {result['type']}")
print("排序后的 IP 和 RTT:")
for ip_info in result['ips']:
    print(f"  {ip_info['ip']}: {ip_info['rtt']}ms")
```

### 方案 2: 在后端自己调用 DNS

如果后端在另一台机器，可以将 SmartDNSSort 作为 DNS 服务器：

```bash
# 后端机器的 /etc/hosts 或 DNS 设置，指向 SmartDNSSort 服务器
# nameserver 192.168.1.100  (SmartDNSSort 所在的 IP)
```

后端的标准 DNS 查询会自动获得排序后的 IP：

```python
import socket

# 获取排序后的 IP（已按 RTT 排序）
addresses = socket.getaddrinfo("google.com", 443)
for addr in addresses:
    print(f"IP: {addr[4][0]}")  # 最快的 IP 会在前面
```

### 方案 3: REST 中间件转发

如果后端需要同时获取 IP 和 RTT，可以创建一个简单的中间件：

**后端调用示例（Python）：**
```python
import requests

def get_fastest_ip(domain):
    """获取域名的所有排序 IP 和 RTT"""
    response = requests.get(
        "http://localhost:8080/api/query",
        params={"domain": domain, "type": "A"}
    )
    
    if response.status_code == 200:
        data = response.json()
        return data['ips']  # 返回 [{"ip": "...", "rtt": ...}, ...]
    else:
        return None

# 使用
ips = get_fastest_ip("google.com")
if ips:
    fastest_ip = ips[0]['ip']  # 获取最快的 IP
    print(f"使用最快的 IP: {fastest_ip}")
```

## 配置调优

### 针对不同场景的优化建议

**场景 1: 高精度 RTT 排序（推荐用于 CDN 选择）**
```yaml
ping:
  count: 5          # 增加 ping 次数，提高精度
  timeout_ms: 800   # 增加超时，避免超时判定
  concurrency: 32   # 提高并发能力
  strategy: "avg"   # 使用平均 RTT
```

**场景 2: 快速响应（推荐用于 DNS 服务）**
```yaml
ping:
  count: 2          # 减少 ping 次数
  timeout_ms: 300   # 降低超时
  concurrency: 16   # 适度并发
  strategy: "min"   # 使用最小 RTT
```

**场景 3: 生产环境（平衡方案）**
```yaml
ping:
  count: 3
  timeout_ms: 500
  concurrency: 16
  strategy: "min"

cache:
  ttl_seconds: 600  # 延长缓存时间，减少重复测试

upstream:
  concurrency: 4
  timeout_ms: 300
```

## 监控和调试

### 查看完整的排序日志

启动服务器时，会看到详细的排序日志：

```
Query: google.com (type=A)
Upstream query: google.com -> [142.251.48.14 142.251.48.46 142.251.48.78]
Ping results for google.com: [142.251.48.14 142.251.48.46 142.251.48.78] with RTTs: [45 52 68]
Building DNS response for google.com (type=A) with IPs: [142.251.48.14 142.251.48.46 142.251.48.78]
```

### 验证排序是否生效

查询两次同一个域名，第二次应该从缓存返回相同顺序：

```powershell
# 第一次（测试并排序）
nslookup google.com 127.0.0.1

# 第二次（从缓存返回，顺序应该一致）
nslookup google.com 127.0.0.1
```

## 工作流总结

| 步骤 | 操作 | 输入 | 输出 |
|------|------|------|------|
| 1 | 接收查询 | DNS 查询 | 域名 + 查询类型 |
| 2 | 查询缓存 | 域名 + 类型 | 缓存 IP + RTT（或未命中） |
| 3 | 上游查询 | 域名 + 类型 | IP 列表（未排序） |
| 4 | Ping 测试 | IP 列表 | IP + RTT 列表 |
| 5 | 排序 | IP + RTT 列表 | 按 RTT 排序的 IP + RTT |
| 6 | 缓存存储 | IP + RTT | 缓存项（含过期时间） |
| 7 | DNS 响应 | 排序后的 IP | DNS 答案（按顺序） |
| 7b | Web API 响应 | 排序后的 IP + RTT | JSON（含 RTT 信息） |

## API 参考

### GET /api/query

查询排序后的 IP 和 RTT 信息。

**参数：**
- `domain` (必需): 域名，如 `google.com`
- `type` (可选): 查询类型，`A` 或 `AAAA`，默认 `A`

**返回值：**
```json
{
  "domain": "string",
  "type": "string",
  "ips": [
    {
      "ip": "string",
      "rtt": "number (毫秒)"
    }
  ],
  "status": "success"
}
```

**示例：**
```bash
curl "http://localhost:8080/api/query?domain=example.com&type=A"
```

### GET /api/stats

获取 DNS 服务统计信息。

**返回值：**
```json
{
  "total_queries": 100,
  "cache_hits": 80,
  "cache_misses": 20,
  "cache_hit_rate": 80.0,
  "ping_successes": 150,
  "ping_failures": 5,
  "average_rtt_ms": 52
}
```

### GET /health

健康检查端点。

**返回值：**
```json
{
  "status": "healthy"
}
```

---

## 故障排查

### Q: Web API 无法访问？
**A:** 确保在 config.yaml 中设置 `webui.enabled: true`，并重启服务器。

### Q: API 返回 "Domain not found in cache"？
**A:** 这是正常的。首次查询需要从上游 DNS 获取并进行 ping 测试。查询一次后缓存会生效。

### Q: RTT 信息总是 0？
**A:** 检查 TCP ping 是否成功。确保上游 IP 的 80/443 端口可访问。

### Q: 排序顺序不一致？
**A:** 这是正常的，因为网络延迟可能波动。可以增加 `ping.count` 提高精度，或增加 `cache.ttl_seconds` 延长缓存。

---

**现在 SmartDNSSort 完全按照你的设计工作了！** 🎉

所有 IP 都被测试并按 RTT 排序，既通过 DNS 协议返回排序的 IP，也通过 Web API 返回完整的 RTT 信息，方便转发给后端。
