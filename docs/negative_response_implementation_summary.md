# 负响应 SOA 记录实现 - 完整修复总结

## 修复日期
2025-12-20

## 问题描述
原项目对于负响应（NXDOMAIN、NODATA、SERVFAIL等）没有添加 SOA 记录，导致客户端无法知道应该缓存负响应多久，只能靠猜测。这不符合 RFC 2308 标准。

## 修复内容

### 1. 核心功能实现

#### 文件：`dnsserver/handler_response.go`
- ✅ 添加 `buildSOARecord()` 函数
- 功能：构造符合 RFC 2308 标准的 SOA 记录
- SOA 字段：
  - MNAME: `ns.smartdnssort.local.`
  - RNAME: `admin.smartdnssort.local.`
  - Serial: Unix 时间戳
  - Refresh: 3600 (1小时)
  - Retry: 600 (10分钟)
  - Expire: 86400 (1天)
  - Minimum: TTL（负缓存时间）

### 2. 负响应场景覆盖

#### 场景 1: NXDOMAIN（域名不存在）
**文件：** `dnsserver/handler_query.go`
- ✅ handleCacheMiss - 第78-88行
- ✅ handleGenericCacheMiss - 第436-442行
- **TTL：** `negative_ttl_seconds` (默认 300秒)

#### 场景 2: NODATA（域名存在但无此类型记录）
**文件：** `dnsserver/handler_query.go`
- ✅ handleCacheMiss - 第138-158行
- **TTL：** `negative_ttl_seconds` (默认 300秒)

#### 场景 3: SERVFAIL（上游查询失败）
**文件：** `dnsserver/handler_query.go`
- ✅ handleCacheMiss - 第88-98行
- ✅ handleGenericCacheMiss - 第443-449行
- ✅ CNAME 递归解析失败 - 第109-124行
- **TTL：** `error_cache_ttl_seconds` (默认 30秒)

#### 场景 4: AdBlock 拦截
**文件：** `dnsserver/utils.go` + `dnsserver/handler_adblock.go`
- ✅ NXDOMAIN 模式 - buildNXDomainResponse()
- ✅ REFUSED 模式 - buildRefuseResponse()
- **TTL：** `blocked_ttl` (默认 3600秒)

#### 场景 5: 本地规则拦截
**文件：** `dnsserver/handler_custom.go`
- ✅ 单标签域名 REFUSED - 第80-89行
- ✅ 反向 DNS 查询 REFUSED - 第103-112行
- ✅ 黑名单域名 REFUSED/NXDOMAIN - 第127-135行
- **TTL：** 300秒（硬编码）

#### 场景 6: 错误缓存命中
**文件：** `dnsserver/handler_cache.go`
- ✅ handleErrorCacheHit - 第14-44行
- **TTL：** 剩余 TTL（递减）

### 3. 细节优化

#### TTL 计算精度修复
**文件：** `dnsserver/handler_cache.go` (第28-29行)
- ❌ 修复前：`remainingTTL := uint32(max(1, entry.TTL-int(elapsed)))`
- ✅ 修复后：`elapsed := int(time.Since(entry.CachedAt).Seconds() + 0.5)` // 四舍五入
- **改进：** 避免浮点数截断导致的精度损失

#### Compress 标志添加
**文件：** `dnsserver/handler_cache.go` (第25行)
- ✅ 添加：`msg.Compress = false`
- **作用：** 与其他响应保持一致

## 配置说明

### 相关配置项

```yaml
cache:
  # NXDOMAIN/NODATA 的缓存 TTL（秒）
  negative_ttl_seconds: 300
  
  # SERVFAIL/REFUSED 等错误的缓存 TTL（秒）
  error_cache_ttl_seconds: 30

adblock:
  # AdBlock 拦截的 TTL（秒）
  blocked_ttl: 3600
  
  # 拦截模式：nxdomain, zero_ip, refuse
  block_mode: zero_ip
```

## 测试验证

### 测试命令

```bash
# 1. NXDOMAIN 测试
dig @127.0.0.1 nonexistent.test A +noall +authority

# 2. NODATA 测试
dig @127.0.0.1 google.com AAAA +noall +authority

# 3. 缓存 TTL 递减测试
dig @127.0.0.1 test-domain.invalid A +noall +authority
# 等待 5 秒
dig @127.0.0.1 test-domain.invalid A +noall +authority

# 4. 本地规则测试
dig @127.0.0.1 local A +noall +authority
dig @127.0.0.1 singlelabel A +noall +authority
```

### 预期结果

所有负响应都应该包含 SOA 记录：

```
;; AUTHORITY SECTION:
<domain>. <TTL> IN SOA ns.smartdnssort.local. admin.smartdnssort.local. <serial> 3600 600 86400 <TTL>
```

**TTL 值：**
- NXDOMAIN/NODATA: 300秒
- SERVFAIL: 30秒
- AdBlock: 3600秒
- 本地规则: 300秒

## RFC 标准符合性

### RFC 2308 - DNS 负缓存
✅ **Section 3**: 负响应必须在 Authority section 包含 SOA 记录
✅ **Section 4**: SOA 记录的 MINIMUM 字段指示负缓存 TTL
✅ **Section 5**: 客户端应使用 SOA 记录中的 TTL 来缓存负响应

### RFC 1035 - DNS 实现规范
✅ **Section 3.3.13**: SOA 记录格式正确
✅ **Section 4.1.3**: Authority section 正确使用

## 性能影响

- **内存增加：** 每个负响应增加约 100 字节（SOA 记录）
- **CPU 影响：** 可忽略（仅构造 SOA 记录）
- **网络流量：** 每个负响应增加约 100 字节

## 后续优化建议

### 1. 可配置的 SOA 字段
```yaml
dns:
  soa_mname: "ns.smartdnssort.local."
  soa_rname: "admin.smartdnssort.local."
  soa_refresh: 3600
  soa_retry: 600
  soa_expire: 86400
```

### 2. 区分不同负响应类型的 TTL
```yaml
cache:
  nxdomain_ttl_seconds: 3600    # 域名不存在
  nodata_ttl_seconds: 300       # 无此类型记录
  servfail_ttl_seconds: 30      # 服务器错误
```

### 3. 保留上游 SOA 记录
如果上游 DNS 返回了 SOA 记录，可以选择保留并只修改 TTL。

## 修改文件列表

1. ✅ `dnsserver/handler_response.go` - 添加 buildSOARecord()
2. ✅ `dnsserver/handler_cache.go` - 修改 handleErrorCacheHit()
3. ✅ `dnsserver/handler_query.go` - 修改 3 处负响应处理
4. ✅ `dnsserver/utils.go` - 修改 buildNXDomainResponse() 和 buildRefuseResponse()
5. ✅ `dnsserver/handler_adblock.go` - 更新所有调用
6. ✅ `dnsserver/handler_custom.go` - 添加 3 处 SOA 记录

## 测试脚本

- `test_negative_response.bat` - 基础测试
- `test_soa_complete.bat` - 完整测试

## 实现状态

🎉 **完成！** 所有负响应场景都已实现 SOA 记录支持。

---

**实现者：** Antigravity AI  
**审核者：** 用户  
**版本：** 1.0  
**状态：** ✅ 已完成
