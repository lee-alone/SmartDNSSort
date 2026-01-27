# DNS 上游查询性能瓶颈代码级分析 - 第二部分

## 4. 并行查询的后台收集延迟

### 问题代码位置
`upstream/manager_parallel.go` - queryParallel 和 collectRemainingResponses 函数

### 问题代码
```go
func (u *Manager) queryParallel(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
    // ...
    
    // 用于标记是否已经发送了快速响应
    var fastResponseSent sync.Once

    // 并发查询所有服务器
    for _, server := range u.servers {
        wg.Add(1)
        go func(srv Upstream) {
            defer wg.Done()
            // ...
            
            // 第一个成功响应立即返回
            if reply.Rcode == dns.RcodeSuccess {
                fastResponseSent.Do(func() {
                    select {
                    case fastResponseChan <- result:
                        logger.Debugf("[queryParallel] 快速响应已发送")
                    case <-queryCtx.Done():
                    }
                })
            }
            
            // 所有响应都放入 resultChan
            select {
            case resultChan <- result:
            case <-queryCtx.Done():
            }
        }(server)
    }

    // 等待第一个成功响应
    select {
    case result := <-fastResponseChan:
        logger.Debugf("[queryParallel] 返回快速响应")
        
        // 后台继续收集其他响应
        go func() {
            u.collectRemainingResponses(resultChan, domain, qtype)
        }()
        
        return result, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}

// 后台收集剩余响应
func (u *Manager) collectRemainingResponses(resultChan chan *QueryResult, domain string, qtype uint16) {
    var allResults []*QueryResult
    
    // 收集所有响应
    for result := range resultChan {
        if result.Error == nil && result.Rcode == dns.RcodeSuccess {
            allResults = append(allResults, result)
        }
    }
    
    // 汇总 IP 并更新缓存
    if len(allResults) > 0 {
        mergedIPs := u.mergeAndDeduplicateIPs(allResults)
        
        // 调用缓存更新回调
        if u.cacheUpdateCallback != nil {
            u.cacheUpdateCallback(domain, qtype, nil, nil, 0)
        }
    }
}
```

### 问题分析

**后台收集延迟场景**
```
假设：
- 服务器数：5 个
- 第一个服务器响应时间：50ms
- 其他服务器响应时间：100-500ms

时间线：
T=50ms:   第一个服务器返回，立即返回给客户端
T=50ms:   启动后台收集 goroutine
T=100ms:  第二个服务器返回
T=200ms:  第三个服务器返回
T=300ms:  第四个服务器返回
T=500ms:  第五个服务器返回
T=500ms:  后台收集完成，更新缓存

问题：
- 缓存更新延迟 450ms（从第一个响应到最后一个响应）
- 如果后台收集失败，缓存不会更新
- 可能导致缓存不完整
```

**性能影响**
- 缓存更新延迟
- 如果后台收集失败，缓存不会更新
- 可能导致下一次查询仍然需要上游查询

### 优化方案

**方案 1：超时控制**
```go
// 后台收集最多等待 N 秒
func (u *Manager) collectRemainingResponses(resultChan chan *QueryResult, domain string, qtype uint16) {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    
    var allResults []*QueryResult
    
    for {
        select {
        case result := <-resultChan:
            if result.Error == nil && result.Rcode == dns.RcodeSuccess {
                allResults = append(allResults, result)
            }
        case <-ctx.Done():
            // 超时，停止收集
            logger.Debugf("[collectRemainingResponses] 收集超时，已收集 %d 个响应", len(allResults))
            break
        }
    }
    
    // 更新缓存
    if len(allResults) > 0 {
        u.updateCache(domain, qtype, allResults)
    }
}
```

**方案 2：错误处理和重试**
```go
// 实现错误处理和重试
func (u *Manager) collectRemainingResponses(resultChan chan *QueryResult, domain string, qtype uint16) {
    var allResults []*QueryResult
    var errors []error
    
    for result := range resultChan {
        if result.Error != nil {
            errors = append(errors, result.Error)
        } else if result.Rcode == dns.RcodeSuccess {
            allResults = append(allResults, result)
        }
    }
    
    // 如果收集失败，记录日志
    if len(errors) > 0 {
        logger.Warnf("[collectRemainingResponses] 收集过程中出现 %d 个错误", len(errors))
    }
    
    // 更新缓存
    if len(allResults) > 0 {
        if err := u.updateCache(domain, qtype, allResults); err != nil {
            logger.Warnf("[collectRemainingResponses] 缓存更新失败: %v", err)
            // 重试机制
            go func() {
                time.Sleep(100 * time.Millisecond)
                u.updateCache(domain, qtype, allResults)
            }()
        }
    }
}
```

**方案 3：缓存更新的原子性**
```go
// 实现原子性更新
func (u *Manager) updateCache(domain string, qtype uint16, results []*QueryResult) error {
    // 先汇总所有数据
    mergedIPs := u.mergeAndDeduplicateIPs(results)
    minTTL := u.getMinTTL(results)
    
    // 原子性更新缓存
    return u.cache.UpdateAtomic(domain, qtype, mergedIPs, minTTL)
}
```

---

## 5. 顺序查询的单点故障延迟

### 问题代码位置
`upstream/manager_sequential.go` - querySequential 函数

### 问题代码
```go
func (u *Manager) querySequential(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
    // 获取单次尝试的超时时间
    attemptTimeout := time.Duration(u.sequentialTimeoutMs) * time.Millisecond
    if u.sequentialTimeoutMs <= 0 {
        attemptTimeout = 1500 * time.Millisecond  // 默认 1.5 秒
    }

    var primaryError error
    var lastDNSError error

    // 按健康度排序服务器
    sortedServers := u.getSortedHealthyServers()
    if len(sortedServers) == 0 {
        sortedServers = u.servers
    }

    for i, server := range sortedServers {
        // 检查总体上下文是否已超时
        select {
        case <-ctx.Done():
            logger.Warnf("[querySequential] 总体超时，停止尝试 (已尝试 %d/%d 个服务器)",
                i, len(sortedServers))
            if primaryError == nil {
                primaryError = ctx.Err()
            }
            if lastDNSError != nil {
                return nil, lastDNSError
            }
            return nil, primaryError
        default:
        }

        // 跳过临时不可用的服务器
        if server.ShouldSkipTemporarily() {
            logger.Debugf("[querySequential] 跳过熔断状态的服务器: %s", server.Address())
            continue
        }

        logger.Debugf("[querySequential] 第 %d 次尝试: %s，超时=%v", i+1, server.Address(), attemptTimeout)

        // 为本次尝试创建短超时的上下文
        attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)

        // 执行查询
        msg := new(dns.Msg)
        msg.SetQuestion(dns.Fqdn(domain), qtype)
        if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
            msg.SetEdns0(4096, true)
        }

        reply, err := server.Exchange(attemptCtx, msg)
        cancel()

        // 处理查询错误
        if err != nil {
            if primaryError == nil {
                primaryError = err
            }

            // 区分错误类型
            if errors.Is(err, context.DeadlineExceeded) {
                // 网络超时 - 延迟 1.5 秒！
                logger.Debugf("[querySequential] 服务器 %s 超时，尝试下一个", server.Address())
                server.RecordTimeout()
                if u.stats != nil {
                    u.stats.IncUpstreamFailure(server.Address())
                }
                continue  // 继续尝试下一个
            } else {
                // 网络层错误
                logger.Debugf("[querySequential] 服务器 %s 错误: %v，尝试下一个", server.Address(), err)
                server.RecordError()
                if u.stats != nil {
                    u.stats.IncUpstreamFailure(server.Address())
                }
                continue
            }
        }

        // 处理 NXDOMAIN - 直接返回
        if reply.Rcode == dns.RcodeNameError {
            ttl := extractNegativeTTL(reply)
            if u.stats != nil {
                u.stats.IncUpstreamSuccess(server.Address())
            }
            logger.Debugf("[querySequential] 服务器 %s 返回 NXDOMAIN，立即返回", server.Address())
            server.RecordSuccess()
            return &QueryResultWithTTL{Records: nil, IPs: nil, CNAMEs: nil, TTL: ttl, DnsMsg: reply.Copy()}, nil
        }

        // 处理其他 DNS 错误响应码
        if reply.Rcode != dns.RcodeSuccess {
            lastDNSError = fmt.Errorf("dns query failed: rcode=%d", reply.Rcode)
            logger.Debugf("[querySequential] 服务器 %s 返回错误码 %d，尝试下一个",
                server.Address(), reply.Rcode)
            server.RecordError()
            if u.stats != nil {
                u.stats.IncUpstreamFailure(server.Address())
            }
            continue
        }

        // 成功！
        records, cnames, ttl := extractRecords(reply)
        var ips []string
        for _, r := range records {
            switch rec := r.(type) {
            case *dns.A:
                ips = append(ips, rec.A.String())
            case *dns.AAAA:
                ips = append(ips, rec.AAAA.String())
            }
        }

        if u.stats != nil {
            u.stats.IncUpstreamSuccess(server.Address())
        }
        server.RecordSuccess()
        return &QueryResultWithTTL{Records: records, IPs: ips, CNAMEs: cnames, TTL: ttl, AuthenticatedData: reply.AuthenticatedData, DnsMsg: reply.Copy()}, nil
    }

    // 所有服务器都失败
    if lastDNSError != nil {
        return nil, lastDNSError
    }
    return nil, primaryError
}
```

### 问题分析

**单点故障延迟场景**
```
假设：
- 服务器数：3 个
- 第一个服务器超时：1.5 秒
- 第二个服务器成功：100ms

时间线：
T=0ms:    尝试服务器 1
T=1500ms: 服务器 1 超时，尝试服务器 2
T=1600ms: 服务器 2 成功，返回结果

总延迟：1600ms（而不是 100ms）

如果第一个服务器是最健康的，但暂时故障：
- 需要等待 1.5 秒才能尝试下一个
- 这 1.5 秒内，客户端一直在等待
```

**性能影响**
- 单点故障延迟高达 1.5 秒
- 如果有多个故障服务器，延迟会更高
- 不利用多个服务器的并行能力

### 优化方案

**方案 1：并行尝试（改用 Parallel 策略）**
```go
// 不再顺序尝试，而是并行尝试
// 这样可以避免单点故障延迟
strategy: "parallel"  // 改为并行
```

**方案 2：缩短单次超时**
```go
// 当前配置
sequentialTimeoutMs: 1500  // 1.5 秒

// 优化后
sequentialTimeoutMs: 500   // 0.5 秒

// 这样可以更快地尝试下一个服务器
// 但需要确保不会误杀正常的慢速服务器
```

**方案 3：实现快速失败机制**
```go
// 如果第一个服务器超时，立即尝试下一个
// 而不是等待完整的 1.5 秒

func (u *Manager) querySequentialWithFastFail(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
    // 为第一个服务器设置更短的超时
    firstAttemptTimeout := 500 * time.Millisecond
    
    // 为其他服务器设置标准超时
    standardTimeout := 1500 * time.Millisecond
    
    sortedServers := u.getSortedHealthyServers()
    
    for i, server := range sortedServers {
        var attemptTimeout time.Duration
        if i == 0 {
            attemptTimeout = firstAttemptTimeout  // 第一个服务器更短的超时
        } else {
            attemptTimeout = standardTimeout
        }
        
        // 执行查询...
    }
}
```

---

## 6. 竞速查询的固定延迟开销

### 问题代码位置
`upstream/manager_racing.go` - queryRacing 函数

### 问题代码
```go
func (u *Manager) queryRacing(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
    // 从 Manager 配置中获取参数
    raceDelay := time.Duration(u.racingDelayMs) * time.Millisecond  // 默认 100ms
    maxConcurrent := u.racingMaxConcurrent

    logger.Debugf("[queryRacing] 竞速参数: 延迟=%v, 最大并发=%d", raceDelay, maxConcurrent)

    sortedServers := u.getSortedHealthyServers()
    if len(sortedServers) == 0 {
        sortedServers = u.servers
    }

    if len(sortedServers) > maxConcurrent {
        sortedServers = sortedServers[:maxConcurrent]
    }

    // 创建用于接收结果的通道
    resultChan := make(chan *QueryResultWithTTL, 1)
    errorChan := make(chan error, maxConcurrent)

    // 创建可取消的上下文
    raceCtx, cancel := context.WithCancel(ctx)
    defer cancel()

    var activeTasks int
    var mu sync.Mutex

    // 1. 立即向最佳的上游服务器发起查询
    activeTasks = 1
    go func(server *HealthAwareUpstream, index int) {
        logger.Debugf("[queryRacing] 主请求发起: 服务器 %d (%s)", index, server.Address())
        msg := new(dns.Msg)
        msg.SetQuestion(dns.Fqdn(domain), dns.StringToType[dns.TypeToString[qtype]])
        if dnssec && r.IsEdns0() != nil && r.IsEdns0().Do() {
            msg.SetEdns0(4096, true)
        }

        reply, err := server.Exchange(raceCtx, msg)

        if err != nil {
            if u.stats != nil {
                u.stats.IncUpstreamFailure(server.Address())
            }
            select {
            case errorChan <- err:
            case <-raceCtx.Done():
            }
            return
        }

        // 处理查询成功
        if reply.Rcode == dns.RcodeSuccess {
            records, cnames, ttl := extractRecords(reply)
            var ips []string
            for _, r := range records {
                switch rec := r.(type) {
                case *dns.A:
                    ips = append(ips, rec.A.String())
                case *dns.AAAA:
                    ips = append(ips, rec.AAAA.String())
                }
            }

            result := &QueryResultWithTTL{Records: records, IPs: ips, CNAMEs: cnames, TTL: ttl, AuthenticatedData: reply.AuthenticatedData, DnsMsg: reply.Copy()}
            select {
            case resultChan <- result:
                logger.Debugf("[queryRacing] 主请求成功: %s", server.Address())
                server.RecordSuccess()
                if u.stats != nil {
                    u.stats.IncUpstreamSuccess(server.Address())
                }
            case <-raceCtx.Done():
            }
            return
        }

        // 处理 NXDOMAIN - 确定性错误，立即返回
        if reply.Rcode == dns.RcodeNameError {
            // ...
        }
    }(sortedServers[0], 0)

    // 2. 延迟后发起备选竞争请求
    time.Sleep(raceDelay)  // 固定延迟 100ms！

    for i := 1; i < len(sortedServers); i++ {
        mu.Lock()
        activeTasks++
        mu.Unlock()

        go func(server *HealthAwareUpstream, index int) {
            logger.Debugf("[queryRacing] 备选请求发起: 服务器 %d (%s)", index, server.Address())
            // 执行查询...
        }(sortedServers[i], i)
    }

    // 3. 等待第一个成功的结果
    select {
    case result := <-resultChan:
        logger.Debugf("[queryRacing] 竞速查询成功")
        return result, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}
```

### 问题分析

**固定延迟开销场景**
```
假设：
- 最佳服务器响应时间：50ms
- 备选服务器响应时间：100ms
- 竞速延迟：100ms

时间线：
T=0ms:    发起主请求（最佳服务器）
T=50ms:   主请求成功，返回结果
T=100ms:  延迟结束，发起备选请求
T=150ms:  备选请求成功（但已经返回了）

问题：
- 即使主请求在 50ms 成功，客户端也需要等待 100ms 才能发起备选请求
- 这 100ms 的延迟是固定的，无法避免
- 如果主请求失败，需要等待备选请求，总延迟 = 100ms + 100ms = 200ms
```

**性能影响**
- 客户端响应延迟 +100ms
- 如果主请求失败，需要等待备选请求
- 可能浪费备选请求的资源

### 优化方案

**方案 1：动态延迟**
```go
// 根据主请求的响应时间动态调整延迟
func (u *Manager) queryRacingWithDynamicDelay(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
    // 记录主请求的响应时间
    startTime := time.Now()
    
    // 发起主请求...
    reply, err := primaryServer.Exchange(raceCtx, msg)
    
    if err == nil && reply.Rcode == dns.RcodeSuccess {
        // 主请求成功，立即返回，不需要延迟
        return result, nil
    }
    
    // 主请求失败，计算动态延迟
    elapsed := time.Since(startTime)
    dynamicDelay := max(50*time.Millisecond, 100*time.Millisecond - elapsed)
    
    // 延迟后发起备选请求
    time.Sleep(dynamicDelay)
    
    // 发起备选请求...
}
```

**方案 2：并行尝试（改用 Parallel 策略）**
```go
// 不再使用竞速策略，而是并行尝试
// 这样可以避免固定延迟开销
strategy: "parallel"  // 改为并行
```

**方案 3：缩短竞速延迟**
```go
// 当前配置
racingDelayMs: 100  // 100ms

// 优化后
racingDelayMs: 50   // 50ms

// 这样可以减少固定延迟开销
// 但需要确保备选请求有足够的时间
```

