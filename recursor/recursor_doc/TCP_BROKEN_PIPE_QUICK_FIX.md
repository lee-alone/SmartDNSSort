# TCP Broken Pipe 问题 - 快速修复指南

## 问题
TCP 连接被 unbound 关闭，导致 "broken pipe" 错误

## 根本原因
1. TCP 连接被远端关闭
2. 连接池仍然尝试复用已关闭的连接
3. 没有连接有效性检查

## 修复方案

### 1. 改进错误分类
```go
// isTemporaryError - broken pipe 是永久错误，不是临时错误
if strings.Contains(errStr, "broken pipe") || 
   strings.Contains(errStr, "connection reset") ||
   strings.Contains(errStr, "EOF") {
    return false // 永久错误
}
```

### 2. 添加连接有效性检查
```go
// 从池中取出连接后检查是否仍然有效
if p.network == "tcp" && p.isConnectionStale(poolConn) {
    poolConn.conn.Close()
    poolConn.closed = true
    return p.Exchange(ctx, msg) // 重新获取
}
```

### 3. TCP broken pipe 自动重试
```go
// 当遇到 broken pipe 时，自动创建新连接并重试
if isBrokenPipe && p.network == "tcp" {
    // 销毁旧连接
    poolConn.conn.Close()
    
    // 创建新连接
    newConn, err := p.createConnection(ctx)
    
    // 重试查询
    reply, err := p.exchangeOnConnection(ctx, newConn, msg)
    
    // 如果成功，返回结果
    return reply, nil
}
```

## 修改的文件
- `upstream/transport/connection_pool.go`

## 验证
```bash
# 编译
go build -o main ./cmd

# 测试 TCP 连接
# 启用 TCP 本地连接，执行 DNS 查询
# 应该能成功，没有 broken pipe 错误
```

## 预期结果
- ✅ 没有 broken pipe 错误
- ✅ 查询自动重试成功
- ✅ 连接正确管理

## 详细文档
- [TCP_BROKEN_PIPE_FIX.md](TCP_BROKEN_PIPE_FIX.md) - 完整技术分析
