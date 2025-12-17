# DNSSEC 处理修复总结

## 问题分析

根据 dig.txt 的对比结果，192.168.1.13 (本地 SmartDNSSort) 的响应相比 192.168.1.11 (上游) 缺失以下内容：

| 项目 | 上游 (192.168.1.11) | 本地 (192.168.1.13) |
|-----|-----------------|-----------------|
| flags | `qr rd ra ad` | `qr rd` |
| DNSSEC验证 | ✓ (ad flag) | ✗ |
| 递归可用 | ✓ (ra flag) | ✗ |
| RRSIG 记录 | 有 | 无 |

## 根本原因

1. **缺失 `ra` (Recursion Available) flag**：所有响应都没有设置 `msg.RecursionAvailable = true`
2. **缺失 `ad` (Authenticated Data) flag**：上游的 DNSSEC 验证标记未被转发
3. **丢失 RRSIG 记录**：缓存结构只保存 IP/CNAME，未保留完整的 DNS 记录

## 修复方案

### 1. 修复递归可用标记 (ra flag)

**文件修改：**
- [dnsserver/utils.go](dnsserver/utils.go)：修复 `buildNXDomainResponse`、`buildZeroIPResponse`、`buildRefuseResponse` 三个函数
- [dnsserver/handler.go](dnsserver/handler.go)：在所有 `msg.SetReply(r)` 后添加 `msg.RecursionAvailable = true`

**修改内容：**
```go
// 示例：所有响应都现在设置
msg := new(dns.Msg)
msg.SetReply(r)
msg.RecursionAvailable = true  // ← 新增
// ...
w.WriteMsg(msg)
```

### 2. 修复 DNSSEC 验证标记 (ad flag)

**文件修改：**

#### 2.1 缓存层 ([cache/cache.go](cache/cache.go))
- 在 `RawCacheEntry` 结构体中添加 `AuthenticatedData bool` 字段
- 新增 `SetRawWithDNSSEC()` 方法，保留 DNSSEC 标记

```go
type RawCacheEntry struct {
    IPs                []string
    CNAMEs             []string
    UpstreamTTL        uint32
    AcquisitionTime    time.Time
    AuthenticatedData  bool  // ← 新增 DNSSEC 验证标记
}
```

#### 2.2 上游查询层 ([upstream/manager.go](upstream/manager.go))
- 在 `QueryResult` 和 `QueryResultWithTTL` 中添加 `AuthenticatedData bool` 字段
- 在所有查询策略（parallel、random、sequential、racing）中捕获上游响应的 `reply.AuthenticatedData`

```go
type QueryResult struct {
    IPs                []string
    CNAMEs             []string
    TTL                uint32
    Server             string
    Rcode              int
    AuthenticatedData  bool  // ← 新增
}
```

#### 2.3 DNS 处理层 ([dnsserver/handler.go](dnsserver/handler.go))
- 新增 `buildDNSResponseWithDNSSEC()` 和 `buildDNSResponseWithCNAMEAndDNSSEC()` 方法
- 在缓存和响应中传递 DNSSEC 标记
- 保存时：`s.cache.SetRawWithDNSSEC(domain, qtype, finalIPs, fullCNAMEs, finalTTL, result.AuthenticatedData)`
- 返回时：`msg.AuthenticatedData = authData`

## 修改的文件列表

1. **dnsserver/utils.go**
   - 修复 `buildNXDomainResponse()` - 添加 SetReply 和 RecursionAvailable
   - 修复 `buildZeroIPResponse()` - 添加 RecursionAvailable
   - 修复 `buildRefuseResponse()` - 添加 SetReply 和 RecursionAvailable

2. **dnsserver/handler.go**
   - 所有 DNS 响应构建位置添加 `msg.RecursionAvailable = true`
   - 新增 `buildDNSResponseWithDNSSEC()` 方法
   - 新增 `buildDNSResponseWithCNAMEAndDNSSEC()` 方法
   - 使用 `SetRawWithDNSSEC()` 保存 DNSSEC 标记
   - 在所有缓存命中处理中使用新方法传递 DNSSEC 标记

3. **upstream/manager.go**
   - `QueryResult` 添加 `AuthenticatedData bool` 字段
   - `QueryResultWithTTL` 添加 `AuthenticatedData bool` 字段
   - `queryParallel()` 中捕获 `reply.AuthenticatedData`
   - `queryRandom()` 中捕获并返回 `reply.AuthenticatedData`
   - `querySequential()` 中捕获并返回 `reply.AuthenticatedData`
   - `queryRacing()` 中捕获并返回 `reply.AuthenticatedData`

4. **cache/cache.go**
   - `RawCacheEntry` 添加 `AuthenticatedData bool` 字段
   - 新增 `SetRawWithDNSSEC()` 方法（向后兼容：旧 `SetRaw()` 调用新方法）

## 预期效果

修复后，使用 `dig +dnssec @192.168.1.13 cloudflare.com` 查询应该返回：

- ✓ `flags: qr rd ra ad` (包含 `ra` 和 `ad` 标记)
- ✓ RRSIG 记录（如果上游支持）
- ✓ AuthenticatedData 标记设置

## 限制说明

当前修复**不能返回 RRSIG 记录**，原因是：
- 缓存结构只保存 IP 和 CNAME，不保存完整的 DNS 记录集
- RRSIG 是额外的 DNS 记录类型，需要完整的消息缓存架构

若要完全支持 RRSIG，需要：
1. 修改缓存结构保存完整的 `*dns.Msg` 或 `Answer []dns.RR`
2. 修改所有响应构建逻辑直接使用缓存的记录
3. 这是一个较大的架构改进，可在后续版本实施

## 测试方法

```bash
# 对比两个服务器的响应
dig +dnssec @192.168.1.11 cloudflare.com  # 上游
dig +dnssec @192.168.1.13 cloudflare.com  # 本地 (修复后)

# 应该看到相同的 flags: qr rd ra ad
# 注意: RRSIG 仍然不会显示（因为缓存架构限制）
```

## 代码编译验证

所有修改已通过 Go 编译检查，无编译错误。
