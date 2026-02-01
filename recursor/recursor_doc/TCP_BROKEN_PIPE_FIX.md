# TCP Broken Pipe 问题修复

## 问题描述

**症状**：
```
2026/02/01 03:43:55 [WARN] [handleQuery] 上游查询失败: write failed: write tcp 127.0.0.1:32912->127.0.0.1:5353: write: broken pipe
2026/02/01 03:43:58 [ERROR] [querySequential] 所有服务器都失败
2026/02/01 03:43:58 [WARN] [handleQuery] 上游查询失败: write failed: write tcp 127.0.0.1:32926->127.0.0.1:5353: write: broken pipe
```

**原因**：
- 将本地连接从 UDP 转换为 TCP
- TCP 连接被 unbound 关闭（可能是空闲超时或其他原因）
- 连接池仍然尝试复用已关闭的连接
- 导致 "broken pipe" 错误

## 根本原因分析

### 1. TCP 连接被远端关闭
- unbound 可能有自己的连接超时设置
- 或者 unbound 主动关闭了连接
- 连接池没有及时检测到连接已关闭

### 2. 连接复用不当
- 从池中取出的连接没有验证是否仍然有效
- 直接尝试在已关闭的连接上写入数据
- 导致 "broken pipe" 错误

### 3. 错误处理不足
- "broken pipe" 被当作临时错误处理
- 连接被放回池中，下一个请求仍然会失败
- 导致大量错误日志

## 解决方案

### 修复 1：改进错误分类

**修改**：区分 "broken pipe" 和其他错误

```go
// isTemporaryError 判断是否是临时错误
func (p *ConnectionPool) isTemporaryError(err error) bool {
    // ...
    
    // TCP broken pipe 和 connection reset 是永久错误
    // 这些错误表示连接已被远端关闭，不应该重试
    if strings.Contains(errStr, "broken pipe") || 
       strings.Contains(errStr, "connection reset") ||
       strings.Contains(errStr, "EOF") {
        return false // 永久错误
    }
    
    return false
}
```

### 修复 2：添加连接有效性检查

**修改**：从池中取出连接时验证其有效性

```go
// 从池中取出连接后，检查是否仍然有效
if p.network == "tcp" && p.isConnectionStale(poolConn) {
    logger.Debugf("[ConnectionPool] TCP 连接已过期或被远端关闭，销毁并重新获取: %s", p.address)
    poolConn.conn.Close()
    poolConn.closed = true
    p.mu.Lock()
    p.activeCount--
    p.totalDestroyed++
    p.mu.Unlock()
    return p.Exchange(ctx, msg) // 递归获取下一个
}
```

**实现**：`isConnectionStale` 方法

```go
func (p *ConnectionPool) isConnectionStale(poolConn *PooledConnection) bool {
    // 检查连接空闲时间
    if time.Since(poolConn.lastUsed) > 5*time.Minute {
        return true
    }
    
    // 尝试轻量级读取来检测连接状态
    tcpConn, ok := poolConn.conn.(*net.TCPConn)
    if !ok {
        return false
    }
    
    // 设置 1ms 的读超时进行快速检查
    tcpConn.SetReadDeadline(time.Now().Add(1 * time.Millisecond))
    defer tcpConn.SetReadDeadline(time.Time{})
    
    buf := make([]byte, 1)
    _, err := tcpConn.Read(buf)
    
    // 如果是超时，连接仍然有效
    if ne, ok := err.(net.Error); ok && ne.Timeout() {
        return false
    }
    
    // 其他错误表示连接已关闭
    if err != nil && (strings.Contains(err.Error(), "EOF") ||
                      strings.Contains(err.Error(), "broken pipe")) {
        return true
    }
    
    return false
}
```

### 修复 3：TCP broken pipe 自动重试

**修改**：当遇到 broken pipe 时，自动创建新连接并重试

```go
} else if isBrokenPipe && p.network == "tcp" {
    // TCP broken pipe 特殊处理：关闭连接，然后重试一次
    logger.Warnf("[ConnectionPool] TCP 连接被远端关闭 (broken pipe)，销毁连接并重试: %s", p.address)
    poolConn.conn.Close()
    poolConn.closed = true
    p.mu.Lock()
    p.activeCount--
    p.totalDestroyed++
    p.totalErrors++
    p.mu.Unlock()
    
    // 重试一次：创建新连接并重新执行查询
    p.mu.Lock()
    if p.activeCount < p.maxConnections {
        p.activeCount++
        p.mu.Unlock()
        
        newConn, err := p.createConnection(ctx)
        if err != nil {
            p.mu.Lock()
            p.activeCount--
            p.mu.Unlock()
            return nil, err
        }
        
        // 使用新连接重新执行查询
        reply, err := p.exchangeOnConnection(ctx, newConn, msg)
        if err != nil {
            newConn.conn.Close()
            newConn.closed = true
            p.mu.Lock()
            p.activeCount--
            p.totalDestroyed++
            p.totalErrors++
            p.mu.Unlock()
            return nil, err
        }
        
        // 重试成功，更新延迟并放回连接
        p.updateAvgLatency(time.Since(startTime))
        newConn.lastUsed = time.Now()
        newConn.usageCount++
        select {
        case p.idleConns <- newConn:
            // 成功归还
        default:
            newConn.conn.Close()
            newConn.closed = true
            p.mu.Lock()
            p.activeCount--
            p.totalDestroyed++
            p.mu.Unlock()
        }
        return reply, nil
    }
}
```

## 修改的文件

- `upstream/transport/connection_pool.go`
  - 修改 `isTemporaryError()` 方法
  - 添加 `isConnectionStale()` 方法
  - 改进 `Exchange()` 方法中的连接获取逻辑
  - 改进 `Exchange()` 方法中的错误处理逻辑

## 工作流程

### 修复前
```
1. 从池中取出连接
2. 尝试在连接上写入数据
3. 收到 "broken pipe" 错误
4. 将连接放回池中
5. 下一个请求仍然会失败
```

### 修复后
```
1. 从池中取出连接
2. 检查连接是否仍然有效（isConnectionStale）
3. 如果无效，销毁连接并重新获取
4. 尝试在连接上写入数据
5. 如果收到 "broken pipe" 错误：
   a. 销毁连接
   b. 创建新连接
   c. 重试查询
   d. 如果成功，返回结果
   e. 如果失败，返回错误
```

## 预期改进

| 方面 | 修复前 | 修复后 |
|------|-------|-------|
| Broken pipe 错误 | ✗ 大量错误 | ✓ 自动重试 |
| 连接复用 | ✗ 复用已关闭连接 | ✓ 验证连接有效性 |
| 查询成功率 | ✗ 低 | ✓ 高 |
| 用户体验 | ✗ 差 | ✓ 好 |

## 测试建议

### 基本功能测试
1. 启用 TCP 本地连接
2. 执行 DNS 查询
3. 验证查询成功

### 压力测试
1. 并发 DNS 查询
2. 长时间运行
3. 监控错误日志

### 连接超时测试
1. 让连接空闲超过 5 分钟
2. 执行新查询
3. 验证连接被正确销毁和重建

## 相关文档

- [LINUX_DEBUG_SUMMARY.md](LINUX_DEBUG_SUMMARY.md) - Linux 递归卡死问题修复
- [LINUX_DEADLOCK_FIX.md](LINUX_DEADLOCK_FIX.md) - 详细技术分析
