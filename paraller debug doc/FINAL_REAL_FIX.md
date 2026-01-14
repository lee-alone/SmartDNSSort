# 最终修复：去除 cached DNSSEC Message 中的重复IP

## 问题回顾

尽管我们在 `buildDNSResponseWithDNSSEC` 等函数中添加了IP去重逻辑，但用户仍然报告存在重复IP。

## 根本原因分析 (The Real Root Cause)

经过仔细代码审查，我们发现了一个被遗漏的路径：

1. **并行查询 (Parallel Query)** 从上游获取结果。
2. 上游结果中包含 `DnsMsg` (原始 DNS 消息)。由于上游 DNS 服务器可能返回重复记录，这个 `DnsMsg` 可能包含重复项。
3. 在 `dnsserver/handler_query.go` 中，如果我们启用了 DNSSEC (`currentCfg.Upstream.Dnssec`)，我们会将这个**原始的、可能包含重复记录的** `DnsMsg` 直接存入 `msgCache`：

   ```go
   // handler_query.go
   if result.DnsMsg != nil {
       // ...
       setDNSSECMsgToCache(domain, qtype, result.DnsMsg) // <--- 这里存入了脏数据
   }
   ```

4. 当后续请求（或当前请求）命中这个 `msgCache` 时：

   ```go
   // handler_query.go
   if entry, found := s.cache.GetDNSSECMsg(domain, qtype); found {
       responseMsg := entry.Message.Copy()
       w.WriteMsg(responseMsg) // <--- 直接返回了包含重复记录的原始消息
       return
   }
   ```

   **注意**：这里直接写入了 `responseMsg`，**绕过了** 我们之前修复的所有 `buildDNSResponse...` 函数！

## 解决方案

我们在将 `result.DnsMsg` 存入缓存之前，对其进行强制去重。

### 1. 新增去重工具函数

在 `dnsserver/handler_response.go` 中添加了 `deduplicateDNSMsg` 和 `deduplicateRecords` 函数，用于清理 `dns.Msg` 中的 `Answer`, `Ns`, `Extra` 部分。

### 2. 在缓存前调用去重

在 `dnsserver/handler_query.go` 中，在调用 `setDNSSECMsgToCache` 之前，先调用 `s.deduplicateDNSMsg(result.DnsMsg)`。

```go
// [Fix] 在缓存前去除重复记录
s.deduplicateDNSMsg(result.DnsMsg)
```

## 验证

这次修复不仅解决了通过 `buildDNSResponse` 构建的响应（之前的修复已覆盖），还解决了直接通过 `msgCache` 服务的高速响应路径。

请重新编译并验证。
