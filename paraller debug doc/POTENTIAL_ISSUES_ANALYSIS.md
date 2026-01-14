# 去重修改的潜在问题分析

## 修改概述
在缓存 DNSSEC 消息前调用 `deduplicateDNSMsg()` 来清理上游返回的重复记录。

---

## 1. 性能影响分析

### 1.1 去重操作的性能开销
**问题**: `deduplicateRecords()` 为每条记录生成去重键，涉及字符串操作和 map 查询。

**影响程度**: 中等
- 对于小规模记录集（< 100 条）：影响可忽略
- 对于大规模记录集（> 1000 条）：可能产生明显延迟

**具体成本**:
```go
// 每条记录的成本
- 字符串格式化: fmt.Sprintf() 调用
- Map 查询: O(1) 平均情况
- 内存分配: 新的 uniqueRecords 切片
```

**建议监控**:
- 记录缓存前的去重耗时
- 特别关注大型 DNS 响应（如 MX 记录查询）

---

## 2. 缓存一致性问题

### 2.1 TTL 处理的潜在风险
**问题**: `deduplicateRecords()` 中对 A/AAAA/CNAME 记录只比较核心内容，忽略 TTL。

```go
// 当前实现
case *dns.A:
    key = fmt.Sprintf("A:%s:%s", header.Name, r.A.String())
    // TTL 被忽略
```

**场景**: 如果上游返回同一 IP 但 TTL 不同的记录：
```
example.com. 300 IN A 1.2.3.4
example.com. 600 IN A 1.2.3.4  // 同一 IP，不同 TTL
```

**当前行为**: 只保留第一条，使用 300 秒 TTL
**潜在问题**: 
- 如果第二条的 TTL 更长，缓存会过早失效
- 如果第二条的 TTL 更短，缓存会过期后仍被使用

**风险等级**: 低（上游通常不会返回同一 IP 的不同 TTL）

---

## 3. DNSSEC 验证问题

### 3.1 RRSIG 记录的处理
**问题**: `deduplicateRecords()` 对 RRSIG 等签名记录使用通用处理。

```go
default:
    // 对于其他记录，临时将 TTL 设为 0 来生成键
    originalTTL := header.Ttl
    header.Ttl = 0
    key = rr.String()
    header.Ttl = originalTTL
```

**潜在问题**:
- RRSIG 记录包含签名数据，即使 TTL 相同，签名也可能不同
- 如果去重了不同的 RRSIG，可能导致 DNSSEC 验证失败

**风险等级**: 中等
**建议**: 对 RRSIG 等签名记录应该保留所有副本，不进行去重

---

## 4. 缓存键冲突问题

### 4.1 多个 CNAME 链的处理
**问题**: 代码在多个地方存储 DNSSEC 消息到缓存：

```go
// handler_query.go 中
setDNSSECMsgToCache(domain, qtype, result.DnsMsg)

for _, cname := range fullCNAMEs {
    cnameDomain := strings.TrimRight(cname, ".")
    setDNSSECMsgToCache(cnameDomain, qtype, result.DnsMsg)
}
```

**场景**: 
```
query: www.example.com -> CNAME cdn.example.com -> A 1.2.3.4
```

**存储的缓存**:
- `www.example.com:A` -> 完整消息（包含 CNAME 和 A 记录）
- `cdn.example.com:A` -> 同一完整消息

**潜在问题**:
- 当查询 `cdn.example.com` 时，返回的消息仍然包含原始的 CNAME 链
- 客户端可能收到不匹配的 CNAME 记录

**风险等级**: 高
**示例**:
```
查询: cdn.example.com A
返回: 
  www.example.com CNAME cdn.example.com  // 不匹配！
  cdn.example.com A 1.2.3.4
```

---

## 5. 并发访问问题

### 5.1 消息修改的线程安全性
**问题**: `deduplicateDNSMsg()` 直接修改 `result.DnsMsg`：

```go
s.deduplicateDNSMsg(result.DnsMsg)  // 直接修改原始消息
```

**场景**: 如果多个 goroutine 同时处理相同的查询（通过 singleflight）：
1. 第一个 goroutine 获得结果，调用 `deduplicateDNSMsg()`
2. 第二个 goroutine 等待相同结果
3. 两个 goroutine 都使用已修改的消息

**当前保护**: singleflight 确保只有一个 goroutine 执行查询，但修改后的消息被多个 goroutine 使用

**风险等级**: 低（singleflight 序列化了查询，但仍需注意）

---

## 6. 缓存失效问题

### 6.1 去重导致的缓存不一致
**问题**: 去重后的消息与原始消息不同，但缓存键相同。

**场景**:
1. 首次查询 `example.com A`，上游返回 `[1.2.3.4, 1.2.3.4]`
2. 去重后存储 `[1.2.3.4]`
3. 缓存命中，返回 `[1.2.3.4]`
4. 用户期望的是原始的 `[1.2.3.4, 1.2.3.4]`（虽然重复）

**风险等级**: 低（去重是正确的行为）

---

## 7. 错误处理问题

### 7.1 nil 消息的处理
**问题**: `deduplicateDNSMsg()` 检查 nil，但调用前没有验证：

```go
if result.DnsMsg != nil {
    s.deduplicateDNSMsg(result.DnsMsg)  // 已检查
}
```

**当前状态**: 安全（已有 nil 检查）

---

## 8. 特殊记录类型的处理

### 8.1 SRV、TXT 等记录的去重
**问题**: 对于 SRV、TXT 等记录，去重键生成可能不准确。

```go
default:
    // 使用 rr.String() 作为键
    key = rr.String()
```

**潜在问题**:
- `rr.String()` 包含 TTL，但我们设置了 TTL=0
- 对于 TXT 记录，如果内容相同但顺序不同，可能被认为不同

**风险等级**: 低（通常不会有重复的 SRV/TXT 记录）

---

## 9. 缓存过期时间问题

### 9.1 TTL 调整的准确性
**问题**: 在 `handleQuery` 中，缓存命中后调整 TTL：

```go
adjustTTL(responseMsg.Answer, elapsed)
```

**场景**: 如果去重改变了记录顺序，TTL 调整可能不准确

**风险等级**: 低（TTL 调整是基于时间差，与记录顺序无关）

---

## 10. 上游服务器兼容性问题

### 10.1 某些上游服务器的特殊行为
**问题**: 某些 DNS 服务器可能故意返回重复记录（虽然不规范）。

**场景**: 
- 某些 CDN 的 DNS 服务器可能返回重复 IP 用于负载均衡
- 去重后改变了原始意图

**风险等级**: 低（RFC 规范不允许重复记录）

---

## 总结与建议

| 问题 | 风险等级 | 建议 |
|------|--------|------|
| 性能开销 | 中 | 监控大型响应的去重耗时 |
| TTL 不一致 | 低 | 保留最小 TTL（当前实现保留第一条） |
| RRSIG 处理 | 中 | **需要改进**：对签名记录不去重 |
| CNAME 缓存键冲突 | 高 | **需要改进**：为 CNAME 链中的每个域名生成不同的消息 |
| 并发访问 | 低 | 当前 singleflight 已保护 |
| 缓存一致性 | 低 | 去重是正确行为 |
| 错误处理 | 低 | 已有 nil 检查 |
| 特殊记录 | 低 | 通常不会有重复 |
| TTL 调整 | 低 | 与记录顺序无关 |
| 上游兼容性 | 低 | 符合 RFC 规范 |

---

## 立即需要修复的问题

### 问题 1: CNAME 缓存键冲突（高风险）

**当前代码**:
```go
setDNSSECMsgToCache(domain, qtype, result.DnsMsg)
for _, cname := range fullCNAMEs {
    cnameDomain := strings.TrimRight(cname, ".")
    setDNSSECMsgToCache(cnameDomain, qtype, result.DnsMsg)
}
```

**问题**: 为 CNAME 链中的每个域名存储相同的完整消息，导致返回时 CNAME 记录不匹配。

**建议修复**:
```go
// 只为原始查询域名存储完整消息
setDNSSECMsgToCache(domain, qtype, result.DnsMsg)

// 对于 CNAME 链中的域名，不存储 msgCache
// 或者为每个域名生成对应的消息副本
```

### 问题 2: RRSIG 记录的去重（中风险）

**当前代码**:
```go
default:
    originalTTL := header.Ttl
    header.Ttl = 0
    key = rr.String()
    header.Ttl = originalTTL
```

**问题**: RRSIG 等签名记录可能被错误去重。

**建议修复**:
```go
case *dns.RRSIG:
    // 不去重 RRSIG 记录，保留所有副本
    uniqueRecords = append(uniqueRecords, rr)
    continue
```

