# Bug 修复：异步缓存刷新导致 DNS 记录丢失

## 问题描述

在异步缓存刷新时，TXT、MX、SRV 等非 IP 类型的 DNS 记录会被丢失，导致这些记录在缓存刷新后返回空结果。

## 根本原因

`dnsserver/refresh.go` 中的 `refreshCacheAsync` 函数在第 71 行调用了 `s.cache.SetRaw()`：

```go
s.cache.SetRaw(domain, qtype, finalIPs, fullCNAMEs, finalTTL)
```

而 `SetRaw` 方法会强制将 `Records` 字段设置为 `nil`：

```go
// cache/cache_raw.go
func (c *Cache) SetRawWithDNSSEC(domain string, qtype uint16, ips []string, cnames []string, upstreamTTL uint32, authData bool) {
	entry := &RawCacheEntry{
		Records:           nil, // ← 强制设置为 nil！
		IPs:               ips,
		CNAMEs:            cnames,
		// ...
	}
	c.rawCache.Set(key, entry)
}
```

## 影响范围

| DNS 记录类型 | 首次查询 | 刷新后 | 受影响 |
|------------|--------|-------|------|
| A/AAAA     | ✅ 正常 | ✅ 正常 | ❌ 否 |
| TXT        | ✅ 正常 | ❌ 空   | ✅ 是 |
| MX         | ✅ 正常 | ❌ 空   | ✅ 是 |
| SRV        | ✅ 正常 | ❌ 空   | ✅ 是 |
| CNAME      | ✅ 正常 | ✅ 正常 | ❌ 否 |

**为什么 A/AAAA 不受影响？** 因为响应构建逻辑可以从 `IPs` 字段重建 A/AAAA 记录。

## 修复方案

将 `refreshCacheAsync` 中的缓存写入改为使用 `SetRawRecords`，这个方法会正确保存完整的 DNS 记录：

```go
// 修复前
s.cache.SetRaw(domain, qtype, finalIPs, fullCNAMEs, finalTTL)

// 修复后
s.cache.SetRawRecords(domain, qtype, recordsToCache, fullCNAMEs, finalTTL)
```

### 关键改动

1. **使用正确的 API**：`SetRawRecords` 而不是 `SetRaw`
2. **保存完整的记录**：在 CNAME 递归的情况下，保存原始查询的记录（包含 CNAME），而不是递归结果的记录
3. **保持 IPs 和 CNAMEs 的一致性**：`SetRawRecords` 会从 `records` 中自动派生 `IPs` 字段

## 修复后的行为

```go
// 首次查询 TXT 记录
Query: _spf.example.com TXT
Result: Records=[TXT("v=spf1 include:...")], IPs=[], CNAMEs=[]

// 1小时后异步刷新
refreshCacheAsync 调用 SetRawRecords
缓存更新: Records=[TXT("v=spf1 include:...")], IPs=[], CNAMEs=[]

// 后续查询返回正确结果
Query: _spf.example.com TXT
Result: TXT 记录正常返回 ✓
```

## 文件修改

- `dnsserver/refresh.go`：修改 `refreshCacheAsync` 函数，使用 `SetRawRecords` 替代 `SetRaw`

## 测试建议

1. 查询 TXT 记录（如 SPF 记录）
2. 等待缓存过期（或手动触发刷新）
3. 验证刷新后 TXT 记录仍然返回正确结果
4. 对 MX、SRV 等其他非 IP 记录类型进行相同测试

## 严重程度

🔴 **高** - 虽然不会导致服务崩溃，但会导致依赖 DNS 记录的服务（SPF/反垃圾邮件、TLSA、SRV 服务发现）间歇性失效。
