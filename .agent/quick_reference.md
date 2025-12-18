# 对象池快速参考

## 什么是对象池？

对象池是一种内存优化技术，通过复用对象而不是频繁创建和销毁，来减少内存分配和垃圾回收的开销。

## 为什么需要对象池？

在高 QPS 场景下（10,000+ QPS）：
- 每个查询创建多个 `dns.Msg` 对象
- 频繁的内存分配导致 GC 压力增加
- GC 运行占用 CPU 资源，影响性能

## 对象池的工作原理

```
┌─────────────────────────────────────┐
│         对象池 (sync.Pool)          │
│  ┌─────────────────────────────────┐│
│  │ 可用对象队列                     ││
│  │ [msg1] [msg2] [msg3] ...        ││
│  └─────────────────────────────────┘│
└─────────────────────────────────────┘
         ↑                    ↓
      Get()                 Put()
         ↑                    ↓
    获取对象              放回对象
    (复用)               (重置)
```

## 使用方式

### 基本模式

```go
// 1. 获取对象
msg := s.msgPool.Get()

// 2. 使用对象
msg.SetReply(r)
msg.RecursionAvailable = true

// 3. 发送响应
w.WriteMsg(msg)

// 4. 放回对象
s.msgPool.Put(msg)
```

### 推荐模式（使用 defer）

```go
msg := s.msgPool.Get()
defer s.msgPool.Put(msg)  // 确保异常情况下也能释放

// 处理消息
msg.SetReply(r)
w.WriteMsg(msg)
```

## 关键方法

### Get() - 获取对象

```go
msg := s.msgPool.Get()  // 返回 *dns.Msg
```

- 从池中获取一个对象
- 如果池中没有，自动创建新对象
- 返回的对象是干净的（已重置）

### Put() - 放回对象

```go
s.msgPool.Put(msg)
```

- 将对象放回池中
- 自动重置对象的所有字段（包括容量控制和 EDNS 清理）
- 下次 Get() 时会复用该对象

### Reset() - 手动重置

```go
s.msgPool.Reset(msg)
```

- 手动重置对象的所有字段
- 通常不需要使用（Put() 会自动重置）

## 重置机制

对象池使用智能重置机制确保内存高效：

### 容量控制
- RR 切片（Answer/Ns/Extra）：容量 > 8 时重新分配
- Question 切片：容量 > 4 时重新分配
- 防止切片容量无限增长

### EDNS 清理
- 通过重置 Extra 切片清除 OPT 记录
- 移除 DNSSEC 相关的 EDNS 选项
- 防止跨请求污染

## 修改的文件

### 新增文件
- `cache/msg_pool.go` - 对象池实现
- `cache/msg_pool_test.go` - 单元测试

### 修改的文件
- `dnsserver/server.go` - 添加 msgPool 字段
- `dnsserver/server_init.go` - 初始化对象池
- `dnsserver/handler_query.go` - 7 处修改
- `dnsserver/handler_cache.go` - 3 处修改
- `dnsserver/handler_custom.go` - 1 处修改
- `dnsserver/handler_adblock.go` - 6 处修改
- `dnsserver/utils.go` - 3 个函数修改

## 性能数据

### 预期改进（高 QPS 场景）

| 指标 | 改进 |
|------|------|
| 内存分配 | ↓ 70-80% |
| GC 暂停 | ↓ 30-50% |
| CPU 占用 | ↓ 10-20% |
| 吞吐量 | ↑ 5-15% |

### 测试结果

```
✅ 7/7 单元测试通过
✅ 无编译错误
✅ 无诊断警告
```

## 常见问题

### Q: 对象池是否线程安全？
**A**: 是的。`sync.Pool` 内部使用原子操作，天然支持并发访问。

### Q: 如果忘记 Put() 会怎样？
**A**: 对象不会被复用，但不会导致内存泄漏。GC 会正常回收。

### Q: 可以长期持有从池中获取的对象吗？
**A**: 不建议。应该在使用完毕后立即释放，避免对象被其他 goroutine 复用。

### Q: 对象池的大小可以配置吗？
**A**: 目前不可以。`sync.Pool` 会根据需要自动调整。

### Q: 如何验证对象池是否工作？
**A**: 运行单元测试：`go test -v ./cache -run TestMsgPool`

## 最佳实践

✅ **DO**
- 在使用完毕后立即释放对象
- 使用 defer 确保异常情况下也能释放
- 在高 QPS 场景下使用对象池

❌ **DON'T**
- 长期持有从池中获取的对象
- 在异步操作中使用后忘记释放
- 修改对象后不重置就放回

## 文档

- `MEMORY_OPTIMIZATION.md` - 详细优化文档
- `.agent/memory_optimization_summary.md` - 实现总结
- `.agent/implementation_checklist.md` - 实现清单

## 相关资源

- [Go sync.Pool 文档](https://golang.org/pkg/sync/#Pool)
- [DNS Message 结构](https://github.com/miekg/dns)
- [内存管理最佳实践](https://golang.org/doc/effective_go#allocation_new)
