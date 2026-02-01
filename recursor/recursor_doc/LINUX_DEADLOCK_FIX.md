# Linux 递归功能卡死问题修复

## 问题诊断

### 症状
- 首次启用 Linux 递归功能时程序卡死
- 大量日志输出：`[WARN] [ConnectionPool] 预热失败: dial failed: dial tcp 127.0.0.1:5353: connect: connection refused`
- 程序无响应

### 根本原因

#### 1. **互斥锁死锁**（主要原因）
在 `manager.go` 的 `Start()` 方法中：
```go
m.mu.Lock()
// ... 检查状态 ...
m.mu.Unlock()

// 调用 startPlatformSpecific()
if err := m.startPlatformSpecific(); err != nil {
    return err
}
```

在 `manager_linux.go` 的 `startPlatformSpecific()` 中：
```go
func (m *Manager) startPlatformSpecific() error {
    // 这里调用 Initialize()，它需要获取 m.mu 锁
    if err := m.Initialize(); err != nil {
        return err
    }
    // ...
}
```

问题：`Start()` 持有 `m.mu` 锁，然后调用 `startPlatformSpecific()`，而 `startPlatformSpecific()` 又尝试在 `Initialize()` 中获取同一个锁，导致**死锁**。

#### 2. **连接池预热时机不当**
- 连接池在启动后立即尝试预热连接到 127.0.0.1:5353
- 但 unbound 进程可能还没完全启动
- 导致大量"连接被拒绝"的错误日志

#### 3. **启动超时过短**
- Linux 上的启动超时只有 10 秒
- 系统 unbound 启动可能需要更长时间

## 解决方案

### 1. 修复互斥锁死锁

**改变流程**：将 `Initialize()` 调用从 `startPlatformSpecific()` 移到 `Start()` 中，在获取锁之前执行。

**修改前**：
```
Start() {
    m.mu.Lock()
    if first_time:
        m.enabled = true
        m.mu.Unlock()
        Initialize()  // 需要锁，但锁已被持有！
        m.mu.Lock()
    m.mu.Unlock()
    
    m.mu.Lock()
    startPlatformSpecific()  // 又调用 Initialize()
    m.mu.Unlock()
}
```

**修改后**：
```
Start() {
    m.mu.Lock()
    if first_time:
        m.enabled = true
        m.mu.Unlock()
        Initialize()  // 在锁外执行
        m.mu.Lock()
        m.installState = StateInstalled
        m.mu.Unlock()
    else:
        m.mu.Unlock()
    
    m.mu.Lock()
    startPlatformSpecificNoInit()  // 不再调用 Initialize()
    m.mu.Unlock()
}
```

### 2. 改进连接池预热

**修改**：
- 增加预热延迟：Windows 5 秒，Linux 3 秒
- 改进 Warmup 日志：预热失败不输出警告，只在调试模式输出
- 增加预热超时：从 5 秒改为 10 秒

```go
// 根据平台调整延迟时间
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
```

### 3. 增加启动超时

**修改**：Linux 启动超时从 10 秒增加到 20 秒

```go
func (m *Manager) waitForReadyTimeoutLinux() time.Duration {
    // Linux 上系统 unbound 启动可能需要更长时间，特别是首次启动时
    return 20 * time.Second
}
```

## 文件修改清单

1. **recursor/manager.go**
   - 修改 `Start()` 方法，调用 `startPlatformSpecificNoInit()` 而不是 `startPlatformSpecific()`

2. **recursor/manager_linux.go**
   - 添加 `startPlatformSpecificNoInit()` 方法（不调用 Initialize）
   - 保留 `startPlatformSpecific()` 以兼容性

3. **recursor/manager_windows.go**
   - 添加 `startPlatformSpecificNoInit()` 方法（不调用 Initialize）
   - 保留 `startPlatformSpecific()` 以兼容性

4. **upstream/transport/connection_pool.go**
   - 增加预热延迟时间
   - 改进 Warmup 日志输出

5. **recursor/manager_common.go**
   - 增加 Linux 启动超时到 20 秒

## 测试建议

1. **单元测试**
   ```bash
   go test -v ./recursor -run TestStart
   ```

2. **集成测试**
   - 启用递归功能
   - 验证 unbound 进程启动成功
   - 验证 DNS 查询正常工作
   - 检查日志中没有死锁或大量错误

3. **性能测试**
   - 测试启动时间
   - 测试连接池预热效果
   - 测试 DNS 查询延迟

## 预期改进

- ✅ 消除互斥锁死锁
- ✅ 减少启动时的错误日志
- ✅ 提高启动成功率
- ✅ 改善用户体验

## 相关文档

- [RECURSOR_IMPLEMENTATION_FINAL_REPORT.md](RECURSOR_IMPLEMENTATION_FINAL_REPORT.md)
- [RECURSOR_BACKEND_IMPLEMENTATION.md](RECURSOR_BACKEND_IMPLEMENTATION.md)
