# Root.Key 管理策略 - 完全由 Unbound 管理

## 设计理念

**核心原则**：让专业工具做专业的事

不再由应用层定期更新 root.key，完全由 unbound 的 `auto-trust-anchor-file` 机制管理。

## 为什么这样做

### 1. Root.Key 极少更新

- **更新频率**：通常几年才更新一次
- **上次更新**：2010 年的 KSK rollover
- **实际情况**：基本上是静态的

### 2. Unbound 已有成熟机制

```unbound
auto-trust-anchor-file: "/etc/unbound/root.key"
```

- 自动监控文件变化
- 文件更新时自动重新加载
- 无需应用干预

### 3. 应用层定期更新的问题

❌ 浪费资源 - 每 30 天检查一次（99.9% 的时间都是无用功）
❌ 增加复杂度 - 额外的 goroutine 和定时任务
❌ 不必要的网络请求 - 依赖网络连接
❌ 重复工作 - unbound 已经能做这件事

## 新的管理策略

### 启动时

1. **检查文件是否存在**
   ```go
   exists, err := os.Stat("/etc/unbound/root.key")
   ```

2. **如果不存在，确保文件存在**
   - 优先尝试从嵌入数据解压
   - 如果解压失败，尝试使用 unbound-anchor 生成
   - 如果都失败，使用嵌入的 fallback

3. **配置 unbound**
   ```unbound
   auto-trust-anchor-file: "/etc/unbound/root.key"
   ```

### 运行时

- **无需应用干预**
- unbound 自动监控文件
- 如果文件更新，自动重新加载
- 完全自动化

## 代码变更

### 移除的代码

1. **manager_lifecycle.go**
   - 移除 `updateRootKeyInBackground()` 方法
   - 移除定期更新的 ticker

2. **manager.go**
   - 移除启动定期更新任务的代码
   - 简化启动流程

3. **system_manager.go**
   - 保留 `ensureRootKey()` 用于初始化
   - 移除 `tryUpdateRootKey()` 方法

### 保留的代码

1. **初始化逻辑**
   ```go
   // 启动时确保文件存在
   if _, err := m.sysManager.ensureRootKey(); err != nil {
       logger.Warnf("[Recursor] Failed to ensure root.key: %v", err)
   }
   ```

2. **配置生成**
   ```go
   auto-trust-anchor-file: "/etc/unbound/root.key"
   ```

## 工作流程

```
应用启动
  ↓
确保 root.key 文件存在
  ├─ 如果存在 → 使用现有文件
  └─ 如果不存在 → 从嵌入数据解压或生成
  ↓
启动 unbound
  ↓
unbound 读取 root.key
  ↓
unbound 监控文件变化（auto-trust-anchor-file）
  ↓
如果文件更新 → unbound 自动重新加载
```

## 性能对比

### 旧方案（定期更新）

| 指标 | 值 |
|------|-----|
| Goroutine 数 | +1 |
| 定时检查 | 每 30 天 |
| 网络请求 | 定期 |
| CPU 占用 | 持续监控 |
| 内存占用 | 额外的 ticker |

### 新方案（完全由 unbound 管理）

| 指标 | 值 |
|------|-----|
| Goroutine 数 | 0 |
| 定时检查 | 0 |
| 网络请求 | 0 |
| CPU 占用 | 0 |
| 内存占用 | 0 |

## 可靠性分析

### 初始化失败的处理

```go
// 优先级顺序
1. 使用现有文件（如果存在）
2. 从嵌入数据解压
3. 使用 unbound-anchor 生成
4. 使用嵌入的 fallback
```

### 运行时更新

- unbound 自动处理
- 无需应用参与
- 完全透明

## 配置示例

### Linux

```unbound
server:
    # ... 其他配置 ...
    
    # DNSSEC 信任锚 - 由 unbound 自动管理
    auto-trust-anchor-file: "/etc/unbound/root.key"
```

### Windows

```unbound
server:
    # ... 其他配置 ...
    
    # DNSSEC 信任锚 - 由 unbound 自动管理
    auto-trust-anchor-file: "unbound/root.key"
```

## 常见问题

### Q: 如果 root.key 需要更新怎么办？

A: unbound 会自动处理。如果你想手动更新：
```bash
# Linux
unbound-anchor -a /etc/unbound/root.key

# 或者删除文件，重启应用会重新生成
rm /etc/unbound/root.key
systemctl restart smartdnssort
```

### Q: 如何验证 root.key 是否被正确加载？

A: 查看 unbound 日志：
```bash
# Linux
tail -f /var/log/unbound.log | grep "trust-anchor"

# 或检查文件
ls -la /etc/unbound/root.key
```

### Q: 如果 root.key 文件损坏怎么办？

A: unbound 会记录错误，应用会在启动时重新生成：
```bash
# 删除损坏的文件
rm /etc/unbound/root.key

# 重启应用
systemctl restart smartdnssort
```

## 设计优势

✅ **简洁** - 代码更少，逻辑更清晰
✅ **高效** - 无定时任务，无额外资源占用
✅ **可靠** - 依赖 unbound 的成熟机制
✅ **自动化** - 完全由 unbound 管理
✅ **符合 Unix 哲学** - 让专业工具做专业的事

## 总结

通过完全由 unbound 的 `auto-trust-anchor-file` 管理 root.key，我们：

1. 消除了不必要的定期更新任务
2. 简化了应用代码
3. 降低了资源占用
4. 提高了系统可靠性
5. 遵循了 Unix 设计哲学

这是一个更加优雅、高效、可靠的解决方案。
