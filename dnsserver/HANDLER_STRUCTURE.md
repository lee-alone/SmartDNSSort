# DNS Handler Package Structure

## Overview
dnsserver 包中的 handler.go 文件已被拆分为多个专注的文件，以提高代码的可维护性和可读性。

## File Organization

### 1. **handler.go**
主文件（现为占位符），说明拆分结构。

### 2. **handler_adblock.go**
AdBlock 相关的处理逻辑。
- `handleAdBlockCheck()` - 执行 AdBlock 过滤检查
  - 检查拦截缓存
  - 检查白名单缓存
  - 执行规则匹配
  - 返回拦截响应
- `handleCNAMEChainValidation()` - 对 CNAME 链进行 AdBlock 检查

### 3. **handler_custom.go**
自定义回复规则和本地规则处理。
- `handleCustomResponse()` - 处理自定义回复规则
  - 支持 CNAME 响应
  - 支持 A/AAAA 响应
- `handleLocalRules()` - 应用本地硬编码规则
  - 单标签域名检查
  - localhost 处理
  - 反向 DNS 查询检查
  - 特定域名黑名单

### 4. **handler_cache.go**
缓存查询处理逻辑。
- `handleErrorCacheHit()` - 处理错误缓存命中 (NXDOMAIN)
- `handleSortedCacheHit()` - 处理排序完成后的缓存命中
  - Stale-While-Revalidate 模式
  - TTL 计算逻辑
  - 异步刷新触发
- `handleRawCacheHit()` - 处理原始缓存（上游DNS响应）命中
  - 兜底排序（Fallback Rank）
  - 过期检查和刷新

### 5. **handler_query.go**
主查询处理逻辑。
- `handleCacheMiss()` - 处理缓存未命中的情况（首次查询）
  - IPv6 开关检查
  - 动态超时计算
  - 上游查询执行
  - CNAME 递归解析
  - CNAME 链 AdBlock 检查
  - 缓存和排序任务创建
  - DNSSEC 消息缓存
- `handleQuery()` - 主查询处理入口
  - 5 个阶段的查询处理流程
  - 缓存优先级管理

### 6. **handler_response.go**
DNS 响应构造相关方法。
- `buildDNSResponse()` - 构造基础 DNS 响应
- `buildDNSResponseWithDNSSEC()` - 构造带 DNSSEC 标记的 DNS 响应
- `buildDNSResponseWithCNAME()` - 构造包含 CNAME 和 IP 的完整 DNS 响应
- `buildDNSResponseWithCNAMEAndDNSSEC()` - 构造包含 CNAME、IP 和 DNSSEC 标记的完整 DNS 响应

### 7. **handler_cname.go**
CNAME 解析相关方法。
- `resolveCNAME()` - 递归解析 CNAME，直到找到 IP 地址
  - 最多 10 次重定向
  - 累加所有 CNAME
  - 返回最终 IP 和完整 CNAME 链

### 8. **utils.go** (已存在)
辅助函数定义。
- `buildNXDomainResponse()` - 构造 NXDOMAIN 响应
- `buildZeroIPResponse()` - 构造零 IP 响应
- `buildRefuseResponse()` - 构造 REFUSED 响应
- `parseRcodeFromError()` - 从错误中解析 DNS Rcode

## Query Processing Flow

```
handleQuery()
    ↓
1. AdBlock 检查 → handleAdBlockCheck()
    ↓
2. 自定义回复检查 → handleCustomResponse()
    ↓
3. 本地规则检查 → handleLocalRules()
    ↓
4. 缓存查询 (优先级)
    ├─ DNSSEC msgCache → cache.GetMsg()
    ├─ 错误缓存 → handleErrorCacheHit()
    ├─ 排序缓存 → handleSortedCacheHit()
    └─ 原始缓存 → handleRawCacheHit()
    ↓
5. 缓存未命中 → handleCacheMiss()
    ├─ IPv6 检查
    ├─ 上游查询
    ├─ CNAME 递归解析 → resolveCNAME()
    ├─ CNAME 链 AdBlock 检查 → handleCNAMEChainValidation()
    ├─ 缓存存储
    ├─ 排序任务创建
    └─ 响应构造 → buildDNSResponse*()
```

## Dependencies

```
handler_query.go (主入口)
    ├─ handler_adblock.go
    ├─ handler_custom.go
    ├─ handler_cache.go
    ├─ handler_cname.go
    └─ handler_response.go
```

## Key Features

1. **职责分离** - 每个文件专注于特定的功能
2. **易于维护** - 相关逻辑聚集在一起
3. **易于测试** - 可以独立测试各个处理阶段
4. **易于扩展** - 添加新功能时只需修改相关文件

## Testing

所有现有的测试文件保持不变：
- `server_test.go` - 服务器测试
- `sorting_test.go` - 排序测试
