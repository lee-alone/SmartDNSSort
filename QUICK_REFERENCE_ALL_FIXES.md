# 递归功能调试 - 快速参考卡片

## 问题 1：Linux 递归卡死

### 症状
- 首次启用 Linux 递归功能时程序卡死
- 大量日志：`[WARN] [ConnectionPool] 预热失败: dial failed: dial tcp 127.0.0.1:5353: connect: connection refused`

### 根本原因
1. **互斥锁死锁** - `Start()` 持有锁，调用 `startPlatformSpecific()`，它又调用 `Initialize()` 尝试获取同一个锁
2. **预热延迟不足** - Linux 只延迟 1 秒，但 unbound 启动需要 2-3 秒
3. **启动超时过短** - Linux 只有 10 秒，但 unbound 启动可能需要 10+ 秒

### 修复
1. 将 `Initialize()` 从 `startPlatformSpecific()` 移到 `Start()` 中，在锁外执行
2. Linux 预热延迟：1 秒 → 3 秒
3. Linux 启动超时：10 秒 → 20 秒

### 修改文件
- `recursor/manager.go`
- `recursor/manager_linux.go`
- `recursor/manager_windows.go`
- `upstream/transport/connection_pool.go`
- `recursor/manager_common.go`

---

## 问题 2：TCP Broken Pipe

### 症状
```
[WARN] [handleQuery] 上游查询失败: write failed: write tcp 127.0.0.1:32912->127.0.0.1:5353: write: broken pipe
[ERROR] [querySequential] 所有服务器都失败
```

### 根本原因
1. TCP 连接被 unbound 关闭
2. 连接池仍然尝试复用已关闭的连接
3. 没有连接有效性检查

### 修复
1. 改进错误分类：broken pipe 是永久错误，不是临时错误
2. 添加连接有效性检查：从池中取出连接时验证其有效性
3. TCP broken pipe 自动重试：遇到 broken pipe 时自动创建新连接并重试

### 修改文件
- `upstream/transport/connection_pool.go`

---

## 验证

```bash
# 编译
go build -o main ./cmd

# 测试
./main
# 在 Web UI 中启用递归功能，应该能成功启动而不卡死
# 执行 DNS 查询，应该能成功，没有 broken pipe 错误
```

---

## 预期结果

- ✅ 程序不再卡死
- ✅ unbound 进程成功启动
- ✅ DNS 查询正常工作
- ✅ 没有大量错误日志
- ✅ TCP 连接稳定可靠

---

## 详细文档

### 问题 1：Linux 递归卡死
- [LINUX_RECURSOR_DEBUG_COMPLETE.md](LINUX_RECURSOR_DEBUG_COMPLETE.md) - 完整修复报告
- [recursor/recursor_doc/LINUX_DEBUG_SUMMARY.md](recursor/recursor_doc/LINUX_DEBUG_SUMMARY.md) - 完整调试总结
- [recursor/recursor_doc/LINUX_DEADLOCK_FIX.md](recursor/recursor_doc/LINUX_DEADLOCK_FIX.md) - 详细技术分析

### 问题 2：TCP Broken Pipe
- [TCP_BROKEN_PIPE_DEBUG_COMPLETE.md](TCP_BROKEN_PIPE_DEBUG_COMPLETE.md) - 完整修复报告
- [recursor/recursor_doc/TCP_BROKEN_PIPE_FIX.md](recursor/recursor_doc/TCP_BROKEN_PIPE_FIX.md) - 完整技术分析

### 总结
- [RECURSOR_DEBUG_COMPLETE_SUMMARY.md](RECURSOR_DEBUG_COMPLETE_SUMMARY.md) - 完整总结
