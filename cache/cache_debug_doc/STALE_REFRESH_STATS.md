# 缓存更新统计功能说明

## 概述

为了解决"总查询数不等于缓存命中+缓存未命中"的问题，添加了"缓存更新"统计项。

## 统计项说明

### 总查询数 = 缓存命中 + 缓存未命中 + 缓存更新

- **缓存命中 (Cache Hits)**：直接从缓存返回有效数据
- **缓存未命中 (Cache Misses)**：缓存中没有数据，需要向上游查询
- **缓存更新 (Cache Refresh)**：缓存已过期，但仍返回给用户，同时异步向上游查询更新数据

## 实现细节

### 后端改动

1. **stats/stats.go**
   - 添加 `cacheStaleRefresh` 计数器
   - 添加 `IncCacheStaleRefresh()` 方法
   - 在 `GetStats()` 中返回 `cache_stale_refresh` 字段
   - 在 `Reset()` 中重置该计数器

2. **dnsserver/handler_cache.go**
   - 在 `handleRawCacheHit()` 中，当缓存过期时调用 `stats.IncCacheStaleRefresh()`
   - 在 `handleRawCacheHitGeneric()` 中，当缓存过期时调用 `stats.IncCacheStaleRefresh()`

### 前端改动

1. **webapi/web/components/dashboard.html**
   - 在"常规统计"卡片中添加"缓存更新"显示项

2. **webapi/web/js/modules/dashboard.js**
   - 在 `updateDashboard()` 中添加 `cache_stale_refresh` 的显示逻辑

3. **国际化文件**
   - **resources-en.js**：添加 `"cacheStaleRefresh": "Cache Refresh"`
   - **resources-zh-cn.js**：添加 `"cacheStaleRefresh": "缓存更新"`

## 用户体验改进

用户现在可以清楚地看到：
- 有多少查询直接从缓存获得（缓存命中）
- 有多少查询需要向上游查询（缓存未命中）
- 有多少查询返回了过期缓存但同时向上游更新（缓存更新）

这三个数字的总和等于总查询数，消除了用户的困惑。

## API 响应示例

```json
{
  "total_queries": 1000,
  "cache_hits": 600,
  "cache_misses": 300,
  "cache_stale_refresh": 100,
  "cache_hit_rate": 60.0,
  ...
}
```

验证：600 + 300 + 100 = 1000 ✓
