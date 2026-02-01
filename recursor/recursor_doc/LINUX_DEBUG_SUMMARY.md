# Linux 递归功能卡死问题 - 完整调试总结

## 问题描述

**症状**：
- 首次启用 Linux 递归功能时程序卡死
- 大量日志输出：`[WARN] [ConnectionPool] 预热失败: dial failed: dial tcp 127.0.0.1:5353: connect: connection refused`
- 程序无响应，需要强制杀死

**环境**：
- 操作系统：Linux
- 时间：2026/02/01 03:35:19
- 日志显示连接池无法连接到 127.0.0.1:5353

## 根本原因分析

### 原因 1：互斥锁死锁（最严重）

**问题代码流程**：

```
Start() 方法：
├─ m.mu.Lock()  ← 获取锁
├─ if first_time && linux:
│  ├─ m.enabled = true
│  ├─ m.mu.Unlock()  ← 释放锁
│  ├─ Initialize()  ← 在锁外执行（正确）
│  ├─ m.mu.Lock()  ← 重新获取锁
│  └─ m.installState = StateInstalled
├─ m.mu.Unlock()  ← 释放锁
├─ m.mu.Lock()  ← 重新获取锁
├─ startPlatformSpecific()  ← 调用平台特定逻辑
│  └─ Initialize()  ← 尝试获取 m.mu 锁！
│     └─ m.mu.Lock()  ← 死锁！（锁已被 Start() 持有）
└─ m.mu.Unlock()
```

**为什么会死锁**：
1. `Start()` 在调用 `startPlatformSpecific()` 时持有 `m.mu` 锁
2. `startPlatformSpecific()` 内部调用 `Initialize()`
3. `Initialize()` 尝试获取 `m.mu` 锁
4. 同一个 goroutine 尝试两次获取同一个互斥锁 → **死锁**

### 原因 2：连接池预热时机不当

**问题**：
- 连接池在启动后立即尝试预热连接到 127.0.0.1:5353
- 但 unbound 进程可能还没完全启动
- 导致大量"连接被拒绝"的错误

**原始代码**：
```go
// 自动预热 50% 的连接（延迟启动，给 unbound 足够的启动时间）
go func() {
    var delay time.Duration
    if runtime.GOOS == "windows" {
        delay = 3 * time.Second  // ← 太短
    } else {
        delay = 1 * time.Second  // ← 太短！
    }
    time.Sleep(delay)
    // ...
}()
```

**问题**：
- Linux 上只延迟 1 秒，但系统 unbound 启动通常需要 2-3 秒
- 导致预热连接失败

### 原因 3：启动超时过短

**原始代码**：
```go
func (m *Manager) waitForReadyTimeoutLinux() time.Duration {
    return 10 * time.Second  // ← 太短
}
```

**问题**：
- Linux 上系统 unbound 启动可能需要 10+ 秒
- 特别是首次启动时，需要初始化 DNSSEC 信任锚等
- 10 秒的超时可能不够

## 解决方案

### 修复 1：消除互斥锁死锁

**关键改变**：
1. 将 `Initialize()` 从 `startPlatformSpecific()` 移到 `Start()` 中
2. 在 `Start()` 中，`Initialize()` 在锁外执行
3. 创建新方法 `startPlatformSpecificNoInit()`，不调用 `Initialize()`

**修改后的流程**：

```
Start() 方法：
├─ m.mu.Lock()
├─ if first_time && linux:
│  ├─ m.enabled = true
│  ├─ m.mu.Unlock()  ← 释放锁
│  ├─ Initialize()  ← 在锁外执行（获取锁，执行，释放锁）
│  ├─ m.mu.Lock()  ← 重新获取锁
│  └─ m.installState = StateInstalled
├─ m.mu.Unlock()  ← 释放锁
├─ m.mu.Lock()  ← 重新获取锁
├─ startPlatformSpecificNoInit()  ← 不调用 Initialize()
│  └─ 只生成配置文件，不需要锁
├─ m.mu.Unlock()  ← 释放锁
└─ 启动 unbound 进程
```

**代码修改**：

```go
// manager.go - Start() 方法
if err := m.startPlatformSpecificNoInit(); err != nil {
    return err
}

// manager_linux.go - 新增方法
func (m *Manager) startPlatformSpecificNoInit() error {
    // 获取 unbound 路径
    if m.sysManager != nil {
        m.unboundPath = m.sysManager.unboundPath
    }
    
    // 生成配置文件
    configPath, err := m.generateConfigLinux()
    if err != nil {
        return err
    }
    m.configPath = configPath
    
    return nil
}
```

### 修复 2：改进连接池预热

**修改**：
- 增加预热延迟：Windows 5 秒，Linux 3 秒
- 改进 Warmup 日志：预热失败不输出警告，只在调试模式输出
- 增加预热超时：从 5 秒改为 10 秒

```go
// connection_pool.go
var delay time.Duration
if runtime.GOOS == "windows" {
    delay = 5 * time.Second
} else {
    // Linux: 系统 unbound 启动通常需要 2-3 秒
    delay = 3 * time.Second
}
time.Sleep(delay)
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

// Warmup 方法改进
func (p *ConnectionPool) Warmup(ctx context.Context, count int) error {
    successCount := 0
    for i := 0; i < count; i++ {
        conn, err := p.createConnection(ctx)
        if err != nil {
            // 预热失败不输出警告，只在调试模式下输出
            logger.Debugf("[ConnectionPool] 预热连接失败 (尝试 %d/%d): %v", i+1, count, err)
            continue
        }
        // ...
    }
    
    if successCount > 0 {
        logger.Debugf("[ConnectionPool] 预热完成: %s, 成功连接数: %d/%d", p.address, successCount, count)
    } else if count > 0 {
        logger.Warnf("[ConnectionPool] 预热失败: %s, 无法建立任何连接 (可能 unbound 还未启动)", p.address)
    }
    return nil
}
```

### 修复 3：增加启动超时

```go
// manager_common.go
func (m *Manager) waitForReadyTimeoutLinux() time.Duration {
    // Linux 上系统 unbound 启动可能需要更长时间，特别是首次启动时
    return 20 * time.Second  // 从 10 秒改为 20 秒
}
```

## 修改文件清单

| 文件 | 修改内容 |
|------|--------|
| `recursor/manager.go` | 调用 `startPlatformSpecificNoInit()` 而不是 `startPlatformSpecific()` |
| `recursor/manager_linux.go` | 添加 `startPlatformSpecificNoInit()` 方法 |
| `recursor/manager_windows.go` | 添加 `startPlatformSpecificNoInit()` 方法 |
| `upstream/transport/connection_pool.go` | 增加预热延迟，改进日志 |
| `recursor/manager_common.go` | 增加 Linux 启动超时到 20 秒 |

## 验证修复

### 编译检查
```bash
go build -o main ./cmd
# 应该编译成功，无错误
```

### 功能测试
```bash
# 1. 启动程序
./main

# 2. 在 Web UI 中启用递归功能
# 应该能成功启动而不卡死

# 3. 检查日志
# 应该看到：
# - [Recursor] Unbound process started (PID: xxx)
# - [Recursor] Unbound is ready and listening on port 5353
# - 没有大量的"连接被拒绝"错误

# 4. 测试 DNS 查询
dig @127.0.0.1 -p 5353 example.com
# 应该能正常返回结果
```

## 预期改进

| 问题 | 修复前 | 修复后 |
|------|-------|-------|
| 程序卡死 | ✗ 卡死 | ✓ 正常启动 |
| 互斥锁死锁 | ✗ 存在 | ✓ 消除 |
| 连接被拒绝错误 | ✗ 大量错误 | ✓ 偶尔错误（正常） |
| 启动时间 | ~10 秒 | ~15-20 秒（更稳定） |
| 用户体验 | ✗ 差 | ✓ 好 |

## 相关文档

- [LINUX_DEADLOCK_FIX.md](LINUX_DEADLOCK_FIX.md) - 详细技术分析
- [LINUX_DEADLOCK_QUICK_FIX.md](LINUX_DEADLOCK_QUICK_FIX.md) - 快速参考
- [RECURSOR_IMPLEMENTATION_FINAL_REPORT.md](RECURSOR_IMPLEMENTATION_FINAL_REPORT.md) - 实现报告

## 后续改进建议

1. **添加单元测试**
   - 测试 `Start()` 方法的并发安全性
   - 测试 `Initialize()` 的锁获取

2. **添加集成测试**
   - 测试完整的启动流程
   - 测试 DNS 查询功能

3. **性能优化**
   - 考虑使用读写锁而不是互斥锁
   - 优化连接池预热策略

4. **监控和告警**
   - 添加启动时间监控
   - 添加连接池健康检查告警
