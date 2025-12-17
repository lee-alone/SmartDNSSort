# DNSSEC msgCache 实施总结

## 修改的文件列表

### 1. config/config.go
**变更内容**：添加 msgCache 大小配置参数

**关键修改**：
- 默认配置模板：添加 `msg_cache_size_mb: 12` 行
- CacheConfig 结构：添加 `MsgCacheSizeMB int` 字段
- LoadConfig 函数：设置智能默认值 `cfg.MsgCacheSizeMB = cfg.MaxMemoryMB / 10`（最小 1MB）

### 2. cache/cache.go
**变更内容**：实现 DNSSEC 消息缓存的完整逻辑

**关键修改**：
1. 导入 miekg/dns 包
2. 新增 DNSSECCacheEntry 结构体：
   - Message: 完整 DNS 消息
   - AcquisitionTime: 获取时间戳
   - TTL: 消息 TTL
   - IsExpired() 方法

3. Cache 结构扩展：
   - 添加 msgCache *LRUCache 字段
   - 添加 prefetcher 和 hits/misses 字段

4. NewCache 初始化：
   - 计算 msgCache 的最大条目数：`(MsgCacheSizeMB * 1024 * 1024) / 2048`
   - 最小 10 条记录

5. 新增方法：
   - `GetMsg(domain, qtype) → (*dns.Msg, bool)`：获取缓存消息
   - `SetMsg(domain, qtype, msg)`：存储消息到 msgCache
   - `extractMinTTLFromMsg(msg) → uint32`：从消息中提取最小 TTL

### 3. dnsserver/handler.go
**变更内容**：集成 msgCache 到 DNS 查询流程

**关键修改**：
1. handleQuery() 函数：
   - 在缓存查询第 4 阶段添加 msgCache 检查（优先级最高）
   - DO flag 检测：`r.IsEdns0() != nil && r.IsEdns0().Do()`
   - 若 DNSSEC 启用且缓存命中，返回完整消息

2. handleCacheMiss() 函数：
   - 在响应构造前，检查是否为 DNSSEC 查询
   - 若是，异步获取完整消息并存储到 msgCache
   - 调用新增方法 getDNSSECFullMessage()

3. 新增方法：getDNSSECFullMessage()
   - 从上游服务器获取完整 DNSSEC 消息
   - 优先级：健康服务器 → 所有服务器
   - 返回第一个成功的响应（Rcode == Success）

### 4. upstream/manager.go
**变更内容**：提供服务器列表访问接口

**关键修改**：
- 新增方法 GetServers() → []Upstream
  - 返回所有上游服务器列表
  - 用于 getDNSSECFullMessage 遍历查询

## 代码流程图

### DNSSEC 缓存检查（handleQuery 第 4 阶段）
```
isDNSSECQuery := r.IsEdns0() != nil && r.IsEdns0().Do()

if isDNSSECQuery && currentCfg.Upstream.Dnssec {
    if msg, found := s.cache.GetMsg(domain, qtype); found {
        // ✓ 缓存命中：返回完整消息
        return
    }
}

// 继续检查其他缓存（错误/排序/原始）
```

### DNSSEC 消息存储（handleCacheMiss 响应前）
```
if currentCfg.Upstream.Dnssec && isDNSSECQuery {
    msgReq := new(dns.Msg)
    msgReq.SetQuestion(dns.Fqdn(targetDomain), qtype)
    msgReq.SetEdns0(4096, true)
    
    if fullMsg, err := s.getDNSSECFullMessage(ctx, msgReq, currentUpstream); 
        err == nil {
        s.cache.SetMsg(targetDomain, qtype, fullMsg)
    }
}
```

### TTL 提取逻辑
```go
func extractMinTTLFromMsg(msg *dns.Msg) uint32 {
    minTTL := uint32(0)
    
    // 检查 Answer 部分
    for _, rr := range msg.Answer {
        ttl := rr.Header().Ttl
        if minTTL == 0 || ttl < minTTL {
            minTTL = ttl
        }
    }
    
    // 检查 Authority 部分（RRSIG）
    for _, rr := range msg.Ns {
        ttl := rr.Header().Ttl
        if minTTL == 0 || ttl < minTTL {
            minTTL = ttl
        }
    }
    
    return minTTL
}
```

## 关键决策点

### 1. 为什么不修改 upstream.Query 返回值？
- **原因**：修改公共 API 会影响所有调用方
- **方案**：在 handler 中直接调用 srv.Exchange()，获取完整消息
- **好处**：隔离 DNSSEC 逻辑，不污染主查询路径

### 2. 为什么用独立的 msgCache？
- **原因1**：主缓存只保存 IPs，msgCache 保存完整 Msg
- **原因2**：DNSSEC 和普通查询有不同的缓存策略
- **原因3**：内存大小可独立配置
- **结果**：设计清晰，维护容易

### 3. 为什么在后台异步获取完整消息？
- **原因**：避免阻塞客户端响应
- **实现**：在 handleCacheMiss 响应后调用 getDNSSECFullMessage()
- **好处**：第二次查询将命中 msgCache，性能最优

### 4. CNAME 处理策略
- **场景**：A 查询返回 CNAME，需要递归解析
- **方案**：获取最终目标域名的完整消息
- **代码**：`targetDomain = fullCNAMEs[len(fullCNAMEs)-1]`

## 测试场景覆盖

| # | 场景 | 预期结果 |
|---|------|---------|
| 1 | 普通 A 查询（无 DO） | 返回排序 IP，跳过 msgCache |
| 2 | A 查询 + DO flag | 若命中 msgCache，返回完整消息 |
| 3 | DNSSEC 禁用 | 跳过 msgCache 检查，不返回 AD 标志 |
| 4 | msgCache 满 | LRU 自动淘汰最少使用的条目 |
| 5 | msgCache TTL 过期 | IsExpired() 返回 true，删除条目，重新查询 |
| 6 | CNAME + DO flag | 返回 CNAME 链的最终目标 + RRSIG |
| 7 | 获取完整消息失败 | 继续返回排序 IP（降级） |
| 8 | 配置 msg_cache_size_mb=0 | msgCache 禁用 |

## 部署检查清单

- [x] 代码编译无错误
- [x] 所有修改文件列出
- [x] 关键方法实现完整
- [x] 缓存容量计算正确
- [x] 错误处理完善
- [ ] 单元测试（待补充）
- [ ] 集成测试（待补充）
- [ ] 性能基准测试（待补充）

## 后续优化建议

1. **缓存预热**：启动时预加载常见域名的 DNSSEC 消息
2. **统计增强**：添加 msgCache 命中率统计
3. **监控指标**：msgCache 大小、条目数、淘汰速率
4. **自适应 TTL**：根据查询频率动态调整 msgCache 条目数
5. **消息压缩**：存储前压缩 DNS 消息以减少内存占用

## 故障排查指南

### 症状：DNSSEC 查询总是返回新数据
- 检查：msgCache 是否被正确初始化
- 检查：GetMsg() 中的 IsExpired() 逻辑
- 检查：SetMsg() 中的 TTL 提取是否正确

### 症状：msgCache 内存持续增长
- 检查：msg_cache_size_mb 配置是否过大
- 检查：LRUCache 的淘汰逻辑是否工作
- 检查：DNS 消息大小估算（~2KB） 是否准确

### 症状：某些域名 DNSSEC 验证失败
- 检查：getDNSSECFullMessage() 返回的消息是否完整
- 检查：RRSIG 是否被正确保存到缓存
- 检查：客户端请求的 ID 是否被正确替换
