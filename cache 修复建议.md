# Cache 模块修复建议

> **审计日期**: 2026 年 3 月 23 日  
> **审计范围**: cache 模块 19 个源文件  
> **审计重点**: 幽灵代码、性能风险、安全风险  
> **最后更新**: 2026 年 3 月 23 日 - 所有问题已修复 ✅

---

## 修复优先级总览

| 优先级 | 数量 | 建议修复周期 | 状态 |
|--------|------|--------------|------|
| 🔴 P0 - 紧急修复 | 3 | 立即修复 | ✅ 已完成 |
| 🟡 P1 - 重要优化 | 2 | 1 周内修复 | ✅ 已完成 |
| 🟢 P2 - 建议优化 | 5 | 2 周内修复 | ✅ 已完成 |

**总计**: 10/10 项已修复 (100%)

---

## ✅ P0 - 紧急修复 (已完成)

### 1. 持久化 EOF 判断修复 ✅

**文件**: `cache_persistence.go`

**修复状态**: ✅ 已修复

**修复内容**:
```go
import (
    "errors"
    "io"
    // ...
)

// LoadFromDisk 中
err := decoder.Decode(&entry)
if err != nil {
    if errors.Is(err, io.EOF) {
        break
    }
    return err
}
```

**验证**: 代码已使用 `errors.Is(err, io.EOF)` 替代脆弱的 `err.Error() == "EOF"` 判断。

---

### 2. Records 字段注释更新 ✅

**文件**: `entries.go`

**修复状态**: ✅ 已修复

**修复内容**:
```go
type RawCacheEntry struct {
    Records           []dns.RR  // 通用记录列表 (所有类型的 DNS 记录)
    IPs               []string  // 原始 IP 列表 (Records 中 A/AAAA 记录的物化视图)
    CNAMEs            []string  // CNAME 记录列表 (支持多级 CNAME)
    // ...
}
```

**验证**: 注释已更新，移除了"向后兼容，暂时保持为 nil"的误导性描述，明确说明 `Records` 是通用记录列表，`IPs` 是其物化视图。

---

### 3. CancelSort channel 关闭保护 ✅

**文件**: `cache_sorted.go`

**修复状态**: ✅ 已修复

**修复内容**:
```go
func (c *Cache) CancelSort(domain string, qtype uint16) {
    c.mu.Lock()
    defer c.mu.Unlock()

    key := cacheKey(domain, qtype)
    if state, exists := c.sortingState[key]; exists {
        if state.InProgress {
            state.InProgress = false
        }
        // 安全地关闭 channel，防止重复关闭
        select {
        case <-state.Done:
            // 已经关闭，不做任何事
        default:
            close(state.Done)
        }
        delete(c.sortingState, key)
    }
    c.sortedCache.Delete(key)
}
```

**验证**: 已添加与 `FinishSort` 相同的 channel 关闭保护逻辑，使用 `select+default` 防止重复关闭。

---

## ✅ P1 - 重要优化 (已完成)

### 4. heapChannelFullCount 原子化 ✅

**文件**: `cache.go` 和 `cache_cleanup.go`

**修复状态**: ✅ 已修复

**修复内容**:
```go
// cache.go
type Cache struct {
    // ...
    heapChannelFullCount int64 // channel 满的次数（原子操作）
    // ...
}

// cache_cleanup.go:addToExpiredHeap
func (c *Cache) addToExpiredHeap(key string, expiryTime int64, queryVersion int64) {
    entry := expireEntry{
        key:          key,
        expiry:       expiryTime,
        queryVersion: queryVersion,
    }

    select {
    case c.addHeapChan <- entry:
    default:
        // channel 满，记录监控指标（原子操作，无需锁）
        atomic.AddInt64(&c.heapChannelFullCount, 1)
    }
}
```

**验证**: 
- `heapChannelFullCount` 字段注释已标注为"原子操作"
- `addToExpiredHeap` 方法已使用 `atomic.AddInt64()` 替代全局锁
- 消除了 Set 路径上的锁竞争

---

### 5. pending 计数器竞态修复 ✅

**文件**: `sharded_lru.go`

**修复状态**: ✅ 已修复

**修复内容**:
```go
// ShardedLRUShard 结构体
type ShardedLRUShard struct {
    mu       sync.RWMutex
    capacity int
    cache    map[string]*list.Element
    list     *list.List

    // 异步访问记录
    accessChan chan string
    stopChan   chan struct{}
    wg         sync.WaitGroup
    // pending 计数器已移除
}

// processAccessRecords 异步处理访问记录
func (shard *ShardedLRUShard) processAccessRecords() {
    defer shard.wg.Done()

    for {
        select {
        case key := <-shard.accessChan:
            shard.mu.Lock()
            if elem, exists := shard.cache[key]; exists {
                shard.list.MoveToFront(elem)
            }
            shard.mu.Unlock()
        // ...
        }
    }
}

// recordAccess 记录访问
func (shard *ShardedLRUShard) recordAccess(key string) {
    select {
    case shard.accessChan <- key:
    default:
        // channel 满，丢弃此次记录
    }
}
```

**验证**: 
- `pending` 计数器已彻底移除
- `processAccessRecords` 不再使用 `atomic.AddInt32(&shard.pending, -1)`
- `recordAccess` 不再使用 `atomic.AddInt32(&shard.pending, 1)`
- 消除了竞态条件和负值风险

---

## ✅ P2 - 建议优化 (已完成)

### 6. cleanAdBlockCaches 调用保护 ✅

**文件**: `adblock_cache.go`

**修复状态**: ✅ 已修复

**修复内容**:
```go
// cleanAdBlockCaches 清理过期的 AdBlock 缓存
// ⚠️ 调用此方法前必须持有 c.mu 锁！
// 此方法由 CleanExpired 在持有锁的情况下调用，不要在其他地方直接调用
// 调用链：CleanExpired -> cleanAuxiliaryCaches -> cleanAdBlockCaches
func (c *Cache) cleanAdBlockCaches() {
    // 清理拦截缓存
    for key, entry := range c.blockedCache {
        if entry.IsExpired() {
            delete(c.blockedCache, key)
        }
    }
    // 清理白名单缓存
    for key, entry := range c.allowedCache {
        if entry.IsExpired() {
            delete(c.allowedCache, key)
        }
    }
}
```

**验证**: 已添加明确的锁保护注释，说明调用链和注意事项。

---

### 7. 合并重复 IP 提取逻辑 ✅

**文件**: `cache_raw.go`

**修复状态**: ✅ 已修复

**修复内容**:
```go
// extractIPsFromRecords 从 DNS 记录中提取 A/AAAA 记录的 IP 字符串（去重）
// 这是一个公共函数，用于消除 SetRawRecordsWithDNSSEC 和 SetRawRecordsWithDNSSECAndVersion 中的重复逻辑
func extractIPsFromRecords(records []dns.RR) []string {
    ipSet := make(map[string]bool)
    var ips []string
    for _, r := range records {
        var ipStr string
        switch rec := r.(type) {
        case *dns.A:
            ipStr = rec.A.String()
        case *dns.AAAA:
            ipStr = rec.AAAA.String()
        default:
            continue
        }
        if !ipSet[ipStr] {
            ipSet[ipStr] = true
            ips = append(ips, ipStr)
        }
    }
    return ips
}
```

**验证**: 
- 公共函数 `extractIPsFromRecords` 已创建
- `SetRawRecordsWithDNSSEC` 和 `SetRawRecordsWithDNSSECAndVersion` 已使用该函数
- 消除了代码重复

---

### 8. gracePeriod 持久化支持 ✅

**文件**: `entries.go` 和 `cache_persistence.go`

**修复状态**: ✅ 已修复

**修复内容**:
```go
// entries.go:PersistentCacheEntry
type PersistentCacheEntry struct {
    Domain          string   `json:"domain"`
    QType           uint16   `json:"qtype"`
    IPs             []string `json:"ips"`
    CNAME           string   `json:"cname,omitempty"`
    CNAMEs          []string `json:"cnames,omitempty"`
    AcquisitionTime int64    `json:"acquisition_time"`
    EffectiveTTL    uint32   `json:"effective_ttl"`
    GracePeriod     uint32   `json:"grace_period,omitempty"` // 新增
}

// cache_persistence.go:SaveToDisk
persistentEntry := PersistentCacheEntry{
    // ...
    GracePeriod: entry.gracePeriod,
}

// cache_persistence.go:LoadFromDisk
cacheEntry := &RawCacheEntry{
    // ...
    gracePeriod: entry.GracePeriod,
}
```

**验证**: 
- `PersistentCacheEntry` 已添加 `GracePeriod` 字段
- `SaveToDisk` 和 `LoadFromDisk` 已正确处理该字段

---

### 9. 添加持久化校验和 ✅

**文件**: `cache_persistence.go`

**修复状态**: ✅ 已修复

**修复内容**:
```go
import "hash/crc32"

// 校验和错误
var ErrChecksumMismatch = errors.New("cache file checksum mismatch")

// cacheFileFooter 持久化文件尾（校验和）
type cacheFileFooter struct {
    Checksum uint32 // CRC32 校验和
    Count    uint64 // 条目数量
}

// SaveToDisk 中
checksum := crc32.NewIEEE()
// ... 计算校验和
footer := cacheFileFooter{
    Checksum: checksum.Sum32(),
    Count:    entryCount,
}

// LoadFromDisk 中
if footer.Checksum != checksum.Sum32() {
    return ErrChecksumMismatch
}
```

**验证**: 
- 已添加 CRC32 校验和计算
- 已添加文件尾结构 `cacheFileFooter`
- 已添加校验和不匹配错误 `ErrChecksumMismatch`

---

### 10. SortQueue 动态缓冲 ✅

**文件**: `sortqueue.go`

**修复状态**: ✅ 已修复

**修复内容**:
```go
// NewSortQueue 创建新的排序队列
// workers: 并发工作线程数
// queueSize: 任务队列缓冲大小（避免阻塞），若 <= 0 则根据 workers 动态计算
// sortTimeout: 单个排序任务的超时时间
func NewSortQueue(workers int, queueSize int, sortTimeout time.Duration) *SortQueue {
    if workers <= 0 {
        workers = 1
    }
    // 动态缓冲：若未指定队列大小，根据 workers 数量动态调整
    // 策略：每个 worker 对应 10 个缓冲槽位，最小 100，最大 1000
    if queueSize <= 0 {
        queueSize = workers * 10
        if queueSize < 100 {
            queueSize = 100
        }
        if queueSize > 1000 {
            queueSize = 1000
        }
    }
    // ...
}
```

**验证**: 
- 已添加动态缓冲逻辑
- 策略：`workers * 10`，最小 100，最大 1000
- 注释已更新说明动态计算规则

---

## 修复检查清单

### ✅ P0 检查项 (已完成)
- [x] 修复 `cache_persistence.go` EOF 判断
- [x] 更新 `Records` 字段注释（移除误导性描述）
- [x] 修复 `CancelSort` channel 关闭保护

### ✅ P1 检查项 (已完成)
- [x] `heapChannelFullCount` 原子化
- [x] `pending` 计数器竞态修复

### ✅ P2 检查项 (已完成)
- [x] `cleanAdBlockCaches` 添加明确注释
- [x] 合并重复 IP 提取逻辑
- [x] `gracePeriod` 持久化支持
- [x] 添加持久化校验和
- [x] `SortQueue` 动态缓冲

---

## 验证测试

修复后请运行以下测试验证：

```bash
# 单元测试
go test ./cache/... -v

# 竞态检测
go test ./cache/... -race -v

# 基准测试
go test ./cache/... -bench=. -benchmem

# 内存分析
go test ./cache/... -bench=. -memprofile=mem.prof
go tool pprof mem.prof
```

---

## 性能回归基准

修复前后对比以下指标：

| 指标 | 修复前 | 修复后 | 目标 |
|------|--------|--------|------|
| 缓存命中率 | - | - | ≥95% |
| P99 查询延迟 | - | - | ≤10ms |
| 清理阻塞时间 | - | - | ≤10ms |
| Channel 满丢弃率 | - | - | ≤1% |
| 内存使用 | - | - | 无增长 |

---

## 附录：相关文件索引

| 文件 | 行数 | 主要问题 | 状态 |
|------|------|----------|------|
| `cache_persistence.go` | 124 | EOF 判断脆弱 | ✅ 已修复 |
| `entries.go` | 22-35 | Records 字段注释误导 | ✅ 已修复 |
| `cache_sorted.go` | 104-117 | CancelSort channel 保护 | ✅ 已修复 |
| `cache.go` | 52 | heapChannelFullCount 需原子化 | ✅ 已修复 |
| `cache_cleanup.go` | 313 | 原子操作 | ✅ 已修复 |
| `sharded_lru.go` | 224-244 | pending 计数器竞态 | ✅ 已修复 |
| `adblock_cache.go` | 97-110 | 锁保护注释需明确 | ✅ 已修复 |
| `cache_raw.go` | 7-24 | 重复 IP 提取逻辑 | ✅ 已修复 |
| `sortqueue.go` | 62-75 | 动态缓冲 | ✅ 已修复 |

---

## 已知测试代码问题

### ⚠️ cleanup_test.go 竞态条件

**文件**: `cleanup_test.go:39`

**问题**: 测试主 goroutine 与 `heapWorker` 异步 goroutine 同时访问 `expiredHeap`

**影响**: 仅影响测试代码，不影响生产代码

**修复建议**:
```go
// 方案 1: 使用原子操作读取堆大小
func (c *Cache) GetExpiredHeapSize() int {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return len(c.expiredHeap)
}

// 测试中使用
if c.GetExpiredHeapSize() != 2 { ... }
```

**状态**: ⚠️ 测试代码问题，建议修复

---

*文档生成时间：2026 年 3 月 23 日*
