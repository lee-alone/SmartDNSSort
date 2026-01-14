# DNS RFC 违规点 - 代码示例和修复方案

## 1. TTL 计算精度问题

### 当前代码（有问题）
```go
// dnsserver/sorting.go:82-103
func (s *Server) calculateRemainingTTL(upstreamTTL uint32, acquisitionTime time.Time) int {
    elapsed := time.Since(acquisitionTime).Seconds()  // 浮点数，精度丢失
    remaining := int(upstreamTTL) - int(elapsed)      // 转换为整数时丢失精度
    
    minTTL := s.cfg.Cache.MinTTLSeconds
    maxTTL := s.cfg.Cache.MaxTTLSeconds
    
    if minTTL == 0 && maxTTL == 0 {
        return remaining
    }
    
    if minTTL > 0 && remaining < minTTL {
        remaining = minTTL
    }
    
    if maxTTL > 0 && remaining > maxTTL {
        remaining = maxTTL
    }
    
    return remaining
}
```

### 问题分析
1. `time.Since().Seconds()` 返回浮点数，精度可能丢失
2. 当 TTL 过期时，remaining 可能为负数
3. 代码使用 `max(1, ...)` 作为兜底，但这违反了 RFC

### 修复方案
```go
func (s *Server) calculateRemainingTTL(upstreamTTL uint32, acquisitionTime time.Time) int {
    // 使用整数毫秒计算，避免浮点精度问题
    elapsedMs := time.Since(acquisitionTime).Milliseconds()
    elapsedSec := int(elapsedMs / 1000)
    
    remaining := int(upstreamTTL) - elapsedSec
    
    // 如果 TTL 已过期，返回 0 而不是强制设为 1
    if remaining <= 0 {
        return 0  // 调用方应检查并重新查询
    }
    
    minTTL := s.cfg.Cache.MinTTLSeconds
    maxTTL := s.cfg.Cache.MaxTTLSeconds
    
    if minTTL == 0 && maxTTL == 0 {
        return remaining
    }
    
    if minTTL > 0 && remaining < minTTL {
        remaining = minTTL
    }
    
    if maxTTL > 0 && remaining > maxTTL {
        remaining = maxTTL
    }
    
    return remaining
}
```

---

## 2. NXDOMAIN 和 NODATA 混淆

### 当前代码（有问题）
```go
// dnsserver/handler_query.go:159-174
if len(finalIPs) == 0 && len(fullCNAMEs) == 0 {
    logger.Debugf("[handleQuery] 上游查询返回空结果 (NODATA): %s", domain)
    
    // 缓存 NODATA 响应（使用 negative_ttl_seconds）
    s.cache.SetError(domain, qtype, dns.RcodeSuccess, currentCfg.Cache.NegativeTTLSeconds)
    
    msg := s.msgPool.Get()
    msg.SetReply(r)
    msg.RecursionAvailable = true
    msg.Compress = false
    msg.SetRcode(r, dns.RcodeSuccess)  // 错误：NODATA 应为 NOERROR
    msg.Answer = nil
    
    // 添加 SOA 记录到 Authority section（符合 RFC 2308）
    soa := s.buildSOARecord(domain, uint32(currentCfg.Cache.NegativeTTLSeconds))
    msg.Ns = append(msg.Ns, soa)
    
    w.WriteMsg(msg)
    s.msgPool.Put(msg)
    return
}
```

### 问题分析
1. NODATA 和 NXDOMAIN 应使用不同的 Rcode
   - NXDOMAIN: Rcode = 3 (NXDOMAIN)
   - NODATA: Rcode = 0 (NOERROR)
2. 两者的缓存策略应不同
3. 代码无法区分这两种情况

### 修复方案
```go
// 首先需要在上游查询中区分 NXDOMAIN 和 NODATA
// upstream/manager_utils.go
func extractRecordsWithStatus(msg *dns.Msg) ([]dns.RR, []string, uint32, bool) {
    // 返回值：records, cnames, ttl, isNXDomain
    
    // 检查 Rcode
    if msg.Rcode == dns.RcodeNameError {
        return nil, nil, extractNegativeTTL(msg), true  // NXDOMAIN
    }
    
    // 如果 Rcode 为 Success 但没有 Answer，则为 NODATA
    if msg.Rcode == dns.RcodeSuccess && len(msg.Answer) == 0 {
        return nil, nil, extractNegativeTTL(msg), false  // NODATA
    }
    
    // 正常情况
    records, cnames, ttl := extractRecords(msg)
    return records, cnames, ttl, false
}

// dnsserver/handler_query.go
if len(finalIPs) == 0 && len(fullCNAMEs) == 0 {
    // 需要从上游响应中获取原始 Rcode
    if result.DnsMsg != nil && result.DnsMsg.Rcode == dns.RcodeNameError {
        // NXDOMAIN 情况
        logger.Debugf("[handleQuery] 上游查询返回 NXDOMAIN: %s", domain)
        s.cache.SetError(domain, qtype, dns.RcodeNameError, currentCfg.Cache.ErrorCacheTTL)
        
        msg := s.msgPool.Get()
        msg.SetReply(r)
        msg.RecursionAvailable = true
        msg.Compress = false
        msg.SetRcode(r, dns.RcodeNameError)  // 正确的 Rcode
        
        soa := s.buildSOARecord(domain, uint32(currentCfg.Cache.ErrorCacheTTL))
        msg.Ns = append(msg.Ns, soa)
        
        w.WriteMsg(msg)
        s.msgPool.Put(msg)
    } else {
        // NODATA 情况
        logger.Debugf("[handleQuery] 上游查询返回 NODATA: %s", domain)
        s.cache.SetError(domain, qtype, dns.RcodeSuccess, currentCfg.Cache.NegativeTTLSeconds)
        
        msg := s.msgPool.Get()
        msg.SetReply(r)
        msg.RecursionAvailable = true
        msg.Compress = false
        msg.SetRcode(r, dns.RcodeSuccess)  // NODATA 使用 NOERROR
        msg.Answer = nil
        
        soa := s.buildSOARecord(domain, uint32(currentCfg.Cache.NegativeTTLSeconds))
        msg.Ns = append(msg.Ns, soa)
        
        w.WriteMsg(msg)
        s.msgPool.Put(msg)
    }
    return
}
```

---

## 3. IP 去重不完整

### 当前代码（有问题）
```go
// dnsserver/handler_response.go:40-75
ipSet := make(map[string]bool)
for _, ip := range ips {
    parsedIP := net.ParseIP(ip)
    if parsedIP == nil {
        continue
    }
    
    // 对IP进行去重
    ipStr := parsedIP.String()  // 依赖 String() 的规范化
    if ipSet[ipStr] {
        continue  // 跳过重复的IP
    }
    ipSet[ipStr] = true
    
    // ... 添加记录
}
```

### 问题分析
1. IPv6 地址有多种表示方式：
   - "::1" 和 "0:0:0:0:0:0:0:1" 被视为不同
   - "::ffff:192.0.2.1" (IPv4-mapped IPv6) 处理不当
2. String() 方法的规范化可能不完整

### 修复方案
```go
// 使用 net.IP.Equal() 进行比较
ipSet := make(map[string]bool)
var uniqueIPs []net.IP

for _, ip := range ips {
    parsedIP := net.ParseIP(ip)
    if parsedIP == nil {
        continue
    }
    
    // 规范化 IPv6 地址
    normalizedIP := parsedIP.To16()
    if normalizedIP == nil {
        normalizedIP = parsedIP.To4()
    }
    
    // 使用规范化后的字符串作为去重键
    ipStr := normalizedIP.String()
    if ipSet[ipStr] {
        continue
    }
    ipSet[ipStr] = true
    uniqueIPs = append(uniqueIPs, parsedIP)
}

// 添加记录时使用 uniqueIPs
for _, ip := range uniqueIPs {
    // ... 添加 A/AAAA 记录
}
```

---

## 4. CNAME 链构建不规范

### 当前代码（有问题）
```go
// dnsserver/handler_response.go:195-240
func (s *Server) buildDNSResponseWithCNAME(msg *dns.Msg, domain string, cnames []string, ips []string, qtype uint16, ttl uint32) {
    if len(cnames) == 0 {
        return
    }
    
    currentName := dns.Fqdn(domain)
    
    // 第一步：添加 CNAME 链（去重）
    cnameSet := make(map[string]bool)
    for _, target := range cnames {
        targetFqdn := dns.Fqdn(target)
        
        cnamePair := currentName + "->" + targetFqdn
        if cnameSet[cnamePair] {
            continue
        }
        cnameSet[cnamePair] = true
        
        msg.Answer = append(msg.Answer, &dns.CNAME{
            Hdr: dns.RR_Header{
                Name:   currentName,
                Rrtype: dns.TypeCNAME,
                Class:  dns.ClassINET,
                Ttl:    ttl,  // 所有 CNAME 使用相同 TTL
            },
            Target: targetFqdn,
        })
        currentName = targetFqdn
    }
    
    // ... 添加 A/AAAA 记录
}
```

### 问题分析
1. 所有 CNAME 记录使用相同的 TTL，但上游可能返回不同的 TTL
2. 未验证 CNAME 目标的有效性
3. 未检测 CNAME 循环

### 修复方案
```go
// 需要在上游查询中保留每个 CNAME 的 TTL
type CNAMERecord struct {
    Target string
    TTL    uint32
}

func (s *Server) buildDNSResponseWithCNAME(msg *dns.Msg, domain string, cnames []CNAMERecord, ips []string, qtype uint16) {
    if len(cnames) == 0 {
        return
    }
    
    currentName := dns.Fqdn(domain)
    seenCNAMEs := make(map[string]bool)
    
    // 第一步：添加 CNAME 链（检测循环）
    for _, cnameRec := range cnames {
        targetFqdn := dns.Fqdn(cnameRec.Target)
        
        // 检测 CNAME 循环
        if seenCNAMEs[targetFqdn] {
            logger.Warnf("[buildDNSResponseWithCNAME] CNAME 循环检测: %s -> %s", currentName, targetFqdn)
            break  // 停止添加更多 CNAME
        }
        seenCNAMEs[targetFqdn] = true
        
        // 验证 CNAME 目标的有效性
        if !isValidDomain(cnameRec.Target) {
            logger.Warnf("[buildDNSResponseWithCNAME] 无效的 CNAME 目标: %s", cnameRec.Target)
            continue
        }
        
        msg.Answer = append(msg.Answer, &dns.CNAME{
            Hdr: dns.RR_Header{
                Name:   currentName,
                Rrtype: dns.TypeCNAME,
                Class:  dns.ClassINET,
                Ttl:    cnameRec.TTL,  // 使用正确的 TTL
            },
            Target: targetFqdn,
        })
        currentName = targetFqdn
    }
    
    // ... 添加 A/AAAA 记录
}

func isValidDomain(domain string) bool {
    // 验证域名格式
    domain = strings.TrimRight(domain, ".")
    if len(domain) == 0 || len(domain) > 253 {
        return false
    }
    
    labels := strings.Split(domain, ".")
    for _, label := range labels {
        if len(label) == 0 || len(label) > 63 {
            return false
        }
        // 检查标签中的字符
        for _, ch := range label {
            if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || 
                 (ch >= '0' && ch <= '9') || ch == '-') {
                return false
            }
        }
    }
    return true
}
```

---

## 5. EDNS0 OPT 记录处理不完整

### 当前代码（有问题）
```go
// upstream/manager_parallel.go:60-62
msg := new(dns.Msg)
msg.SetQuestion(dns.Fqdn(domain), qtype)
if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
    msg.SetEdns0(4096, true)  // 硬编码 4096
}
```

### 问题分析
1. 硬编码 UDP 缓冲区大小为 4096，未从客户端请求中提取
2. 未处理客户端请求中的其他 EDNS0 选项
3. 未验证响应中的 OPT 记录

### 修复方案
```go
// 从客户端请求中提取 EDNS0 信息
func extractEDNS0Info(r *dns.Msg) (uint16, bool, []dns.EDNS0) {
    opt := r.IsEdns0()
    if opt == nil {
        return 512, false, nil  // 默认 UDP 缓冲区大小
    }
    
    udpSize := opt.UDPSize()
    if udpSize < 512 {
        udpSize = 512
    }
    if udpSize > 65535 {
        udpSize = 65535
    }
    
    do := opt.Do()
    options := opt.Option
    
    return udpSize, do, options
}

// 在上游查询中使用
udpSize, do, options := extractEDNS0Info(r)

msg := new(dns.Msg)
msg.SetQuestion(dns.Fqdn(domain), qtype)

if dnssec && do {
    msg.SetEdns0(udpSize, true)
    
    // 转发客户端的 EDNS0 选项
    opt := msg.IsEdns0()
    if opt != nil {
        for _, option := range options {
            opt.Option = append(opt.Option, option)
        }
    }
}
```

---

## 6. 负缓存 TTL 处理不规范

### 当前代码（有问题）
```go
// upstream/manager_utils.go:100-115
func extractNegativeTTL(msg *dns.Msg) uint32 {
    for _, ns := range msg.Ns {
        if soa, ok := ns.(*dns.SOA); ok {
            ttl := soa.Hdr.Ttl
            minttl := min(soa.Minttl, ttl)
            return minttl  // 未完全遵循 RFC 2308
        }
    }
    return 300  // 硬编码默认值
}
```

### 问题分析
1. RFC 2308 规定负缓存 TTL 应为 SOA 记录的 Minimum 字段
2. 但也应考虑 SOA 记录本身的 TTL
3. 未区分 NXDOMAIN 和 NODATA

### 修复方案
```go
// RFC 2308 Section 5
func extractNegativeTTL(msg *dns.Msg, isNXDomain bool) uint32 {
    var soa *dns.SOA
    
    // 从 Authority section 查找 SOA 记录
    for _, ns := range msg.Ns {
        if s, ok := ns.(*dns.SOA); ok {
            soa = s
            break
        }
    }
    
    if soa == nil {
        // 如果没有 SOA 记录，使用默认值
        if isNXDomain {
            return 300  // NXDOMAIN 默认 5 分钟
        } else {
            return 300  // NODATA 默认 5 分钟
        }
    }
    
    // RFC 2308: 负缓存 TTL = min(SOA.Minimum, SOA.TTL)
    negativeTTL := soa.Minttl
    if soa.Hdr.Ttl < negativeTTL {
        negativeTTL = soa.Hdr.Ttl
    }
    
    // 应用最小和最大限制
    if negativeTTL < 1 {
        negativeTTL = 1
    }
    if negativeTTL > 86400 {  // 最多 1 天
        negativeTTL = 86400
    }
    
    return negativeTTL
}
```

---

## 7. DNSSEC 验证标志处理

### 当前代码（有问题）
```go
// cache/cache_dnssec.go:95-99
func filterRecords(rrs []dns.RR) []dns.RR {
    var filtered []dns.RR
    for _, rr := range rrs {
        if rr.Header().Rrtype != dns.TypeDNSKEY && rr.Header().Rrtype != dns.TypeDS {
            filtered = append(filtered, rr)  // 过滤掉 DNSSEC 记录
        }
    }
    return filtered
}
```

### 问题分析
1. 过滤掉 DNSKEY 和 DS 记录会导致客户端无法进行 DNSSEC 验证
2. 这违反了 RFC 4035 的要求
3. 应该保留这些记录用于验证

### 修复方案
```go
// 不过滤 DNSSEC 记录，而是实现内存管理
func (c *Cache) SetDNSSECMsg(domain string, qtype uint16, msg *dns.Msg) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    key := cacheKey(domain, qtype)
    
    // 创建消息的副本，不过滤任何记录
    cachedMsg := msg.Copy()
    
    // 获取消息中所有记录的最小 TTL
    minMsgTTL := getMinTTL(cachedMsg)
    
    // 结合配置的 DNSSEC 消息缓存 TTL
    effectiveTTL := minMsgTTL
    if c.config.DNSSECMsgCacheTTLSeconds > 0 {
        effectiveTTL = uint32(min(int(minMsgTTL), c.config.DNSSECMsgCacheTTLSeconds))
    }
    
    entry := &DNSSECCacheEntry{
        Message:         cachedMsg,  // 保留完整消息
        AcquisitionTime: timeNow(),
        TTL:             effectiveTTL,
    }
    c.msgCache.Set(key, entry)
}
```

---

## 8. Question Section 验证

### 当前代码（有问题）
```go
// upstream/manager.go:118-119
if len(r.Question) == 0 {
    return nil, errors.New("query message has no questions")
}
question := r.Question[0]  // 仅处理第一个
```

### 问题分析
1. 仅检查 Question 是否为空
2. 未验证 Question 数量（应为 1）
3. 未验证 QCLASS 和 QTYPE 的有效性

### 修复方案
```go
func (u *Manager) Query(ctx context.Context, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
    // 验证 Question section
    if len(r.Question) == 0 {
        return nil, errors.New("query message has no questions")
    }
    
    if len(r.Question) > 1 {
        logger.Warnf("[Query] 收到多个 Question，仅处理第一个")
    }
    
    question := r.Question[0]
    
    // 验证 QCLASS
    if question.Qclass != dns.ClassINET {
        return nil, fmt.Errorf("unsupported QCLASS: %d", question.Qclass)
    }
    
    // 验证 QTYPE
    if !isValidQType(question.Qtype) {
        return nil, fmt.Errorf("unsupported QTYPE: %d", question.Qtype)
    }
    
    // ... 继续处理
}

func isValidQType(qtype uint16) bool {
    // 支持的查询类型
    supportedTypes := map[uint16]bool{
        dns.TypeA:     true,
        dns.TypeAAAA:  true,
        dns.TypeCNAME: true,
        dns.TypeMX:    true,
        dns.TypeNS:    true,
        dns.TypeSOA:   true,
        dns.TypeTXT:   true,
        dns.TypeSRV:   true,
        dns.TypePTR:   true,
        dns.TypeANY:   true,
    }
    return supportedTypes[qtype]
}
```

