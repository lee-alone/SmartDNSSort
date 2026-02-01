# Linux 递归卡死问题修复 - 快速参考卡片

## 问题
首次启用 Linux 递归功能时程序卡死

## 根本原因
1. **互斥锁死锁** - `Start()` 持有锁，调用 `startPlatformSpecific()`，它又调用 `Initialize()` 尝试获取同一个锁
2. **预热延迟太短** - Linux 只延迟 1 秒，但 unbound 启动需要 2-3 秒
3. **启动超时太短** - Linux 只有 10 秒，但 unbound 启动可能需要 10+ 秒

## 修复方案

### 修复 1：消除死锁
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

### 修复 2：增加预热延迟
```go
// connection_pool.go
var delay time.Duration
if runtime.GOOS == "windows" {
    delay = 5 * time.Second
} else {
    delay = 3 * time.Second  // 从 1 秒改为 3 秒
}
time.Sleep(delay)
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)  // 从 5 秒改为 10 秒
```

### 修复 3：增加启动超时
```go
// manager_common.go
func (m *Manager) waitForReadyTimeoutLinux() time.Duration {
    return 20 * time.Second  // 从 10 秒改为 20 秒
}
```

## 修改的文件
- `recursor/manager.go`
- `recursor/manager_linux.go`
- `recursor/manager_windows.go`
- `upstream/transport/connection_pool.go`
- `recursor/manager_common.go`

## 验证
```bash
# 编译
go build -o main ./cmd

# 测试
./main
# 在 Web UI 中启用递归功能，应该能成功启动而不卡死
```

## 预期结果
- ✅ 程序不再卡死
- ✅ unbound 进程成功启动
- ✅ DNS 查询正常工作
- ✅ 日志中没有大量错误

## 详细文档
- [LINUX_DEBUG_SUMMARY.md](LINUX_DEBUG_SUMMARY.md) - 完整调试总结
- [LINUX_DEADLOCK_FIX.md](LINUX_DEADLOCK_FIX.md) - 详细技术分析
- [LINUX_FIX_VERIFICATION.md](LINUX_FIX_VERIFICATION.md) - 验证清单
