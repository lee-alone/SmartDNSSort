# 优雅关闭修复 - Graceful Shutdown

## 问题描述

**症状**：关闭服务器时，unbound 进程被停止，但随后立即自动重启

**日志**：
```
[WARN] [Recursor] Process exited unexpectedly. Restart attempt 1/5 after 1s delay...
[INFO] [Recursor] Recursor stopped successfully.
[INFO] [Recursor] Using system unbound: /usr/sbin/unbound
[INFO] [Recursor] Starting unbound: /usr/sbin/unbound -c /etc/unbound/unbound.conf.d/smartdnssort.conf -d
[INFO] [Recursor] Unbound process started (PID: 606)
[INFO] [Recursor] Process restarted successfully
```

## 根本原因

### 问题流程

```
1. 服务器关闭
   ↓
2. Shutdown() 调用 recursorMgr.Stop()
   ↓
3. Stop() 关闭 stopCh 并停止进程
   ↓
4. healthCheckLoop() 检测到进程退出（<-m.exitCh）
   ↓
5. healthCheckLoop() 尝试重启进程
   ↓
6. 新的 unbound 进程启动
```

### 竞态条件

```go
// Stop() 中
close(m.stopCh)  // 关闭停止通道
m.enabled = false // 标记为禁用

// healthCheckLoop() 中
case <-m.exitCh:
    // 进程退出
    m.enabled = false  // 这里才标记为禁用
    m.restartAttempts++
    // 尝试重启
```

**问题**：
- `Stop()` 关闭 `stopCh` 后，`healthCheckLoop()` 可能还没有检测到 `stopCh` 关闭
- 当 `healthCheckLoop()` 收到 `<-m.exitCh` 信号时，它立即尝试重启
- 此时 `m.enabled` 还没有被设置为 `false`

## 解决方案

### 修复 1：提前标记为禁用

**修改 `Stop()` 方法**：
```go
func (m *Manager) Stop() error {
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
    
    // 停止进程
    // ...
}
```

### 修复 2：检查禁用标志

**修改 `healthCheckLoop()` 方法**：
```go
case <-m.exitCh:
    // 进程意外退出
    m.mu.Lock()
    // 检查是否已被禁用（Stop() 调用时会禁用）
    if !m.enabled {
        m.mu.Unlock()
        // 已禁用，不尝试重启
        logger.Debugf("[Recursor] Process exited but recursor is disabled, not restarting")
        return
    }
    
    m.restartAttempts++
    m.lastRestartTime = time.Now()
    attempts := m.restartAttempts
    m.mu.Unlock()
    
    // 尝试重启
    // ...
```

## 工作流程

### 修复前

```
关闭服务器
  ↓
Stop() 关闭 stopCh
  ↓
Stop() 停止进程
  ↓
healthCheckLoop() 收到 exitCh 信号
  ↓
healthCheckLoop() 尝试重启（因为 enabled 还是 true）
  ↓
新的 unbound 进程启动 ❌
```

### 修复后

```
关闭服务器
  ↓
Stop() 标记 enabled = false
  ↓
Stop() 关闭 stopCh
  ↓
Stop() 停止进程
  ↓
healthCheckLoop() 收到 exitCh 信号
  ↓
healthCheckLoop() 检查 enabled 标志
  ↓
enabled = false，不尝试重启 ✅
  ↓
healthCheckLoop() 退出
```

## 修改的文件

| 文件 | 修改内容 |
|------|--------|
| `recursor/manager.go` | 修改 `Stop()` 方法，提前标记为禁用 |
| `recursor/manager.go` | 修改 `healthCheckLoop()` 方法，检查禁用标志 |

## 预期的日志输出

### 修复前

```
[WARN] [Recursor] Process exited unexpectedly. Restart attempt 1/5 after 1s delay...
[INFO] [Recursor] Recursor stopped successfully.
[INFO] [Recursor] Using system unbound: /usr/sbin/unbound
[INFO] [Recursor] Starting unbound: /usr/sbin/unbound -c /etc/unbound/unbound.conf.d/smartdnssort.conf -d
[INFO] [Recursor] Unbound process started (PID: 606)
[INFO] [Recursor] Process restarted successfully
```

### 修复后

```
[INFO] [Recursor] Recursor stopped successfully.
[DEBUG] [Recursor] Process exited but recursor is disabled, not restarting
```

## 验证

### 测试步骤

1. 启动程序
2. 启用递归功能
3. 关闭服务器
4. 观察日志

### 预期结果

- ✅ unbound 进程被停止
- ✅ 没有自动重启
- ✅ 服务器正常关闭
- ✅ 日志中没有 "Process exited unexpectedly" 警告

## 相关代码

### Stop() 方法

```go
func (m *Manager) Stop() error {
    m.mu.Lock()
    
    if !m.enabled {
        m.mu.Unlock()
        return nil
    }
    
    // 立即标记为禁用，防止 healthCheckLoop 尝试重启
    m.enabled = false
    m.mu.Unlock()
    
    // 关闭停止通道
    close(m.stopCh)
    
    // 停止进程
    if m.cmd != nil && m.cmd.Process != nil {
        // 发送 SIGTERM 信号
        if err := m.cmd.Process.Signal(os.Interrupt); err != nil {
            // ...
        }
        
        // 等待进程退出
        select {
        case <-m.exitCh:
            // 进程已退出
        case <-time.After(5 * time.Second):
            if err := m.cmd.Process.Kill(); err != nil {
                // ...
            }
        }
    }
    
    // 清理配置文件
    if m.configPath != "" {
        _ = os.Remove(m.configPath)
    }
    
    logger.Infof("[Recursor] Unbound process stopped")
    return nil
}
```

### healthCheckLoop() 方法

```go
func (m *Manager) healthCheckLoop() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-m.stopCh:
            // 收到停止信号，退出循环
            return
        
        case <-m.exitCh:
            // 进程意外退出
            m.mu.Lock()
            // 检查是否已被禁用（Stop() 调用时会禁用）
            if !m.enabled {
                m.mu.Unlock()
                // 已禁用，不尝试重启
                logger.Debugf("[Recursor] Process exited but recursor is disabled, not restarting")
                return
            }
            
            m.restartAttempts++
            m.lastRestartTime = time.Now()
            attempts := m.restartAttempts
            m.mu.Unlock()
            
            // 检查重启次数是否超过限制
            if attempts > 5 {
                logger.Errorf("[Recursor] Process exited unexpectedly. Max restart attempts (%d) exceeded, giving up", attempts)
                m.mu.Lock()
                m.enabled = false
                m.mu.Unlock()
                return
            }
            
            // 计算指数退避延迟
            backoffDuration := time.Duration(1<<uint(attempts-1)) * time.Second
            logger.Warnf("[Recursor] Process exited unexpectedly. Restart attempt %d/%d after %v delay...",
                attempts, 5, backoffDuration)
            
            // 等待指数退避时间
            select {
            case <-m.stopCh:
                // 在等待期间收到停止信号
                return
            case <-time.After(backoffDuration):
                // 继续重启
            }
            
            // 尝试重启
            if err := m.Start(); err != nil {
                logger.Errorf("[Recursor] Failed to restart (attempt %d): %v", attempts, err)
            } else {
                // 重启成功，重置计数器
                m.mu.Lock()
                m.restartAttempts = 0
                m.mu.Unlock()
                logger.Infof("[Recursor] Process restarted successfully")
                return
            }
        
        case <-ticker.C:
            // 定期端口健康检查
            m.performHealthCheck()
        }
    }
}
```

## 总结

通过以下两个关键修改，解决了关闭服务器时 unbound 进程自动重启的问题：

1. **提前标记为禁用** - 在 `Stop()` 中立即设置 `m.enabled = false`
2. **检查禁用标志** - 在 `healthCheckLoop()` 中检查 `m.enabled` 标志

这确保了当服务器关闭时，unbound 进程不会被自动重启。
