# DNS RFC 规范违规分析报告

## 执行摘要

本报告对 SmartDNSSort 项目进行了全面的 DNS RFC 规范合规性分析。分析涵盖了 DNS 消息格式、缓存策略、错误响应、DNSSEC 处理、记录去重、响应构建、上游查询和特殊域名处理等 8 个关键领域。

**发现的主要违规点：共 28 项**

---

## 1. DNS 消息格式和字段处理

### 1.1 【严重】EDNS0 OPT 记录处理不完整
**文件**: `upstream/manager_*.go`, `dnsserver/handler_query.go`
**RFC**: RFC 6891 (EDNS0)

**问题**:
- 代码在创建 DNSSEC 请求时设置 EDNS0，但没有正确处理响应中的 OPT 记录
- 硬编码 UDP 缓冲区大小为 4096 字节，未根据客户端请求调整
- 未验证 OPT 记录中的扩展字段（如 NSID、COOKIE 等）

**代码位置**:
```go
// upstream/manager_parallel.go:60-62
if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
    msg.SetEdns0(4096, true)  // 硬编码 4096，未考虑客户端请求
}
```

**建议修复**:
- 从客户端请求中提取 UDP 缓冲区大小
- 正确转发 OPT 记录中的扩展选项
- 验证 EDNS0 版本号

---

### 1.2 【中等】Question Section 验证不足
**文件**: `upstream/manager.go:118-119`, `dnsserver/handler_query.go:289-300`
**RFC**: RFC 1035 (DNS Protocol)

**问题**:
- 仅检查 Question 是否为空，未验证 Question 数量
- RFC 1035 允许多个 Question，但代码只处理第一个
- 未验证 Question 中的 QCLASS 和 QTYPE 有效性

**代码位置**:
```go
// upstream/manager.go:118-119
if len(r.Question) == 0 {
    return nil, errors.New("query message has no questions")
}
question := r.Question[0]  // 只处理第一个 Question
```

**建议修复**:
- 验证 Question 数量（通常应为 1）
- 验证 QCLASS 和 QTYPE 的有效性
- 对多个 Question 的情况进行明确处理

---

### 1.3 【中等】Message ID 和 Flags 处理
**文件**: `dnsserver/handler_response.go`, `dnsserver/handler_cache.go`
**RFC**: RFC 1035

**问题**:
- 使用 `msg.SetReply(r)` 自动复制 ID 和 Flags，但未验证 RD 标志
- 未检查 AA (Authoritative Answer) 标志的正确性
- 未处理 CD (Checking Disabled) 标志

**建议修复**:
- 显式验证和设置 Flags
- 根据响应类型正确设置 AA 标志
- 处理 CD 标志以支持 DNSSEC 验证

---

## 2. 缓存策略和 TTL 处理

### 2.1 【严重】TTL 计算存在精度问题
**文件**: `dnsserver/sorting.go:82-103`, `dnsserver/handler_cache.go`
**RFC**: RFC 1035, RFC 2181

**问题**:
- TTL 计算使用浮点数 `time.Since().Seconds()`，可能导致精度丢失
- 未考虑时钟调整（NTP 同步）的影响
- TTL 可能变为负数，代码使用 `max(1, ...)` 作为兜底，但这违反了 RFC

**代码位置**:
```go
// dnsserver/sorting.go:84
elapsed := time.Since(acquisitionTime).Seconds()
remaining := int(upstreamTTL) - int(elapsed)  // 浮点转整数，精度丢失
```

**建议修复**:
- 使用整数毫秒计算，避免浮点精度问题
- 实现 NTP 时钟调整检测
- 当 TTL 过期时，应返回错误而非强制设为 1

---

### 2.2 【严重】负缓存 TTL 处理不规范
**文件**: `dnsserver/handler_cache.go:14-30`, `upstream/manager_utils.go:100-115`
**RFC**: RFC 2308 (Negative Caching)

**问题**:
- 从 SOA 记录的 Minimum 字段提取 TTL，但未考虑 SOA 记录本身的 TTL
- 未区分 NXDOMAIN 和 NODATA 的缓存策略
- 默认负缓存 TTL 为 300 秒，未根据 SOA 记录调整

**代码位置**:
```go
// upstream/manager_utils.go:100-115
func extractNegativeTTL(msg *dns.Msg) uint32 {
    for _, ns := range msg.Ns {
        if soa, ok := ns.(*dns.SOA); ok {
            ttl := soa.Hdr.Ttl
            minttl := min(soa.Minttl, ttl)
            return minttl  // 未考虑 RFC 2308 的完整规则
        }
    }
    return 300  // 硬编码默认值
}
```

**建议修复**:
- 正确实现 RFC 2308 的负缓存规则
- 区分 NXDOMAIN 和 NODATA 的处理
- 使用 SOA 记录的 Minimum 字段作为负缓存 TTL 的上限

---

### 2.3 【中等】缓存过期检查不一致
**文件**: `cache/entries.go`, `cache/cache_raw.go`
**RFC**: RFC 1035

**问题**:
- `GetRaw()` 不检查过期，但 `GetSorted()` 检查过期
- 导致返回过期缓存的行为不一致
- 可能导致客户端收到过期数据

**代码位置**:
```go
// cache/cache_raw.go:8-15
func (c *Cache) GetRaw(domain string, qtype uint16) (*RawCacheEntry, bool) {
    // 注意:此方法不检查过期,调用方需要自行判断是否过期
    // 这导致行为不一致
}
```

**建议修复**:
- 统一缓存过期检查逻辑
- 在所有 Get 方法中检查过期
- 提供单独的 "GetExpired" 方法用于特殊场景

---

### 2.4 【中等】UserReturnTTL 循环逻辑问题
**文件**: `dnsserver/handler_cache.go:50-65`
**RFC**: RFC 1035

**问题**:
- 使用 `cycleOffset` 和 `cappedTTL` 实现循环 TTL，但逻辑复杂且容易出错
- 当 `UserReturnTTL` 为 0 时，应使用原始 TTL，但代码处理不当

**代码位置**:
```go
if cfg.Cache.UserReturnTTL > 0 {
    cycleOffset := int(elapsedRaw.Seconds()) % cfg.Cache.UserReturnTTL
    cappedTTL := cfg.Cache.UserReturnTTL - cycleOffset
    // 这个逻辑容易导致 TTL 跳跃
}
```

**建议修复**:
- 简化 TTL 计算逻辑
- 明确文档化 UserReturnTTL 的语义
- 添加单元测试验证 TTL 计算

---

## 3. 错误响应处理

### 3.1 【严重】NXDOMAIN 和 NODATA 混淆
**文件**: `dnsserver/handler_query.go:159-174`, `dnsserver/handler_cache.go:14-30`
**RFC**: RFC 2308, RFC 1035

**问题**:
- 代码将 NODATA（域名存在但无此类型记录）和 NXDOMAIN（域名不存在）混为一谈
- 两者应使用不同的缓存策略和 SOA 记录
- 未正确设置 RCODE（NODATA 应为 NOERROR，NXDOMAIN 应为 NXDOMAIN）

**代码位置**:
```go
// dnsserver/handler_query.go:159-174
if len(finalIPs) == 0 && len(fullCNAMEs) == 0 {
    logger.Debugf("[handleQuery] 上游查询返回空结果 (NODATA): %s", domain)
    s.cache.SetError(domain, qtype, dns.RcodeSuccess, ...)  // 错误：NODATA 应为 NOERROR
}
```

**建议修复**:
- 区分 NXDOMAIN (Rcode=3) 和 NODATA (Rcode=0)
- 为 NODATA 响应添加 SOA 记录到 Authority section
- 实现正确的缓存策略

---

### 3.2 【中等】SERVFAIL 响应缺少 SOA 记录
**文件**: `dnsserver/handler_query.go:89-96`
**RFC**: RFC 2308

**问题**:
- SERVFAIL 响应添加了 SOA 记录，但 RFC 2308 建议 SERVFAIL 不应缓存
- 代码缓存 SERVFAIL 响应，违反 RFC 建议

**代码位置**:
```go
// dnsserver/handler_query.go:89-96
msg.SetRcode(r, dns.RcodeServerFailure)
soa := s.buildSOARecord(domain, uint32(currentCfg.Cache.ErrorCacheTTL))
msg.Ns = append(msg.Ns, soa)  // SERVFAIL 不应缓存
```

**建议修复**:
- 不缓存 SERVFAIL 响应
- 或使用极短的 TTL（如 1 秒）
- 添加配置选项控制 SERVFAIL 缓存行为

---

### 3.3 【中等】错误响应码提取不完整
**文件**: `dnsserver/utils.go:96-115`
**RFC**: RFC 1035

**问题**:
- `parseRcodeFromError()` 仅处理特定的错误格式
- 未处理所有可能的 DNS 错误码（如 FORMERR、REFUSED 等）
- 默认返回 SERVFAIL，可能掩盖真实错误

**代码位置**:
```go
// dnsserver/utils.go:96-115
func parseRcodeFromError(err error) int {
    // 仅处理 "rcode=X" 格式和 NXDOMAIN
    // 其他错误默认返回 SERVFAIL
    return dns.RcodeServerFailure  // 过于宽泛
}
```

**建议修复**:
- 完整实现所有 DNS 错误码的映射
- 区分网络错误和 DNS 错误
- 添加日志记录未知错误类型

---

## 4. DNSSEC 相关处理

### 4.1 【严重】DNSSEC 验证标志处理不当
**文件**: `dnsserver/handler_query.go:226-250`, `cache/cache_dnssec.go`
**RFC**: RFC 4035 (DNSSEC Protocol)

**问题**:
- 代码存储完整的 DNSSEC 消息，但过滤掉 DNSKEY 和 DS 记录
- 这会导致客户端无法进行 DNSSEC 验证
- AD 标志的转发逻辑不清晰

**代码位置**:
```go
// cache/cache_dnssec.go:95-99
func filterRecords(rrs []dns.RR) []dns.RR {
    var filtered []dns.RR
    for _, rr := range rrs {
        if rr.Header().Rrtype != dns.TypeDNSKEY && rr.Header().Rrtype != dns.TypeDS {
            filtered = append(filtered, rr)  // 过滤掉 DNSSEC 记录
        }
    }
}
```

**建议修复**:
- 保留 DNSSEC 记录用于验证
- 仅在必要时过滤（如内存限制）
- 正确转发 AD 标志

---

### 4.2 【中等】DO 标志处理不完整
**文件**: `dnsserver/handler_query.go:226`, `upstream/manager_*.go`
**RFC**: RFC 3225 (DNSSEC Lookaside Validation)

**问题**:
- 检查 DO 标志但未验证 DNSSEC 是否真正启用
- 未处理 DO 标志与 DNSSEC 禁用的冲突
- 未在响应中正确设置 DO 标志

**建议修复**:
- 验证 DNSSEC 配置与 DO 标志的一致性
- 在响应中正确设置 DO 标志
- 添加日志记录 DNSSEC 相关的决策

---

### 4.3 【中等】RRSIG 和 NSEC 记录处理缺失
**文件**: `upstream/manager_utils.go`, `dnsserver/handler_response.go`
**RFC**: RFC 4034, RFC 4035

**问题**:
- 代码未特殊处理 RRSIG 和 NSEC 记录
- 这些记录对 DNSSEC 验证至关重要
- 可能导致客户端无法验证签名

**建议修复**:
- 保留 RRSIG 和 NSEC 记录
- 实现 RRSIG 过期检查
- 正确处理 NSEC 记录用于否定证明

---

## 5. 记录去重和处理

### 5.1 【严重】IP 去重逻辑不完整
**文件**: `dnsserver/handler_response.go:40-75`, `upstream/manager_parallel.go:265-290`
**RFC**: RFC 1035

**问题**:
- IP 去重仅基于字符串比较，未考虑 IPv4 和 IPv6 的规范化
- 例如 "::1" 和 "0:0:0:0:0:0:0:1" 被视为不同的 IP
- 未处理 IPv4-mapped IPv6 地址（如 "::ffff:192.0.2.1"）

**代码位置**:
```go
// dnsserver/handler_response.go:40-75
ipSet := make(map[string]bool)
for _, ip := range ips {
    parsedIP := net.ParseIP(ip)
    ipStr := parsedIP.String()  // 依赖 String() 的规范化
    if ipSet[ipStr] {
        continue
    }
    ipSet[ipStr] = true
}
```

**建议修复**:
- 使用 `net.IP.Equal()` 进行 IP 比较
- 规范化 IPv6 地址
- 处理 IPv4-mapped IPv6 地址

---

### 5.2 【中等】CNAME 去重不规范
**文件**: `dnsserver/handler_response.go:217-240`, `dnsserver/handler_cname.go:50-56`
**RFC**: RFC 1035

**问题**:
- CNAME 去重基于字符串比较，未规范化域名（如大小写、尾部点）
- 可能导致重复的 CNAME 记录
- 未检测 CNAME 循环

**代码位置**:
```go
// dnsserver/handler_cname.go:50-56
cnameSet := make(map[string]bool)
for _, cname := range result.CNAMEs {
    if !cnameSet[cname] {  // 字符串比较，未规范化
        cnameSet[cname] = true
        accumulatedCNAMEs = append(accumulatedCNAMEs, cname)
    }
}
```

**建议修复**:
- 规范化域名（转小写、移除尾部点）
- 实现 CNAME 循环检测
- 限制 CNAME 链长度

---

### 5.3 【中等】记录去重键生成不完整
**文件**: `dnsserver/handler_response.go:294-320`
**RFC**: RFC 1035

**问题**:
- 去重键仅考虑记录内容，未考虑 TTL
- 但 RFC 允许相同内容不同 TTL 的记录
- 可能导致 TTL 信息丢失

**代码位置**:
```go
// dnsserver/handler_response.go:294-320
key := ""
switch r := rr.(type) {
case *dns.A:
    key = fmt.Sprintf("A:%s:%s", header.Name, r.A.String())
    // 未包含 TTL，可能导致 TTL 丢失
}
```

**建议修复**:
- 明确定义去重策略
- 文档化 TTL 处理规则
- 考虑保留不同 TTL 的记录

---

## 6. 响应构建逻辑

### 6.1 【严重】CNAME 链构建不规范
**文件**: `dnsserver/handler_response.go:195-240`
**RFC**: RFC 1035

**问题**:
- CNAME 链中的记录名称处理不当
- 未验证 CNAME 目标是否为 FQDN
- 可能导致无效的 CNAME 链

**代码位置**:
```go
// dnsserver/handler_response.go:217-240
currentName := fqdn
for _, target := range cnames {
    targetFqdn := dns.Fqdn(target)
    msg.Answer = append(msg.Answer, &dns.CNAME{
        Hdr: dns.RR_Header{
            Name:   currentName,
            Rrtype: dns.TypeCNAME,
            Class:  dns.ClassINET,
            Ttl:    ttl,
        },
        Target: targetFqdn,
    })
    currentName = targetFqdn
}
```

**问题分析**:
- 所有 CNAME 记录使用相同的 TTL，但上游可能返回不同的 TTL
- 未验证 CNAME 目标的有效性

**建议修复**:
- 为每个 CNAME 记录使用正确的 TTL
- 验证 CNAME 目标的有效性
- 实现 CNAME 链长度限制

---

### 6.2 【中等】Answer Section 顺序不确定
**文件**: `dnsserver/handler_response.go`, `dnsserver/handler_cache.go`
**RFC**: RFC 1035

**问题**:
- 代码使用 map 进行去重，导致记录顺序不确定
- RFC 建议保持一致的记录顺序
- 可能导致客户端缓存不一致

**建议修复**:
- 使用有序数据结构（如 slice）
- 实现确定性的记录排序
- 文档化排序规则

---

### 6.3 【中等】Authority Section 处理不完整
**文件**: `dnsserver/handler_response.go`, `dnsserver/handler_cache.go`
**RFC**: RFC 1035

**问题**:
- 仅在错误响应中添加 SOA 记录
- 未处理 Authority Section 中的其他记录（如 NS）
- 可能导致客户端无法进行递归查询

**建议修复**:
- 正确处理 Authority Section
- 添加 NS 记录用于递归查询
- 实现 Authority Section 的完整支持

---

## 7. 上游查询处理

### 7.1 【严重】并行查询中的 TTL 选择不当
**文件**: `upstream/manager_parallel.go:240-258`
**RFC**: RFC 1035

**问题**:
- 选择最小的 TTL 作为合并结果的 TTL
- 这可能导致 TTL 过短，频繁查询
- 未考虑 TTL 的语义（应为最保守的值）

**代码位置**:
```go
// upstream/manager_parallel.go:240-258
minTTL := fastResponse.TTL
for _, result := range allSuccessResults {
    if result.TTL < minTTL {
        minTTL = result.TTL  // 选择最小值
    }
}
```

**建议修复**:
- 使用最小 TTL 是正确的（最保守的策略）
- 但应添加配置选项允许用户调整
- 添加日志记录 TTL 选择的原因

---

### 7.2 【中等】NXDOMAIN 处理不一致
**文件**: `upstream/manager_sequential.go:101-110`, `upstream/manager_random.go:89-100`
**RFC**: RFC 1035

**问题**:
- 不同的查询策略对 NXDOMAIN 的处理不一致
- Sequential 和 Random 策略直接返回，但 Parallel 继续查询
- 可能导致不同的结果

**建议修复**:
- 统一 NXDOMAIN 处理逻辑
- 在所有策略中立即返回 NXDOMAIN
- 添加单元测试验证一致性

---

### 7.3 【中等】记录合并去重不完整
**文件**: `upstream/manager_parallel.go:265-290`
**RFC**: RFC 1035

**问题**:
- 合并多个上游响应时，仅基于 IP 地址和 CNAME 目标去重
- 未处理其他记录类型（如 MX、SRV 等）
- 可能导致重复的非 A/AAAA 记录

**代码位置**:
```go
// upstream/manager_parallel.go:265-290
func (u *Manager) mergeAndDeduplicateRecords(results []*QueryResult) []dns.RR {
    // 仅处理 A、AAAA、CNAME
    // 其他记录使用 String() 比较，可能不完整
}
```

**建议修复**:
- 为每种记录类型实现专门的去重逻辑
- 考虑记录的所有字段（不仅是主要字段）
- 添加配置选项控制去重行为

---

## 8. 特殊域名处理

### 8.1 【中等】本地域名处理不规范
**文件**: `dnsserver/handler_custom.go:81-140`
**RFC**: RFC 6762 (mDNS), RFC 6763 (DNS-SD)

**问题**:
- 硬编码的本地域名列表不完整
- 未处理 ".local" 域名的特殊语义
- 未实现 mDNS 支持

**代码位置**:
```go
// dnsserver/handler_custom.go:120-132
blockedDomains := map[string]int{
    "local":                     dns.RcodeRefused,
    "corp":                      dns.RcodeRefused,
    // ... 其他硬编码域名
}
```

**建议修复**:
- 实现完整的本地域名处理
- 支持 mDNS 查询
- 添加配置选项控制本地域名行为

---

### 8.2 【中等】反向 DNS 查询处理不完整
**文件**: `dnsserver/handler_custom.go:107-112`
**RFC**: RFC 1035

**问题**:
- 拒绝所有反向 DNS 查询
- 但某些反向查询可能是合法的
- 未实现反向 DNS 解析

**代码位置**:
```go
// dnsserver/handler_custom.go:107-112
if strings.HasSuffix(domain, ".in-addr.arpa") || strings.HasSuffix(domain, ".ip6.arpa") {
    logger.Debugf("[QueryFilter] REFUSED: reverse DNS query for '%s'", domain)
    msg.SetRcode(r, dns.RcodeRefused)
}
```

**建议修复**:
- 实现反向 DNS 解析
- 或提供配置选项控制反向查询行为
- 添加日志记录反向查询的原因

---

### 8.3 【低】单标签域名处理
**文件**: `dnsserver/handler_custom.go:81-86`
**RFC**: RFC 1035

**问题**:
- 拒绝所有单标签域名
- 但某些单标签域名可能是合法的（如 "localhost"）
- 未区分不同的单标签域名

**建议修复**:
- 实现白名单机制
- 允许配置单标签域名处理
- 添加日志记录被拒绝的单标签域名

---

## 总结和优先级

### 优先级 1（严重，需立即修复）
1. TTL 计算精度问题
2. 负缓存 TTL 处理不规范
3. NXDOMAIN 和 NODATA 混淆
4. DNSSEC 验证标志处理不当
5. IP 去重逻辑不完整
6. CNAME 链构建不规范
7. EDNS0 OPT 记录处理不完整

### 优先级 2（中等，应尽快修复）
1. Question Section 验证不足
2. 缓存过期检查不一致
3. SERVFAIL 响应缺少 SOA 记录
4. 错误响应码提取不完整
5. CNAME 去重不规范
6. 并行查询中的 TTL 选择
7. NXDOMAIN 处理不一致
8. 记录合并去重不完整

### 优先级 3（低，可逐步改进）
1. Message ID 和 Flags 处理
2. UserReturnTTL 循环逻辑问题
3. DO 标志处理不完整
4. RRSIG 和 NSEC 记录处理缺失
5. 记录去重键生成不完整
6. Answer Section 顺序不确定
7. Authority Section 处理不完整
8. 本地域名处理不规范
9. 反向 DNS 查询处理不完整
10. 单标签域名处理

---

## 建议的修复步骤

1. **第一阶段**：修复所有优先级 1 的问题
2. **第二阶段**：修复所有优先级 2 的问题
3. **第三阶段**：改进优先级 3 的问题
4. **持续改进**：添加单元测试和集成测试验证 RFC 合规性

