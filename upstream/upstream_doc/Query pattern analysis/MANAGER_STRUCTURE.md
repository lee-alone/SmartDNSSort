# Upstream Manager Package Structure

## Overview
upstream 包中的 manager.go 文件已被拆分为多个专注的文件，以提高代码的可维护性和可读性。

## File Organization

### 1. **manager.go**
主管理器和核心逻辑。
- `QueryResult` 结构体 - 查询结果
- `QueryResultWithTTL` 结构体 - 带 TTL 信息的查询结果
- `Manager` 结构体 - 上游 DNS 查询管理器
- `NewManager()` - 创建管理器实例
- `SetCacheUpdateCallback()` - 设置缓存更新回调
- `GetServers()` - 获取所有服务器
- `GetHealthyServerCount()` - 获取健康服务器数量
- `GetTotalServerCount()` - 获取总服务器数量
- `Query()` - 主查询入口，根据策略分发到不同的查询方法

### 2. **manager_parallel.go**
并行查询策略实现。
- `queryParallel()` - 并行查询多个上游 DNS 服务器
  - 快速响应机制：第一个成功响应立即返回
  - 后台收集其他响应并更新缓存
- `collectRemainingResponses()` - 在后台收集剩余响应
  - 汇总所有 IP 地址
  - 选择最小 TTL
  - 调用缓存更新回调
- `mergeAndDeduplicateIPs()` - 汇总并去重 IP 地址

### 3. **manager_random.go**
随机查询策略实现。
- `queryRandom()` - 随机选择服务器进行查询
  - 按随机顺序尝试所有服务器
  - 完整容错机制
  - 支持 NXDOMAIN 直接返回
  - 支持空结果处理

### 4. **manager_sequential.go**
顺序查询策略实现。
- `querySequential()` - 按健康度排序后顺序查询
  - 优先使用健康度最好的服务器
  - 区分不同类型的错误
  - 支持超时和网络错误处理
  - 支持 NXDOMAIN 直接返回

### 5. **manager_racing.go**
竞争查询策略实现。
- `queryRacing()` - 竞争查询策略
  - 为第一个服务器争取时间
  - 延迟后发起备选竞争请求
  - 返回最先到达的有效结果
  - 支持 NXDOMAIN 确定性错误处理

### 6. **manager_utils.go**
工具函数和辅助方法。
- `extractIPs()` - 从 DNS 响应中提取 IP、CNAME 和 TTL
- `extractNegativeTTL()` - 从 NXDOMAIN 响应中提取否定缓存 TTL
- `getSortedHealthyServers()` - 按健康度排序服务器
- `isDNSError()` - 检查是否是 DNS 错误
- `isDNSNXDomain()` - 检查是否是 NXDOMAIN 错误

## Query Strategies

### 1. Parallel (并行)
- 同时向所有服务器发起查询
- 第一个成功响应立即返回给客户端
- 后台继续收集其他响应，汇总 IP 并更新缓存
- 优点：快速响应，获得最完整的 IP 池
- 缺点：消耗更多网络资源

### 2. Random (随机)
- 随机选择服务器顺序
- 按顺序尝试，直到找到成功响应
- 完整容错机制
- 优点：简单、均衡负载
- 缺点：可能需要多次尝试

### 3. Sequential (顺序)
- 按健康度排序后顺序尝试
- 优先使用健康度最好的服务器
- 优点：优先使用最可靠的服务器
- 缺点：可能不够均衡

### 4. Racing (竞争)
- 立即向最佳服务器发起查询
- 延迟后发起备选竞争请求
- 返回最先到达的有效结果
- 优点：平衡速度和可靠性
- 缺点：实现复杂

## Error Handling

所有查询策略都支持以下错误处理：

1. **NXDOMAIN (域名不存在)**
   - 确定性错误，直接返回
   - 从 SOA 记录提取否定缓存 TTL

2. **其他 DNS 错误**
   - 尝试下一个服务器
   - 记录统计信息

3. **网络错误**
   - 超时：尝试下一个服务器
   - 连接错误：尝试下一个服务器

4. **空结果**
   - 继续尝试其他服务器
   - 最后返回空结果

## Health Check Integration

所有查询策略都集成了健康检查机制：

- 跳过临时不可用的服务器（熔断状态）
- 记录服务器成功/失败统计
- 支持服务器恢复

## DNSSEC Support

所有查询策略都支持 DNSSEC：

- 检查请求中的 DO 标志
- 保存原始 DNS 消息
- 转发 AuthenticatedData 标记

## Dependencies

```
manager.go (主入口)
    ├─ manager_parallel.go
    ├─ manager_random.go
    ├─ manager_sequential.go
    ├─ manager_racing.go
    └─ manager_utils.go
```

## Key Features

1. **职责分离** - 每个查询策略独立实现
2. **易于维护** - 相关逻辑聚集在一起
3. **易于测试** - 可以独立测试各个策略
4. **易于扩展** - 添加新策略只需创建新文件
5. **完整的错误处理** - 支持多种错误场景
6. **健康检查集成** - 自动跳过不健康的服务器
7. **DNSSEC 支持** - 完整的 DNSSEC 消息处理

## Testing

所有现有的测试文件保持不变。
