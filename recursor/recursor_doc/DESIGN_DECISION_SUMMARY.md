# 设计决策总结 - Root.Key 和 Root.Zone 管理

## 概述

通过充分利用 unbound 的成熟机制，完全消除应用层的定期更新任务，实现更加简洁、高效、可靠的系统设计。

## 两个关键决策

### 1. Root.Key 管理

**决策**：完全由 unbound 的 `auto-trust-anchor-file` 管理

**原因**：
- DNSSEC 根密钥极少更新（通常几年才变化一次）
- unbound 已有成熟的自动监控机制
- 应用层定期更新是不必要的重复工作

**实现**：
- 启动时确保文件存在
- 之后完全由 unbound 管理
- 移除所有定期更新逻辑

**代码变更**：
- ❌ 移除 `updateRootKeyInBackground()`
- ❌ 移除 `tryUpdateRootKey()`
- ✅ 保留 `ensureRootKey()` 用于初始化

### 2. Root.Zone 管理

**决策**：由 unbound 通过 `auth-zone` 配置自动从根服务器同步

**原因**：
- unbound 可以配置多个根服务器作为权威数据源
- unbound 会定期检查更新
- 应用层定期下载是不必要的重复工作

**实现**：
```unbound
auth-zone:
    name: "."
    zonefile: "/etc/unbound/root.zone"
    primary: 192.0.32.132      # 根服务器
    primary: 192.0.47.132      # 根服务器
    primary: 2001:500:12::d0d  # 根服务器 IPv6
    primary: 2001:500:1::53    # 根服务器 IPv6
    fallback-enabled: yes      # 网络故障回退
    for-upstream: yes          # 递归查询使用
    for-downstream: no         # 隐私保护
```

**代码变更**：
- ❌ 移除 `UpdateRootZonePeriodically()`
- ✅ 保留 `EnsureRootZone()` 用于初始化
- ✅ 优先从嵌入数据解压，其次从网络下载

## 设计原则

### Unix 哲学

> "让专业工具做专业的事"

- unbound 是 DNS 递归解析的专业工具
- 让 unbound 管理 DNS 相关的数据和更新
- 应用层专注于 DNS 查询和缓存

### 简洁性

**旧方案**：
- 应用层定期检查 root.key（每 30 天）
- 应用层定期检查 root.zone（每 7 天）
- 多个 goroutine 和定时任务
- 复杂的错误处理和重试逻辑

**新方案**：
- 启动时初始化文件
- 之后完全由 unbound 管理
- 无定时任务
- 代码更简洁

### 可靠性

**旧方案**：
- 依赖应用的定时任务
- 网络故障时无法更新
- 应用崩溃时无法更新

**新方案**：
- 依赖 unbound 的成熟机制
- 网络故障时自动回退
- unbound 独立管理，应用崩溃不影响

## 性能对比

### 资源占用

| 资源 | 旧方案 | 新方案 | 改进 |
|------|--------|--------|------|
| Goroutine | +2 | 0 | -100% |
| 定时任务 | 2 个 | 0 | -100% |
| 网络请求 | 定期 | 0 | -100% |
| CPU 占用 | 持续 | 0 | -100% |
| 内存占用 | 较高 | 较低 | ↓ |

### 启动时间

| 阶段 | 旧方案 | 新方案 |
|------|--------|--------|
| 初始化 root.key | 快速 | 快速 |
| 初始化 root.zone | 可能慢（网络） | 快速（嵌入数据） |
| 启动 unbound | 快速 | 快速 |
| 总体 | 较慢 | 更快 |

## 代码变更统计

### 移除的代码

1. **manager_lifecycle.go**
   - `updateRootKeyInBackground()` 方法

2. **manager.go**
   - 启动定期更新任务的代码

3. **system_manager.go**
   - `tryUpdateRootKey()` 方法

### 修改的代码

1. **manager_rootzone.go**
   - 修改 `ensureRootZoneWithRetry()` 优先从嵌入数据解压
   - 添加 `extractEmbeddedRootZone()` 方法

2. **manager_linux.go**
   - 移除 `extractRootZoneLinux()` 调用

3. **config_generator.go**
   - 修改 `GetRootZoneConfig()` 添加根服务器配置

### 保留的代码

1. **初始化逻辑**
   - `ensureRootKey()` - 启动时确保 root.key 存在
   - `EnsureRootZone()` - 启动时确保 root.zone 存在

2. **配置生成**
   - `auto-trust-anchor-file` 配置
   - `auth-zone` 配置

## 工作流程对比

### 旧方案

```
应用启动
  ↓
初始化 root.key
  ├─ 检查文件
  └─ 如果不存在，生成
  ↓
启动定期更新任务（每 30 天）
  ↓
初始化 root.zone
  ├─ 检查文件
  └─ 如果不存在，下载
  ↓
启动定期更新任务（每 7 天）
  ↓
启动 unbound
  ↓
运行时
  ├─ 应用定期检查 root.key 更新
  ├─ 应用定期检查 root.zone 更新
  └─ unbound 处理 DNS 查询
```

### 新方案

```
应用启动
  ↓
初始化 root.key
  ├─ 检查文件
  └─ 如果不存在，从嵌入数据解压
  ↓
初始化 root.zone
  ├─ 检查文件
  └─ 如果不存在，从嵌入数据解压或下载
  ↓
启动 unbound
  ↓
运行时
  ├─ unbound 自动监控 root.key（auto-trust-anchor-file）
  ├─ unbound 自动同步 root.zone（auth-zone primary）
  └─ unbound 处理 DNS 查询
```

## 验证清单

- [x] 移除定期更新任务
- [x] 优先从嵌入数据解压
- [x] 配置 unbound 自动管理
- [x] 无语法错误
- [x] 向后兼容
- [x] 文档完整

## 总结

通过这两个关键决策，我们实现了：

✅ **代码简洁** - 移除了不必要的定时任务
✅ **资源高效** - 无额外的 goroutine 和 CPU 占用
✅ **系统可靠** - 依赖 unbound 的成熟机制
✅ **完全自动化** - 启动后无需应用干预
✅ **符合哲学** - 让专业工具做专业的事

这是一个更加优雅、高效、可靠的系统设计。
