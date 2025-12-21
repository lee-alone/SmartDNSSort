# 负响应类型TTL改写优化方案

## 问题分析

当前项目对于负响应（NXDOMAIN、NODATA等）的处理存在以下问题：

1. **缺少SOA记录**：返回负响应时没有在Authority section添加SOA记录
2. **客户端无法获知TTL**：客户端不知道应该缓存负响应多久，只能使用默认值或猜测
3. **配置已预留但未使用**：`negative_ttl_seconds` 配置项存在但未充分利用

## RFC 标准要求

根据 RFC 2308（DNS负缓存）：
- NXDOMAIN响应应在Authority section包含SOA记录
- SOA记录的MINIMUM字段指示负缓存的TTL
- 客户端应使用SOA记录中的TTL来缓存负响应

## 优化方案

### 方案一：添加SOA记录（推荐）

**优点**：
- 符合RFC标准
- 客户端可以正确缓存负响应
- 更好的DNS生态兼容性

**实现步骤**：

1. **创建SOA记录构造函数**
```go
// 在 dnsserver/handler_response.go 中添加
func (s *Server) buildSOARecord(domain string, ttl uint32) *dns.SOA {
    // 使用配置的权威服务器名称，或使用默认值
    mname := "ns.smartdnssort.local."
    rname := "admin.smartdnssort.local."
    
    return &dns.SOA{
        Hdr: dns.RR_Header{
            Name:   dns.Fqdn(domain),
            Rrtype: dns.TypeSOA,
            Class:  dns.ClassINET,
            Ttl:    ttl,
        },
        Ns:      mname,
        Mbox:    rname,
        Serial:  uint32(time.Now().Unix()),
        Refresh: 3600,
        Retry:   600,
        Expire:  86400,
        Minttl:  ttl, // 这个字段指示负缓存TTL
    }
}
```

2. **修改负响应处理函数**

修改 `handleErrorCacheHit` 函数：
```go
func (s *Server) handleErrorCacheHit(w dns.ResponseWriter, r *dns.Msg, domain string, qtype uint16, stats *stats.Stats) bool {
    if entry, ok := s.cache.GetError(domain, qtype); ok {
        stats.IncCacheHits()
        logger.Debugf("[handleQuery] 错误缓存命中: %s (type=%s, rcode=%d)",
            domain, dns.TypeToString[qtype], entry.Rcode)

        msg := s.msgPool.Get()
        msg.SetReply(r)
        msg.RecursionAvailable = true
        msg.SetRcode(r, entry.Rcode)
        
        // 计算剩余TTL
        elapsed := time.Since(entry.CachedAt).Seconds()
        remainingTTL := uint32(math.Max(1, float64(entry.TTL)-elapsed))
        
        // 添加SOA记录到Authority section
        soa := s.buildSOARecord(domain, remainingTTL)
        msg.Ns = append(msg.Ns, soa)
        
        w.WriteMsg(msg)
        s.msgPool.Put(msg)
        return true
    }
    return false
}
```

3. **修改首次查询时的负响应处理**

在 `handler_query.go` 的两处错误处理（第78-88行和第412-419行）：
```go
if originalRcode == dns.RcodeNameError {
    s.cache.SetError(domain, qtype, originalRcode, currentCfg.Cache.NegativeTTLSeconds)
    logger.Debugf("[handleQuery] NXDOMAIN 错误，缓存并返回: %s", domain)
    msg.SetRcode(r, dns.RcodeNameError)
    
    // 添加SOA记录
    soa := s.buildSOARecord(domain, uint32(currentCfg.Cache.NegativeTTLSeconds))
    msg.Ns = append(msg.Ns, soa)
    
    w.WriteMsg(msg)
}
```

### 方案二：改写上游响应中的SOA记录

如果上游DNS已经返回了SOA记录，可以改写其TTL：

**优点**：
- 保留上游的SOA记录信息
- 只需修改TTL字段

**实现**：
```go
// 在处理上游响应时
if result.DnsMsg != nil && result.DnsMsg.Rcode == dns.RcodeNameError {
    // 查找并修改SOA记录
    for _, rr := range result.DnsMsg.Ns {
        if soa, ok := rr.(*dns.SOA); ok {
            soa.Hdr.Ttl = uint32(currentCfg.Cache.NegativeTTLSeconds)
            soa.Minttl = uint32(currentCfg.Cache.NegativeTTLSeconds)
        }
    }
}
```

### 方案三：扩展ErrorCacheEntry存储完整响应

**优点**：
- 可以保留上游的完整响应（包括SOA等记录）
- 支持更复杂的负响应场景

**实现**：
```go
// 修改 cache/entries.go
type ErrorCacheEntry struct {
    Rcode       int          // DNS 错误码
    CachedAt    time.Time    // 缓存时间
    TTL         int          // 缓存 TTL（秒）
    AuthSection []dns.RR     // 新增：Authority section（包含SOA等）
    ExtraSection []dns.RR    // 新增：Additional section
}
```

## 配置优化建议

### 1. 区分不同类型的负响应TTL

```yaml
cache:
  # NXDOMAIN (域名不存在) 的缓存TTL
  nxdomain_ttl_seconds: 3600
  
  # NODATA (域名存在但无此类型记录) 的缓存TTL  
  nodata_ttl_seconds: 300
  
  # SERVFAIL/REFUSED 等错误的缓存TTL
  error_cache_ttl_seconds: 30
```

### 2. 添加SOA配置

```yaml
dns:
  # SOA记录配置（用于负响应）
  soa_mname: "ns.smartdnssort.local."
  soa_rname: "admin.smartdnssort.local."
  soa_refresh: 3600
  soa_retry: 600
  soa_expire: 86400
```

## 实现优先级

1. **高优先级**：方案一 - 添加SOA记录
   - 最符合标准
   - 实现相对简单
   - 立即改善客户端体验

2. **中优先级**：配置优化 - 区分不同负响应类型
   - 提供更精细的控制
   - 符合实际使用场景

3. **低优先级**：方案三 - 存储完整响应
   - 更完整但更复杂
   - 可以作为后续优化

## 测试建议

1. **功能测试**：
   - 查询不存在的域名，检查响应中是否有SOA记录
   - 验证SOA记录的TTL是否正确
   - 测试缓存命中时SOA的TTL是否递减

2. **兼容性测试**：
   - 使用dig工具验证响应格式
   - 测试各种DNS客户端（Windows、Linux、macOS）
   - 验证递归DNS服务器的缓存行为

3. **性能测试**：
   - 验证添加SOA记录不会显著影响性能
   - 测试高并发负响应场景

## 参考资料

- RFC 2308: Negative Caching of DNS Queries (DNS NCACHE)
- RFC 1035: Domain Names - Implementation and Specification
- RFC 2136: Dynamic Updates in the Domain Name System
