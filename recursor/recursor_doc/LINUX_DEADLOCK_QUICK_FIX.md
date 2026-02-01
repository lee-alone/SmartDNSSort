# Linux 递归卡死问题 - 快速修复指南

## 问题
首次启用 Linux 递归功能时程序卡死，日志显示连接被拒绝。

## 根本原因
1. **互斥锁死锁**：`Start()` 持有锁，然后调用 `startPlatformSpecific()`，而它又尝试在 `Initialize()` 中获取同一个锁
2. **连接池预热时机不当**：unbound 还未启动就尝试预热连接
3. **启动超时过短**：Linux 上只有 10 秒，系统 unbound 启动需要更长时间

## 修复方案

### 核心改变
将 `Initialize()` 从 `startPlatformSpecific()` 移到 `Start()` 中，在获取锁之前执行。

### 具体修改

#### 1. manager.go - Start() 方法
```go
// 首次启用时执行初始化（仅 Linux）
if m.installState == StateNotInstalled && runtime.GOOS == "linux" {
    m.installState = StateInstalling
    m.enabled = true
    m.mu.Unlock()
    
    if err := m.Initialize(); err != nil {  // 在锁外执行
        // ...
    }
    
    m.mu.Lock()
    m.installState = StateInstalled
    m.mu.Unlock()
}

// 调用不包含 Initialize 的平台特定逻辑
if err := m.startPlatformSpecificNoInit(); err != nil {
    return err
}
```

#### 2. manager_linux.go - 新增方法
```go
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

#### 3. connection_pool.go - 增加预热延迟
```go
// Linux: 3 秒延迟，Windows: 5 秒延迟
var delay time.Duration
if runtime.GOOS == "windows" {
    delay = 5 * time.Second
} else {
    delay = 3 * time.Second
}
time.Sleep(delay)

// 预热超时从 5 秒改为 10 秒
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
```

#### 4. manager_common.go - 增加启动超时
```go
func (m *Manager) waitForReadyTimeoutLinux() time.Duration {
    return 20 * time.Second  // 从 10 秒改为 20 秒
}
```

## 验证修复

```bash
# 编译
go build -o main ./cmd

# 测试启用递归
# 在 Web UI 中启用递归功能，应该能成功启动而不卡死
```

## 预期结果

- ✅ 程序不再卡死
- ✅ unbound 进程成功启动
- ✅ 日志中没有大量连接被拒绝的错误
- ✅ DNS 查询正常工作

## 相关文件

- `recursor/manager.go` - 主管理器
- `recursor/manager_linux.go` - Linux 特定逻辑
- `recursor/manager_windows.go` - Windows 特定逻辑
- `upstream/transport/connection_pool.go` - 连接池
- `recursor/manager_common.go` - 通用配置
