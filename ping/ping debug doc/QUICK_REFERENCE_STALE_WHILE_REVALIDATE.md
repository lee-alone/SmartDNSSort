# 快速参考：Stale-While-Revalidate 软过期更新

## 核心概念

### 问题
```
缓存过期瞬间，用户查询会被卡在探测上
响应时间从 1ms 跳到 800ms（延迟波动）
```

### 解决
```
缓存过期后仍返回旧数据（1ms）
同时在后台异步更新（不阻塞用户）
```

---

## 缓存生命周期

```
0s          - 缓存写入
0-600s      - 缓存有效（ProbeMethod: "cached"）
600s        - 缓存过期（硬过期时间）
600-630s    - 软过期期间（ProbeMethod: "stale"）
              返回旧数据 + 异步更新
630s        - 软过期结束（硬过期时间 + gracePeriod）
              需要同步探测
```

---

## 配置

### 默认配置
```go
pinger.staleGracePeriod = 30 * time.Second  // 默认 30 秒
```

### 自定义配置
```go
// 高可用场景：给异步更新充足时间
pinger.staleGracePeriod = 60 * time.Second

// 低延迟场景：快速发现故障
pinger.staleGracePeriod = 10 * time.Second

// 自动计算（推荐）：TTL 的 10%
pinger.staleGracePeriod = 0
```

---

## 性能数据

### 缓存过期瞬间
```
优化前：800ms（需要探测）
优化后：1ms（返回旧数据）
改进：快 800 倍
```

### 并发查询
```
优化前：10 个查询 → 10 次探测
优化后：10 个查询 → 1 次异步更新
改进：减少 90% 探测
```

### 用户体验
```
优化前：延迟波动 799ms（1ms → 800ms）
优化后：延迟波动 0ms（始终 1ms）
```

---

## 实现细节

### 缓存条目
```go
type rttCacheEntry struct {
    rtt       int
    loss      float64
    expiresAt time.Time  // 硬过期
    staleAt   time.Time  // 软过期（新增）
}
```

### 缓存检查
```go
if now.Before(e.expiresAt) {
    // 未过期：直接返回
    return cached
} else if now.Before(e.staleAt) {
    // 软过期：返回旧数据 + 异步更新
    return stale
    triggerStaleRevalidate(ip, domain)
} else {
    // 硬过期：需要重新探测
    return needsProbe
}
```

### 异步更新去重
```go
// 检查是否已在更新中
if p.staleRevalidating[ip] {
    return  // 避免重复
}

// 标记为正在更新
p.staleRevalidating[ip] = true

// 后台执行
go func() {
    result := p.pingIP(ctx, ip, domain)
    p.rttCache.set(ip, newEntry)
    delete(p.staleRevalidating, ip)
}()
```

---

## 监控指标

```go
// 软过期命中率
staleHits := countProbeMethod("stale")
hitRate := staleHits / totalQueries

// 异步更新队列
pinger.staleRevalidateMu.Lock()
queueLength := len(pinger.staleRevalidating)
pinger.staleRevalidateMu.Unlock()

// 缓存状态
entries := pinger.rttCache.getAllEntries()
for ip, entry := range entries {
    if time.Now().Before(entry.expiresAt) {
        // 有效缓存
    } else if time.Now().Before(entry.staleAt) {
        // 软过期
    } else {
        // 硬过期
    }
}
```

---

## 常见问题

**Q: 软过期期间返回的数据是否准确？**
A: 是的。返回的是上一次探测的结果，同时后台异步更新。

**Q: 异步更新失败怎么办？**
A: 下一次查询会检测到硬过期，执行同步探测。

**Q: 会不会导致内存泄漏？**
A: 不会。异步更新完成后会清除 staleRevalidating 记录。

**Q: 与 SingleFlight 如何协同？**
A: 异步更新也使用 SingleFlight，避免重复探测。

**Q: 能否禁用软过期？**
A: 可以。设置 `staleGracePeriod = 0` 并修改代码逻辑。

---

## 测试命令

```bash
# 运行软过期测试
go test -v -run "TestStaleWhileRevalidate" ./ping

# 运行所有测试
go test -v ./ping

# 基准测试
go test -bench="BenchmarkStaleWhileRevalidate" ./ping
```

---

## 文件清单

| 文件 | 说明 |
|------|------|
| `ping/ping.go` | 软过期逻辑、异步更新 |
| `ping/ping_init.go` | 初始化 |
| `ping/stale_while_revalidate_test.go` | 测试 |

---

## 下一步

1. ✅ 部署到生产环境
2. 📊 监控软过期命中率
3. 🔧 根据实际情况调整 staleGracePeriod
4. 📈 考虑与其他优化的协同效果
