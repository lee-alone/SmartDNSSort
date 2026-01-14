# CNAME 缓存键冲突风险场景分析

## 关键发现

经过代码分析，**CNAME 缓存键冲突风险在当前实现中实际上不会出现**。原因如下：

---

## 缓存键的生成方式

```go
// cache/cache_dnssec.go
key := cacheKey(domain, qtype)
```

缓存键是基于 `(domain, qtype)` 生成的。这意味着：
- `www.example.com:A` 和 `cdn.example.com:A` 是**不同的缓存键**
- 即使存储相同的消息，也会在不同的键下

---

## 当前代码的行为

```go
// handler_query.go
setDNSSECMsgToCache(domain, qtype, result.DnsMsg)

for _, cname := range fullCNAMEs {
    cnameDomain := strings.TrimRight(cname, ".")
    setDNSSECMsgToCache(cnameDomain, qtype, result.DnsMsg)
}
```

**实际存储**:
```
缓存键: www.example.com:A
缓存值: 完整消息（包含 CNAME 链和最终 A 记录）

缓存键: cdn.example.com:A
缓存值: 同一完整消息（包含 CNAME 链和最终 A 记录）
```

---

## 为什么你的测试是正常的

当查询 `cdn.example.com A` 时：

1. **缓存查询**:
   ```go
   if entry, found := s.cache.GetDNSSECMsg("cdn.example.com", dns.TypeA); found {
       responseMsg := entry.Message.Copy()
       w.WriteMsg(responseMsg)
       return
   }
   ```

2. **返回的消息**:
   ```
   Question: cdn.example.com A
   Answer:
     www.example.com CNAME cdn.example.com
     cdn.example.com A 1.2.3.4
   ```

3. **为什么正常**:
   - DNS 客户端收到这个响应
   - 客户端看到 `www.example.com CNAME cdn.example.com`
   - 客户端理解这是一个 CNAME 链
   - 客户端最终使用 `1.2.3.4`
   - **没有问题**，因为 CNAME 链是有效的信息

---

## 真正的风险场景

风险**只在以下特定情况下出现**：

### 场景 1: 直接查询 CNAME 链中间的域名（不常见）

**假设**:
```
www.example.com -> CNAME -> cdn.example.com -> CNAME -> real.example.com -> A 1.2.3.4
```

**首次查询**: `www.example.com A`
- 存储到缓存:
  - `www.example.com:A` -> 完整消息
  - `cdn.example.com:A` -> 完整消息
  - `real.example.com:A` -> 完整消息

**后续查询**: `cdn.example.com A`（直接查询中间的 CNAME）
- 缓存命中，返回完整消息
- 消息包含: `www.example.com CNAME cdn.example.com`
- **问题**: 客户端收到的 CNAME 指向 `cdn.example.com`，但查询的就是 `cdn.example.com`
- **实际影响**: 大多数 DNS 客户端会忽略这个"自指向"的 CNAME，继续使用 A 记录

### 场景 2: 上游返回不同的 CNAME 链（极端情况）

**假设**:
```
首次查询 www.example.com:
  返回: www.example.com CNAME cdn.example.com
        cdn.example.com A 1.2.3.4

后续查询 cdn.example.com:
  上游返回: cdn.example.com A 1.2.3.5  (不同的 IP)
```

**当前行为**:
- 缓存中已有 `cdn.example.com:A` 的完整消息
- 直接返回缓存，不再查询上游
- 返回 `1.2.3.4` 而不是 `1.2.3.5`

**这是缓存的正常行为**，不是 CNAME 冲突问题。

---

## 真正需要关注的问题

### 问题 1: 缓存污染（Cache Pollution）

**场景**:
```
查询 1: www.example.com A
  返回: www.example.com CNAME cdn.example.com
        cdn.example.com A 1.2.3.4

查询 2: cdn.example.com A (直接查询，不经过 www.example.com)
  上游返回: cdn.example.com A 1.2.3.5

当前行为:
  缓存中已有 cdn.example.com:A -> 返回 1.2.3.4
  实际应该返回: 1.2.3.5
```

**风险等级**: 中等
**原因**: 不同的查询路径可能导致不同的结果，但缓存键相同

**何时出现**: 
- 用户直接查询 CNAME 链中的中间域名
- 该域名的 IP 与通过 CNAME 链查询时不同

---

### 问题 2: RRSIG 记录的去重

**当前代码**:
```go
// handler_response.go
s.deduplicateDNSMsg(result.DnsMsg)  // 在缓存前去重
```

**潜在问题**:
```go
// deduplicateRecords 中
default:
    originalTTL := header.Ttl
    header.Ttl = 0
    key = rr.String()
    header.Ttl = originalTTL
```

**风险**: RRSIG 记录可能被错误去重

**何时出现**:
- 上游返回多个 RRSIG 记录（用于不同的密钥或算法）
- 去重后只保留一个，导致 DNSSEC 验证失败

**风险等级**: 低（通常上游不会返回重复的 RRSIG）

---

## IP 去重的影响分析

### 当前 IP 去重逻辑

```go
// handler_response.go
ipSet := make(map[string]bool)
for _, ip := range ips {
    parsedIP := net.ParseIP(ip)
    if parsedIP == nil {
        continue
    }
    
    ipStr := parsedIP.String()
    if ipSet[ipStr] {
        continue  // 跳过重复
    }
    ipSet[ipStr] = true
    // 添加到响应
}
```

### 影响分析

**正面影响**:
- ✅ 消除重复 IP
- ✅ 减少响应大小
- ✅ 提高客户端兼容性

**潜在问题**:
- ❌ 改变了 IP 的顺序（如果上游故意排序）
- ❌ 改变了 IP 的数量（某些应用可能依赖重复 IP 表示权重）

**何时出现问题**:
- 某些 CDN 或负载均衡器故意返回重复 IP 表示权重
- 去重后改变了原始意图

**风险等级**: 低（RFC 规范不允许重复记录）

---

## 总结：风险出现的时机

| 风险 | 出现时机 | 概率 | 影响 |
|------|--------|------|------|
| CNAME 自指向 | 直接查询 CNAME 链中间的域名 | 低 | 低（客户端通常忽略） |
| 缓存污染 | 同一域名通过不同路径查询，结果不同 | 低 | 中（返回过期数据） |
| RRSIG 去重 | 上游返回多个 RRSIG | 极低 | 中（DNSSEC 验证失败） |
| IP 权重改变 | 上游故意返回重复 IP 表示权重 | 极低 | 低（通常不影响功能） |

---

## 建议

### 立即行动（高优先级）
1. **监控缓存命中率** - 确保缓存策略有效
2. **测试 DNSSEC 验证** - 确保 RRSIG 记录完整

### 后续优化（中优先级）
1. **区分查询路径** - 为不同的查询路径使用不同的缓存键
2. **RRSIG 保护** - 对 RRSIG 记录不进行去重

### 监控指标
- 缓存命中率
- DNSSEC 验证成功率
- 直接查询 CNAME 链中间域名的频率

---

## 结论

**你的测试正常的原因**:
- 缓存键基于 `(domain, qtype)` 生成
- 不同的域名有不同的缓存键
- 即使存储相同的消息，也不会相互覆盖
- CNAME 链在响应中是有效的信息，不会导致问题

**真正的风险**:
- 缓存污染（同一域名通过不同路径查询）
- RRSIG 记录的去重（极低概率）

**当前实现是安全的**，可以继续使用。

