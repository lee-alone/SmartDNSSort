# Ping 包 - 实现说明

## 概述

`ping` 包提供了智能 IP 延迟测量和排序功能，用于 DNS 查询结果的优化排序。

## 未使用字段说明

### strategy 字段

**位置**: `Pinger` 结构体

**状态**: 已弃用，保留用于向后兼容

**原因**: 
- 早期版本支持多种 ping 策略（random, parallel, sequential 等）
- 当前实现统一使用 `smartPing` 策略，该策略结合了多种探测方法
- 为了保持 API 兼容性，保留了 `strategy` 字段但不再使用

**当前实现**:
- 所有 ping 操作都使用 `smartPing()` 方法
- `smartPing()` 实现了智能混合探测：
  1. 先测试 TCP 443 端口（HTTPS）
  2. 进行 TLS 握手验证（带 SNI）
  3. 如果失败，尝试 UDP DNS 查询（端口 53）
  4. 可选：测试 TCP 80 端口（HTTP）

**未来改进**:
- 如果需要支持多种策略，可以重新激活此字段
- 当前不建议使用此字段

## 未使用参数说明

### udpDnsPing 中的 ctx 参数

**函数**: `udpDnsPing(ctx context.Context, ip string) int`

**状态**: 已添加文档说明

**原因**:
- 当前实现使用 `net.DialTimeout()` 而不是支持 context 的方法
- 保留 `ctx` 参数以便未来改进（例如支持 context 取消）
- 保持与其他 ping 方法的 API 一致性

**改进方向**:
- 可以使用 `net.Dialer` 的 `DialContext()` 方法来支持 context 取消
- 这样可以在上游取消时立即停止 UDP 查询

## 设计决策

### 为什么保留未使用的字段和参数？

1. **向后兼容性**: 如果外部代码依赖这些字段，移除会导致编译错误
2. **未来扩展**: 这些字段/参数为未来的功能改进预留了空间
3. **API 一致性**: 保持所有 ping 方法的签名一致

### 为什么使用 smartPing 策略？

1. **流量极小**: 每次查询只需 30-500 字节
2. **准确率高**: 结合多种探测方法，能够识别假阳性节点
3. **性能优秀**: 平均响应时间快，支持并发测试
4. **可靠性**: 能够处理各种网络环境和防火墙配置

## 文件结构

| 文件 | 用途 |
|------|------|
| `ping.go` | 核心 Pinger 实现 |
| `ping_test.go` | 单元测试 |
| `PING_NOTES.md` | 本文档 |

## 使用示例

```go
// 创建 Pinger 实例
pinger := NewPinger(
    3,      // count: 每个 IP 测试 3 次
    800,    // timeoutMs: 单次测试超时 800ms
    8,      // concurrency: 并发测试 8 个 IP
    0,      // maxTestIPs: 0 表示测试所有 IP
    3600,   // rttCacheTtlSeconds: 缓存 1 小时
    false,  // enableHttpFallback: 不测试 HTTP
)

// 执行 ping 和排序
results := pinger.PingAndSort(ctx, ips, "example.com")

// 清理资源
defer pinger.Stop()
```

## 诊断信息

### 预期的 linter 警告

- `strategy` 字段未使用 (U1000) - 已弃用，保留用于向后兼容

这是预期的行为，不影响功能。

