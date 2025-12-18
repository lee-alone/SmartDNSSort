# Ping 包结构文档

本文档描述了 ping 包在拆分后的组织结构。

## 概述

原始的 `ping.go` 文件（~300+ 行）已被拆分为 6 个专注的文件，每个文件处理特定的功能。

## 文件组织

### 1. `ping.go` (~80 行)
**用途**: 核心结构体定义和公共 API

**关键组件**:
- `Result` 结构体 - ping 结果（IP、RTT、丢包率）
- `rttCacheEntry` 结构体 - 缓存条目
- `Pinger` 结构体 - 主要的 Pinger 类
- `PingAndSort()` - 执行并发 ping 和排序的主方法
- `Stop()` - 停止后台任务

**职责**:
- 定义核心数据结构
- 提供公共 API
- 协调缓存、并发测试和排序

---

### 2. `ping_init.go` (~30 行)
**用途**: 初始化和构造函数

**关键函数**:
- `NewPinger()` - 创建新的 Pinger 实例

**初始化步骤**:
1. 验证和设置默认参数
2. 初始化 RTT 缓存
3. 启动缓存清理器（如果启用）

**职责**:
- 创建 Pinger 实例
- 设置合理的默认值
- 启动后台任务

---

### 3. `ping_probe.go` (~80 行)
**用途**: 探测方法实现

**关键方法**:
- `smartPing()` - 智能混合探测
- `tcpPingPort()` - TCP 端口探测
- `tlsHandshakeWithSNI()` - TLS 握手验证
- `udpDnsPing()` - UDP DNS 查询

**探测顺序**:
1. TCP 443（HTTPS）
2. TLS 握手验证（带 SNI）
3. UDP DNS 查询（端口 53）
4. TCP 80（HTTP，可选）

**职责**:
- 实现各种探测方法
- 提供智能混合探测策略
- 最小化网络流量

---

### 4. `ping_test_methods.go` (~35 行)
**用途**: 单个 IP 测试方法

**关键方法**:
- `pingIP()` - 单个 IP 多次测试

**测试过程**:
1. 执行多次 smartPing 测试
2. 计算平均 RTT
3. 计算丢包率
4. 应用丢包惩罚

**职责**:
- 测试单个 IP
- 计算统计数据
- 应用质量惩罚

---

### 5. `ping_concurrent.go` (~50 行)
**用途**: 并发测试和排序

**关键方法**:
- `concurrentPing()` - 并发测试多个 IP
- `sortResults()` - 排序结果

**并发控制**:
- 使用信号量限制并发数
- 避免资源耗尽
- 支持上下文取消

**排序规则**:
- 综合得分 = RTT + Loss * 18
- 按得分升序排列
- 相同得分按 IP 字典序排列

**职责**:
- 管理并发测试
- 控制资源使用
- 排序结果

---

### 6. `ping_cache.go` (~25 行)
**用途**: 缓存管理

**关键方法**:
- `startRttCacheCleaner()` - 启动缓存清理器

**缓存特性**:
- 自动过期清理
- 线程安全的读写
- 可配置的 TTL

**职责**:
- 管理 RTT 缓存
- 清理过期条目
- 提高性能

---

## 数据流

```
PingAndSort()
    ├─ 检查缓存
    ├─ concurrentPing()
    │   ├─ pingIP() (并发)
    │   │   └─ smartPing() (多次)
    │   │       ├─ tcpPingPort(443)
    │   │       ├─ tlsHandshakeWithSNI()
    │   │       ├─ udpDnsPing()
    │   │       └─ tcpPingPort(80) [可选]
    │   └─ 返回结果
    ├─ 更新缓存
    ├─ sortResults()
    └─ 返回排序结果
```

---

## 线程安全

- **RWMutex (rttCacheMu)**: 保护 RTT 缓存的读写
- **WaitGroup**: 同步并发 goroutine
- **Channel**: 传递结果和控制信号

---

## 依赖关系

- `context` - 上下文支持
- `crypto/tls` - TLS 握手
- `net` - 网络操作
- `sort` - 排序
- `sync` - 同步原语
- `time` - 时间操作

---

## 设计原则

1. **单一职责**: 每个文件处理一个特定的功能
2. **最小化流量**: 智能探测使用最少的网络流量
3. **高准确率**: 多种探测方法提高可靠性
4. **性能优化**: 缓存和并发提高效率
5. **向后兼容**: 保留已弃用的字段以维持 API 兼容性

---

## 迁移说明

拆分保持 100% 向后兼容性。所有现有代码继续工作，无需修改。拆分改进了：

- **可维护性**: 更容易定位和修改特定功能
- **可测试性**: 每个文件可以独立测试
- **可读性**: 更小、专注的文件更容易理解
- **可扩展性**: 新功能可以添加到适当的文件中

---

## 使用示例

```go
// 创建 Pinger
pinger := NewPinger(3, 800, 8, 0, 3600, false)
defer pinger.Stop()

// 执行 ping 和排序
results := pinger.PingAndSort(ctx, ips, "example.com")

// 处理结果
for _, r := range results {
    fmt.Printf("%s: %dms (loss: %.1f%%)\n", r.IP, r.RTT, r.Loss)
}
```

