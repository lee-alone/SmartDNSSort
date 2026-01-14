# DNS RFC 违规点文件映射表

## 快速查找表

| 违规点 | 严重程度 | 文件 | 行号 | RFC | 描述 |
|------|--------|------|------|-----|------|
| EDNS0 OPT 处理不完整 | 严重 | upstream/manager_*.go | 60-62 | RFC 6891 | 硬编码 4096，未处理 OPT 扩展 |
| Question 验证不足 | 中等 | upstream/manager.go | 118-119 | RFC 1035 | 仅检查是否为空，未验证数量 |
| Message ID/Flags | 中等 | dnsserver/handler_response.go | - | RFC 1035 | 未验证 RD、AA、CD 标志 |
| TTL 计算精度问题 | 严重 | dnsserver/sorting.go | 82-103 | RFC 1035 | 浮点精度丢失，可能为负 |
| 负缓存 TTL 不规范 | 严重 | dnsserver/handler_cache.go | 14-30 | RFC 2308 | 未区分 NXDOMAIN/NODATA |
| 缓存过期检查不一致 | 中等 | cache/cache_raw.go | 8-15 | RFC 1035 | GetRaw 不检查过期 |
| UserReturnTTL 逻辑 | 中等 | dnsserver/handler_cache.go | 50-65 | RFC 1035 | 循环逻辑复杂易出错 |
| NXDOMAIN/NODATA 混淆 | 严重 | dnsserver/handler_query.go | 159-174 | RFC 2308 | 混为一谈，缓存策略错误 |
| SERVFAIL 缓存 | 中等 | dnsserver/handler_query.go | 89-96 | RFC 2308 | 不应缓存 SERVFAIL |
| 错误码提取不完整 | 中等 | dnsserver/utils.go | 96-115 | RFC 1035 | 仅处理特定格式 |
| DNSSEC 验证标志 | 严重 | dnsserver/handler_query.go | 226-250 | RFC 4035 | 过滤 DNSKEY/DS 记录 |
| DO 标志处理 | 中等 | dnsserver/handler_query.go | 226 | RFC 3225 | 未验证 DNSSEC 启用状态 |
| RRSIG/NSEC 处理 | 中等 | upstream/manager_utils.go | - | RFC 4034 | 未特殊处理 DNSSEC 记录 |
| IP 去重不完整 | 严重 | dnsserver/handler_response.go | 40-75 | RFC 1035 | 未规范化 IPv6 地址 |
| CNAME 去重不规范 | 中等 | dnsserver/handler_response.go | 217-240 | RFC 1035 | 未规范化域名，无循环检测 |
| 记录去重键不完整 | 中等 | dnsserver/handler_response.go | 294-320 | RFC 1035 | 未考虑 TTL |
| CNAME 链构建不规范 | 严重 | dnsserver/handler_response.go | 195-240 | RFC 1035 | TTL 处理不当，无验证 |
| Answer 顺序不确定 | 中等 | dnsserver/handler_response.go | - | RFC 1035 | 使用 map 导致顺序不确定 |
| Authority Section 不完整 | 中等 | dnsserver/handler_response.go | - | RFC 1035 | 仅处理 SOA，未处理 NS |
| 并行查询 TTL 选择 | 严重 | upstream/manager_parallel.go | 240-258 | RFC 1035 | 选择最小 TTL（实际正确） |
| NXDOMAIN 处理不一致 | 中等 | upstream/manager_*.go | 101-110 | RFC 1035 | 不同策略处理不同 |
| 记录合并去重不完整 | 中等 | upstream/manager_parallel.go | 265-290 | RFC 1035 | 仅处理 A/AAAA/CNAME |
| 本地域名处理 | 中等 | dnsserver/handler_custom.go | 120-132 | RFC 6762 | 硬编码列表不完整 |
| 反向 DNS 查询 | 中等 | dnsserver/handler_custom.go | 107-112 | RFC 1035 | 拒绝所有反向查询 |
| 单标签域名处理 | 低 | dnsserver/handler_custom.go | 81-86 | RFC 1035 | 拒绝所有单标签域名 |

---

## 按文件分类

### dnsserver/handler_query.go
- **行 159-174**: NXDOMAIN/NODATA 混淆 (严重)
- **行 89-96**: SERVFAIL 缓存 (中等)
- **行 226-250**: DNSSEC 验证标志 (严重)
- **行 226**: DO 标志处理 (中等)

### dnsserver/handler_response.go
- **行 40-75**: IP 去重不完整 (严重)
- **行 195-240**: CNAME 链构建不规范 (严重)
- **行 217-240**: CNAME 去重不规范 (中等)
- **行 294-320**: 记录去重键不完整 (中等)
- **行 283-290**: deduplicateDNSMsg 函数

### dnsserver/handler_cache.go
- **行 14-30**: 负缓存 TTL 处理 (严重)
- **行 50-65**: UserReturnTTL 循环逻辑 (中等)

### dnsserver/sorting.go
- **行 82-103**: TTL 计算精度问题 (严重)

### dnsserver/utils.go
- **行 96-115**: 错误码提取不完整 (中等)

### dnsserver/handler_custom.go
- **行 81-86**: 单标签域名处理 (低)
- **行 107-112**: 反向 DNS 查询处理 (中等)
- **行 120-132**: 本地域名处理 (中等)

### upstream/manager.go
- **行 118-119**: Question 验证不足 (中等)

### upstream/manager_parallel.go
- **行 240-258**: 并行查询 TTL 选择 (严重)
- **行 265-290**: 记录合并去重不完整 (中等)

### upstream/manager_sequential.go
- **行 101-110**: NXDOMAIN 处理不一致 (中等)

### upstream/manager_random.go
- **行 89-100**: NXDOMAIN 处理不一致 (中等)

### upstream/manager_utils.go
- **行 100-115**: 负缓存 TTL 提取 (严重)
- **行 265-290**: 记录合并去重 (中等)

### cache/cache_raw.go
- **行 8-15**: 缓存过期检查不一致 (中等)

### cache/cache_dnssec.go
- **行 95-99**: DNSSEC 记录过滤 (严重)

### cache/entries.go
- 缓存过期检查不一致 (中等)

### upstream/manager_*.go (所有)
- **行 60-62**: EDNS0 OPT 处理不完整 (严重)

---

## 按 RFC 分类

### RFC 1035 (DNS Protocol)
- Question 验证不足
- Message ID/Flags 处理
- TTL 计算精度问题
- 缓存过期检查不一致
- UserReturnTTL 循环逻辑
- IP 去重不完整
- CNAME 去重不规范
- 记录去重键不完整
- CNAME 链构建不规范
- Answer 顺序不确定
- Authority Section 不完整
- 并行查询 TTL 选择
- NXDOMAIN 处理不一致
- 记录合并去重不完整
- 反向 DNS 查询处理
- 单标签域名处理

### RFC 2308 (Negative Caching)
- 负缓存 TTL 处理不规范
- NXDOMAIN/NODATA 混淆
- SERVFAIL 缓存

### RFC 3225 (DNSSEC Lookaside Validation)
- DO 标志处理不完整

### RFC 4034 (DNSSEC Algorithm Numbers)
- RRSIG/NSEC 记录处理缺失

### RFC 4035 (DNSSEC Protocol)
- DNSSEC 验证标志处理不当

### RFC 6762 (mDNS)
- 本地域名处理不规范

### RFC 6891 (EDNS0)
- EDNS0 OPT 记录处理不完整

---

## 修复优先级建议

### 立即修复（P1）
1. dnsserver/sorting.go:82-103 - TTL 计算精度
2. dnsserver/handler_cache.go:14-30 - 负缓存 TTL
3. dnsserver/handler_query.go:159-174 - NXDOMAIN/NODATA
4. dnsserver/handler_query.go:226-250 - DNSSEC 验证标志
5. dnsserver/handler_response.go:40-75 - IP 去重
6. dnsserver/handler_response.go:195-240 - CNAME 链构建
7. upstream/manager_*.go:60-62 - EDNS0 处理

### 尽快修复（P2）
1. upstream/manager.go:118-119 - Question 验证
2. cache/cache_raw.go:8-15 - 缓存过期检查
3. dnsserver/handler_cache.go:50-65 - UserReturnTTL
4. dnsserver/handler_query.go:89-96 - SERVFAIL 缓存
5. dnsserver/utils.go:96-115 - 错误码提取
6. dnsserver/handler_query.go:226 - DO 标志
7. dnsserver/handler_response.go:217-240 - CNAME 去重
8. upstream/manager_parallel.go:265-290 - 记录合并去重

### 逐步改进（P3）
1. dnsserver/handler_response.go:294-320 - 记录去重键
2. dnsserver/handler_response.go - Answer 顺序
3. dnsserver/handler_response.go - Authority Section
4. upstream/manager_utils.go - RRSIG/NSEC 处理
5. dnsserver/handler_custom.go:120-132 - 本地域名
6. dnsserver/handler_custom.go:107-112 - 反向 DNS
7. dnsserver/handler_custom.go:81-86 - 单标签域名

