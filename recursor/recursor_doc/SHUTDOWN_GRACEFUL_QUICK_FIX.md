# 优雅关闭 - 快速修复

## 问题

关闭服务器时，unbound 进程被停止，但随后立即自动重启

## 根本原因

竞态条件：
- `Stop()` 关闭 `stopCh` 并停止进程
- `healthCheckLoop()` 检测到进程退出
- `healthCheckLoop()` 尝试重启（因为 `enabled` 还是 `true`）

## 修复

### 1. 提前标记为禁用

```go
// Stop() 中
m.mu.Lock()
if !m.enabled {
    m.mu.Unlock()
    return nil
}

// 立即标记为禁用，防止 healthCheckLoop 尝试重启
m.enabled = false
m.mu.Unlock()

// 然后关闭停止通道
close(m.stopCh)
```

### 2. 检查禁用标志

```go
// healthCheckLoop() 中
case <-m.exitCh:
    m.mu.Lock()
    // 检查是否已被禁用
    if !m.enabled {
        m.mu.Unlock()
        logger.Debugf("[Recursor] Process exited but recursor is disabled, not restarting")
        return
    }
    
    m.restartAttempts++
    // ...
```

## 修改的文件

- `recursor/manager.go` - `Stop()` 和 `healthCheckLoop()` 方法

## 预期结果

- ✅ unbound 进程被停止
- ✅ 没有自动重启
- ✅ 服务器正常关闭

## 详细文档

- [SHUTDOWN_GRACEFUL_FIX.md](SHUTDOWN_GRACEFUL_FIX.md) - 完整技术分析
