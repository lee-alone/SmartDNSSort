# DNS RFC 违规情况汇总

## 概览

**总计违规点：28 项**
- 严重（P1）：7 项
- 中等（P2）：14 项  
- 低级（P3）：7 项

---

## 严重违规（P1）- 需立即修复

### 1. TTL 计算精度问题
**文件**: `dnsserver/sorting.go:82-103`
**RFC**: RFC 1035, RFC 2181
**问题**: 
- 使用浮点数计算 TTL，精度丢失
- TTL 可能变为负数
- 代码强制设为 1，违反 RFC

**影响**: 缓存 TTL 不准确，可能导致频繁查询或过期数据

---

### 2. 负缓存 TTL 处理不规范
**文件**: `dnsserver/handler_cache.go:14-30`, `upstream/manager_utils.go:100-115`
**RFC**: RFC 2308 (Negative Caching)
**问题**:
- 未正确实现 RFC 2308 的负缓存规则
- 未区分 NXDOMAIN 和 NODATA
- 默认值硬编码为 300 秒

**影响**: 负缓存策略错误，可能导致缓存污染

---

### 3. NXDOMAIN 和 NODATA 混淆
**文件**: `dnsserver/handler_query.go:159-174`
**RFC**: RFC 2308, RFC 1035
**问题**:
- 混为一谈，使用相同的缓存策略
- 返回码设置错误（NODATA 应为 NOERROR）
- 无法区分两种情况

**影响**: 错误的缓存策略，客户端收到错误的响应码

---

### 4. DNSSEC 验证标志处理不当
**文件**: `dnsserver/handler_query.go:226-250`, `cache/cache_dnssec.go:95-99`
**RFC**: RFC 4035 (DNSSEC Protocol)
**问题**:
- 过滤掉 DNSKEY 和 DS 记录
- 导致客户端无法进行 DNSSEC 验证
- AD 标志转发逻辑不清晰

**影响**: DNSSEC 验证失败，安全性降低

---

### 5. IP 去重逻辑不完整
**文件**: `dnsserver/handler_response.go:40-75`
**RFC**: RFC 1035
**问题**:
- IPv6 地址规范化不完整
- "::1" 和 "0:0:0:0:0:0:0:1" 被视为不同
- 未处理 IPv4-mapped IPv6 地址

**影响**: 可能返回重复的 IP（不同表示形式）

---

### 6. CNAME 链构建不规范
**文件**: `dnsserver/handler_response.go:195-240`
**RFC**: RFC 1035
**问题**:
- 所有 CNAME 使用相同 TTL，但上游可能返回不同 TTL
- 未验证 CNAME 目标有效性
- 未检测 CNAME 循环

**影响**: CNAME 链信息不准确，可能导致客户端错误

---

### 7. EDNS0 OPT 记录处理不完整
**文件**: `upstream/manager_*.go:60-62`
**RFC**: RFC 6891 (EDNS0)
**问题**:
- 硬编码 UDP 缓冲区大小为 4096
- 未从客户端请求中提取
- 未处理 OPT 扩展选项

**影响**: 无法正确处理大型 DNS 响应

---

## 中等违规（P2）- 应尽快修复

### 8. Question Section 验证不足
**文件**: `upstream/manager.go:118-119`
**RFC**: RFC 1035
**问题**: 仅检查是否为空，未验证数量和有效性

---

### 9. 缓存过期检查不一致
**文件**: `cache/entries.go`, `cache/cache_raw.go:8-15`
**RFC**: RFC 1035
**问题**: GetRaw() 不检查过期，但 GetSorted() 检查

---

### 10. SERVFAIL 缓存不规范
**文件**: `dnsserver/handler_query.go:89-96`
**RFC**: RFC 2308
**问题**: 缓存 SERVFAIL 响应，RFC 建议不应缓存

---

### 11. 错误响应码提取不完整
**文件**: `dnsserver/utils.go:96-115`
**RFC**: RFC 1035
**问题**: 仅处理特定格式，默认返回 SERVFAIL

---

### 12. CNAME 去重不规范
**文件**: `dnsserver/handler_response.go:217-240`
**RFC**: RFC 1035
**问题**: 未规范化域名，无循环检测

---

### 13. 并行查询 TTL 选择
**文件**: `upstream/manager_parallel.go:240-258`
**RFC**: RFC 1035
**问题**: 选择最小 TTL（实际正确，但应有配置选项）

---

### 14. NXDOMAIN 处理不一致
**文件**: `upstream/manager_sequential.go:101-110`, `upstream/manager_random.go:89-100`
**RFC**: RFC 1035
**问题**: 不同策略处理不一致

---

### 15. 记录合并去重不完整
**文件**: `upstream/manager_parallel.go:265-290`
**RFC**: RFC 1035
**问题**: 仅处理 A/AAAA/CNAME，其他记录处理不完整

---

### 16-21. 其他中等违规
- Message ID/Flags 处理
- UserReturnTTL 循环逻辑
- DO 标志处理不完整
- RRSIG/NSEC 记录处理缺失
- 记录去重键生成不完整
- Authority Section 处理不完整

---

## 低级违规（P3）- 可逐步改进

### 22. 本地域名处理不规范
**文件**: `dnsserver/handler_custom.go:120-132`
**RFC**: RFC 6762 (mDNS)
**问题**: 硬编码列表不完整，未实现 mDNS

---

### 23. 反向 DNS 查询处理不完整
**文件**: `dnsserver/handler_custom.go:107-112`
**RFC**: RFC 1035
**问题**: 拒绝所有反向查询，未实现反向解析

---

### 24. 单标签域名处理
**文件**: `dnsserver/handler_custom.go:81-86`
**RFC**: RFC 1035
**问题**: 拒绝所有单标签域名，未区分合法情况

---

### 25-28. 其他低级违规
- Answer Section 顺序不确定
- 缓存一致性问题
- 特殊记录处理
- 其他边界情况

---

## 按影响程度分类

### 高影响（影响功能正确性）
1. TTL 计算精度 - 缓存失效
2. NXDOMAIN/NODATA 混淆 - 错误的缓存策略
3. DNSSEC 处理 - 安全性降低
4. CNAME 链构建 - 解析错误
5. IP 去重 - 可能返回重复 IP

### 中影响（影响兼容性）
1. EDNS0 处理 - 大型响应失败
2. 错误码提取 - 错误处理不当
3. 缓存过期检查 - 行为不一致
4. Question 验证 - 边界情况处理

### 低影响（影响特殊场景）
1. 本地域名处理 - 特定场景
2. 反向 DNS - 特定查询
3. 单标签域名 - 特定场景
4. Answer 顺序 - 客户端兼容性

---

## 修复建议

### 第一阶段（1-2 周）- 修复 P1 严重违规
优先修复：
1. TTL 计算精度
2. NXDOMAIN/NODATA 区分
3. DNSSEC 记录保留
4. IP 去重规范化
5. CNAME 链验证

### 第二阶段（2-3 周）- 修复 P2 中等违规
重点修复：
1. 缓存过期检查统一
2. EDNS0 处理完整
3. 错误码提取完整
4. CNAME 去重规范化

### 第三阶段（持续）- 改进 P3 低级违规
逐步改进：
1. 本地域名支持
2. 反向 DNS 支持
3. 特殊域名处理
4. 边界情况处理

---

## 测试建议

### 单元测试
- TTL 计算精度测试
- NXDOMAIN/NODATA 区分测试
- IP 去重规范化测试
- CNAME 链验证测试

### 集成测试
- 完整的 DNS 查询流程
- 缓存策略验证
- DNSSEC 验证流程
- 错误处理流程

### RFC 合规性测试
- 使用 DNS 测试工具（如 dig、nslookup）
- 验证响应格式
- 验证缓存行为
- 验证错误处理

---

## 相关文件

详细分析文档：
- `DNS_RFC_COMPLIANCE_ANALYSIS.md` - 完整分析报告
- `RFC_VIOLATIONS_FILE_MAPPING.md` - 文件映射表
- `RFC_VIOLATIONS_CODE_EXAMPLES.md` - 代码示例和修复方案

