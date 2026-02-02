# Root.key 管理实现总结

## 概述

实现了 Linux 系统上的 DNSSEC root.key 自动管理机制，支持通过 `unbound-anchor` 工具自动下载和更新，同时提供嵌入式 root.key 作为 fallback。

## 架构设计

### 平台差异

| 方面 | Windows | Linux |
|------|---------|-------|
| **Unbound 来源** | 嵌入的二进制文件 | 系统安装的 unbound |
| **root.key 来源** | 嵌入的文件（固定） | 系统生成 + 嵌入 fallback |
| **root.key 更新** | ❌ 无法更新 | ✅ 可通过 `unbound-anchor` 更新 |
| **配置位置** | 临时目录 | `/etc/unbound/unbound.conf.d/` |

### 工作流程

```
首次启动（Linux）：
├─ 尝试 unbound-anchor 生成 root.key
│   ├─ 成功 → 使用系统生成的 root.key
│   └─ 失败（网络受限）→ 使用嵌入的 root.key
│
└─ 启动 Unbound

运行中（每 30 天）：
├─ 尝试更新 root.key
│   ├─ 成功 → 更新成功，日志记录
│   └─ 失败 → 继续使用旧文件（非致命）
│
└─ DNS 服务继续运行
```

## 实现细节

### 1. 新增文件

#### `recursor/system_manager_linux.go`
Linux 特定的 root.key 管理实现：
- `ensureRootKeyLinux()` - 确保 root.key 存在
- `runUnboundAnchor()` - 运行 unbound-anchor 命令
- `isTemporaryAnchorError()` - 判断是否为临时错误
- `extractEmbeddedRootKey()` - 从嵌入文件中提取 root.key

#### `recursor/system_manager_windows.go`
Windows 特定的实现（简单 stub）：
- 所有方法都返回错误，表示 Windows 不支持此功能

#### `recursor/system_manager_linux_test.go`
Linux 特定的单元测试

#### `recursor/system_manager_rootkey_test.go`
通用的 root.key 管理测试

### 2. 修改的文件

#### `recursor/system_manager.go`
添加了平台无关的接口方法：
- `ensureRootKey()` - 公共接口，根据平台调用相应实现
- `tryUpdateRootKey()` - 后台更新任务

#### `recursor/manager_linux.go`
在 `startPlatformSpecificNoInit()` 中添加：
```go
// 确保 root.key 存在（Linux 特定）
if _, err := m.sysManager.ensureRootKey(); err != nil {
    logger.Warnf("[Recursor] Failed to ensure root.key: %v", err)
    logger.Warnf("[Recursor] DNSSEC validation may be disabled")
}
```

#### `recursor/manager.go`
1. 在 `Start()` 方法中添加后台更新任务启动：
```go
// 启动 root.key 定期更新任务（仅 Linux）
if runtime.GOOS == "linux" && m.sysManager != nil {
    go m.updateRootKeyInBackground()
}
```

2. 添加 `updateRootKeyInBackground()` 方法：
   - 每 30 天尝试更新一次
   - 首次更新在启动后 1 小时
   - 更新失败不影响 DNS 服务

## 关键特性

### 1. 智能 Fallback 机制
- 优先使用 `unbound-anchor` 工具（系统标准做法）
- 网络受限时自动 fallback 到嵌入的 root.key
- 区分临时错误和严重错误

### 2. 临时错误识别
以下错误被认为是临时性的，可以使用 fallback：
- timeout（超时）
- network unreachable（网络不可达）
- connection refused（连接拒绝）
- resolution failed（DNS 解析失败）
- no address（无法解析地址）
- could not fetch（无法获取）
- no such file（文件不存在）
- command not found（命令不存在）

### 3. 后台定期更新
- 每 30 天自动尝试更新一次
- 首次更新在启动后 1 小时（给系统时间稳定网络）
- 更新失败不是致命错误，继续使用现有的 root.key
- 使用 `unbound-anchor -4` 强制 IPv4（在首次启动时很重要）

### 4. 详细日志记录
- 记录 root.key 的来源（system/embedded）
- 记录生成、更新、fallback 的过程
- 便于后续调试和监控

## 使用场景

### 场景 1：首次启动（网络正常）
1. 系统检测到 root.key 不存在
2. 调用 `unbound-anchor -a /etc/unbound/root.key -4`
3. 成功生成 root.key
4. 日志：`[SystemManager] Root key generated successfully`

### 场景 2：首次启动（网络受限）
1. 系统检测到 root.key 不存在
2. 调用 `unbound-anchor` 失败（网络不可达）
3. 识别为临时错误，使用 fallback
4. 从嵌入文件中提取 root.key
5. 日志：`[SystemManager] Using embedded root.key as fallback`

### 场景 3：后台定期更新
1. 启动后 1 小时，启动定期更新任务
2. 每 30 天尝试更新一次
3. 更新成功：`[SystemManager] Root key updated successfully`
4. 更新失败：`[SystemManager] Root key update failed (non-critical): ...`

## 测试

### 单元测试
```bash
go test -v ./recursor -run TestEnsureRootKey
go test -v ./recursor -run TestTryUpdateRootKey
```

### 集成测试（需要 root 权限）
```bash
sudo go test -v ./recursor -run TestEnsureRootKeyLinux
```

## 注意事项

### 1. 权限要求
- Linux 上需要 root 权限才能写入 `/etc/unbound/root.key`
- 建议以 root 身份运行应用

### 2. 网络要求
- 首次启动时需要网络连接（用于下载 root.key）
- 如果网络不可用，会自动使用嵌入的 root.key
- 后台更新任务需要网络连接

### 3. unbound-anchor 工具
- 必须安装 unbound 包（包含 unbound-anchor 工具）
- 如果 unbound-anchor 不可用，会自动 fallback

### 4. DNSSEC 验证
- 如果 root.key 生成失败，DNSSEC 验证可能被禁用
- 应用会继续运行，但 DNSSEC 验证功能受限
- 建议监控日志中的警告信息

## 后续改进建议

1. **监控和告警**
   - 如果使用了嵌入的 root.key，应该在日志中标记为警告级别
   - 可以添加指标收集，记录每次更新的成功/失败状态

2. **配置选项**
   - 允许用户自定义 root.key 路径
   - 允许用户自定义更新间隔

3. **验证机制**
   - 定期验证 root.key 的有效性（检查文件大小、修改时间等）
   - 如果 root.key 损坏，自动重新生成

4. **多平台支持**
   - 考虑在 macOS 上实现类似的机制
   - 支持其他 Linux 发行版的特定路径

## 相关文件

- `recursor/system_manager.go` - 系统管理器基类
- `recursor/system_manager_linux.go` - Linux 特定实现
- `recursor/system_manager_windows.go` - Windows 特定实现
- `recursor/manager_linux.go` - Linux 管理器
- `recursor/manager.go` - 通用管理器
- `recursor/embedded.go` - 嵌入文件管理
