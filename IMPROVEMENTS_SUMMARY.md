# 代码改进总结

## 1. 消除重复的上游查询（主要优化）

### 问题分析
在原始代码中，当处理 DNSSEC 请求时：
1. `handleCacheMiss` 调用 `upstream.Query()` 获取 IPs 和 CNAMEs
2. 随后调用 `getDNSSECFullMessage()` 再次向上游发起相同的查询以获取原始 `*dns.Msg`
3. 结果：**每个首次发起的 DNSSEC 查询都触发两次网络请求**

### 解决方案
通过以下三步改造来消除重复查询：

#### 步骤 1: 修改 `upstream/manager.go`

**修改 `QueryResult` 结构体** - 添加 `DnsMsg` 字段：
```go
type QueryResult struct {
	IPs               []string
	CNAMEs            []string
	TTL               uint32
	Error             error
	Server            string
	Rcode             int
	AuthenticatedData bool
	DnsMsg            *dns.Msg // 新增：原始 DNS 消息
}
```

**修改 `QueryResultWithTTL` 结构体** - 添加 `DnsMsg` 字段：
```go
type QueryResultWithTTL struct {
	IPs               []string
	CNAMEs            []string
	TTL               uint32
	AuthenticatedData bool
	DnsMsg            *dns.Msg // 新增：原始 DNS 消息
}
```

**更新所有查询方法**：
- `queryParallel()`: 在成功的查询结果中保存 `reply.Copy()` 到 `DnsMsg`
- `querySequential()`: 在成功的查询结果中保存 `reply.Copy()` 到 `DnsMsg`
- `queryRandom()`: 在成功的查询结果中保存 `reply.Copy()` 到 `DnsMsg`
- `queryRacing()`: 在成功的查询结果中保存 `reply.Copy()` 到 `DnsMsg`

#### 步骤 2: 重构 `dnsserver/handler.go` 中的 `handleCacheMiss`

**原始代码逻辑**（被删除）：
```go
// 获取完整消息的单独查询
if fullMsg, err := s.getDNSSECFullMessage(ctx, msgReq, currentUpstream); err == nil && fullMsg != nil {
    s.cache.SetMsg(targetDomain, qtype, fullMsg)
}
```

**新的代码逻辑**（使用 Query 返回的消息）：
```go
// 直接使用 Query 返回的原始 DNS 消息
if currentCfg.Upstream.Dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
    if result.DnsMsg != nil {
        // 为请求的域名存储消息
        s.cache.SetMsg(domain, qtype, result.DnsMsg)
        
        // 为 CNAME 链中的每个域名都存储消息（方案 A）
        for _, cname := range fullCNAMEs {
            cnameDomain := strings.TrimRight(cname, ".")
            s.cache.SetMsg(cnameDomain, qtype, result.DnsMsg)
        }
    }
}
```

#### 步骤 3: 删除冗余的辅助函数

**删除** `getDNSSECFullMessage()` 函数 - 该函数已不再需要。

### 性能提升
- ✅ **消除了重复的网络请求**：从 2 次查询减少到 1 次
- ✅ **降低了延迟**：减少了不必要的网络往返时间
- ✅ **节省资源**：减少了上游服务器的负载

---

## 2. 修复 CNAME 的 msgCache 缓存键问题

### 问题分析
当一个域名 (如 www.a.com) 是 CNAME 到 www.b.com 时：
- `handleCacheMiss` 中使用 www.b.com 作为 msgCache 的键存储缓存
- `handleQuery` 中使用原始域名 www.a.com 去查询 msgCache
- 结果：**CNAME 缓存永远无法命中**

### 解决方案（方案 A - 推荐）

在 `SetMsg` 时，为 CNAME 链中的**每一个域名**都写入缓存。

**实现细节**：
```go
// 为请求的域名存储消息
s.cache.SetMsg(domain, qtype, result.DnsMsg)

// 为 CNAME 链中的每个域名都存储消息
// 如果 a -> b -> c，则分别为 a, b, c 设置缓存
for _, cname := range fullCNAMEs {
    cnameDomain := strings.TrimRight(cname, ".")
    s.cache.SetMsg(cnameDomain, qtype, result.DnsMsg)
}
```

**优势**：
- ✅ 实现直接简洁，无需复杂的指针追踪
- ✅ 后续对链中**任意环节**的查询都能命中缓存
- ✅ 符合实际使用场景（用户可能查询链中任何域名）

**权衡**：
- 会消耗额外的内存来存储重复的缓存项（但通常 CNAME 链较短，影响有限）

---

## 文件修改清单

| 文件 | 修改内容 |
|------|--------|
| `upstream/manager.go` | • 添加 `DnsMsg` 字段到 `QueryResult` 和 `QueryResultWithTTL`<br>• 更新 `queryParallel()`, `querySequential()`, `queryRandom()`, `queryRacing()` 方法保存原始 DNS 消息 |
| `dnsserver/handler.go` | • 重构 `handleCacheMiss()` 中的 DNSSEC msgCache 处理逻辑<br>• 删除 `getDNSSECFullMessage()` 函数<br>• 实现方案 A：为 CNAME 链中每个域名都写入 msgCache |

---

## 验证和测试建议

1. **功能测试**：
   - 验证 DNSSEC 请求是否只发起一次上游查询
   - 测试 CNAME 链的 msgCache 缓存命中情况

2. **性能测试**：
   - 测量消除重复查询后的延迟改进
   - 比较 Query 方法的执行时间

3. **兼容性检查**：
   - 确保所有不同的查询策略（parallel, sequential, random, racing）都正确工作
   - 验证错误处理是否仍然正确

---

## 代码质量改进

- ✅ 代码逻辑更清晰：消除了冗余的函数调用
- ✅ 减少了代码复杂度：删除了 `getDNSSECFullMessage()` 这个单一用途的辅助函数
- ✅ 更好的性能特性：一次网络请求获取所有需要的数据
- ✅ 更强的一致性：CNAME 链的所有域名都能有效利用缓存
