# IP 测速排序问题修复总结

## 问题描述

获取上游 IP 后，IP 没有进行测速排序，而是按数值大小进行排序。

## 根本原因

在 `dnsserver/handler_cache.go` 的 `handleRawCacheHit()` 函数中，当原始缓存命中时，代码直接使用 `fallbackIPs`（通过 `prefetcher.GetFallbackRank()` 获得的历史排序）返回给客户端，**而没有检查排序缓存**。

### 流程分析

**正确的流程应该是：**
1. 获取上游 IP → 缓存到原始缓存
2. 异步触发 `sortIPsAsync()` 进行测速排序
3. 排序完成后，结果缓存到排序缓存
4. 后续查询时，优先使用排序缓存中的排序结果

**实际的错误流程：**
1. 获取上游 IP → 缓存到原始缓存
2. 异步触发 `sortIPsAsync()` 进行测速排序
3. 排序完成后，结果缓存到排序缓存
4. **后续查询时，直接使用 `fallbackIPs`（历史排序），而不是排序缓存** ❌

## 修复方案

修改 `dnsserver/handler_cache.go` 中的 `handleRawCacheHit()` 函数，添加排序缓存检查逻辑：

```go
// 优先使用排序缓存，如果不存在则使用历史数据进行兜底排序
var ipsToReturn []string

// 1. 首先尝试获取排序缓存
if sorted, ok := s.cache.GetSorted(domain, qtype); ok {
    logger.Debugf("[handleQuery] 排序缓存命中: %s (type=%s) -> %v", domain, dns.TypeToString[qtype], sorted.IPs)
    ipsToReturn = sorted.IPs
} else {
    // 2. 排序缓存不存在，使用历史数据进行兜底排序 (Fallback Rank)
    rankDomain := domain
    if len(raw.CNAMEs) > 0 {
        rankDomain = strings.TrimRight(raw.CNAMEs[len(raw.CNAMEs)-1], ".")
    }
    ipsToReturn = s.prefetcher.GetFallbackRank(rankDomain, raw.IPs)
    logger.Debugf("[handleQuery] 使用兜底排序: %s (type=%s) -> %v", domain, dns.TypeToString[qtype], ipsToReturn)
}

// 使用 ipsToReturn 而不是 fallbackIPs 返回给客户端
```

## 修复效果

- ✅ 首次查询：使用 `fallbackIPs`（历史排序）快速响应
- ✅ 异步排序：后台执行 ping 测速，结果缓存到排序缓存
- ✅ 后续查询：优先使用排序缓存中的测速排序结果
- ✅ 兜底方案：如果排序缓存不存在或过期，使用历史排序

## 相关文件

- **修改文件**：`dnsserver/handler_cache.go`
- **相关文件**：
  - `dnsserver/sorting.go` - 排序逻辑
  - `cache/sortqueue.go` - 排序队列
  - `ping/ping_concurrent.go` - 测速排序算法
  - `prefetch/fallback.go` - 历史排序（兜底方案）

## 验证方法

1. 查询一个域名（首次查询，使用 fallbackIPs）
2. 等待排序完成（异步 ping 测速）
3. 再次查询同一域名（应该使用排序缓存中的测速结果）
4. 观察 IP 顺序是否按测速结果排序，而不是按数值排序
