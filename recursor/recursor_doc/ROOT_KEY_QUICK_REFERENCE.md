# Root.key 管理 - 快速参考

## 核心改动

### 1. 新增 Linux 特定的 root.key 管理

**文件：** `recursor/system_manager_linux.go`

```go
// 确保 root.key 存在（优先 unbound-anchor，fallback 嵌入文件）
func (sm *SystemManager) ensureRootKeyLinux() (string, error)

// 运行 unbound-anchor 命令
func (sm *SystemManager) runUnboundAnchor(rootKeyPath string) error

// 判断是否为临时错误（可以 fallback）
func (sm *SystemManager) isTemporaryAnchorError(err error, output string) bool

// 从嵌入文件中提取 root.key
func (sm *SystemManager) extractEmbeddedRootKey(targetPath string) error
```

### 2. 平台无关的接口

**文件：** `recursor/system_manager.go`

```go
// 公共接口，根据平台调用相应实现
func (sm *SystemManager) ensureRootKey() (string, error)

// 后台更新任务
func (sm *SystemManager) tryUpdateRootKey() error
```

### 3. 启动时确保 root.key 存在

**文件：** `recursor/manager_linux.go`

```go
func (m *Manager) startPlatformSpecificNoInit() error {
    // ...
    // 确保 root.key 存在（Linux 特定）
    if _, err := m.sysManager.ensureRootKey(); err != nil {
        logger.Warnf("[Recursor] Failed to ensure root.key: %v", err)
    }
    // ...
}
```

### 4. 后台定期更新

**文件：** `recursor/manager.go`

```go
// 在 Start() 方法中启动
if runtime.GOOS == "linux" && m.sysManager != nil {
    go m.updateRootKeyInBackground()
}

// 后台更新任务（每 30 天）
func (m *Manager) updateRootKeyInBackground()
```

## 工作流程

### 首次启动（Linux）

```
1. 检查 /etc/unbound/root.key 是否存在
   ├─ 存在且有效 → 使用现有文件
   └─ 不存在或无效 → 继续

2. 尝试 unbound-anchor 生成
   ├─ 成功 → 使用系统生成的 root.key
   └─ 失败 → 检查错误类型

3. 判断是否为临时错误
   ├─ 是（网络问题） → 使用 fallback
   └─ 否（严重错误） → 返回错误

4. 使用嵌入的 root.key
   ├─ 成功 → 启动 Unbound
   └─ 失败 → 启动失败
```

### 后台更新（每 30 天）

```
1. 启动后等待 1 小时
2. 每 30 天尝试更新一次
3. 调用 unbound-anchor 更新
4. 更新失败不影响 DNS 服务
```

## 日志示例

### 成功场景
```
[SystemManager] Using existing root.key: /etc/unbound/root.key
[Recursor] Root key ready
```

### Fallback 场景
```
[SystemManager] Attempting to generate root.key using unbound-anchor...
[SystemManager] unbound-anchor failed, using embedded root.key
[SystemManager] Using embedded root.key as fallback
[Recursor] Root key ready
```

### 后台更新
```
[Recursor] Root key update scheduler started (every 30 days)
[Recursor] Scheduled root.key update...
[SystemManager] Attempting to update root.key...
[SystemManager] Root key updated successfully
```

## 关键参数

| 参数 | 值 | 说明 |
|------|-----|------|
| root.key 路径 | `/etc/unbound/root.key` | Linux 标准位置 |
| 更新间隔 | 30 天 | 每 30 天尝试更新一次 |
| 首次更新延迟 | 1 小时 | 启动后 1 小时开始首次更新 |
| unbound-anchor 参数 | `-a <path> -4` | `-a` 指定输出路径，`-4` 强制 IPv4 |
| 最小文件大小 | 1024 字节 | root.key 有效性检查 |

## 临时错误列表

以下错误被认为是临时性的，会触发 fallback：

- `timeout` - 超时
- `network unreachable` - 网络不可达
- `connection refused` - 连接拒绝
- `resolution failed` - DNS 解析失败
- `no address` - 无法解析地址
- `could not fetch` - 无法获取
- `no such file` - 文件不存在
- `command not found` - 命令不存在

## 测试命令

```bash
# 编译检查
go build -v ./recursor

# 运行所有测试
go test -v ./recursor

# 运行特定测试
go test -v ./recursor -run TestEnsureRootKey
go test -v ./recursor -run TestTryUpdateRootKey

# 需要 root 权限的测试
sudo go test -v ./recursor -run TestEnsureRootKeyLinux
```

## 平台支持

| 平台 | 支持 | 说明 |
|------|------|------|
| Linux | ✅ | 完全支持，可自动下载和更新 |
| Windows | ⚠️ | 仅使用嵌入的 root.key，无法更新 |
| macOS | ❌ | 暂不支持 |

## 故障排查

### 问题：root.key 生成失败

**原因：**
- unbound 未安装
- unbound-anchor 工具不可用
- 网络不可用且嵌入文件缺失

**解决：**
1. 检查 unbound 是否已安装：`which unbound-anchor`
2. 检查网络连接
3. 查看日志中的详细错误信息

### 问题：DNSSEC 验证不工作

**原因：**
- root.key 文件损坏或无效
- root.key 路径配置错误

**解决：**
1. 检查 `/etc/unbound/root.key` 是否存在
2. 检查文件大小是否合理（> 1KB）
3. 手动运行 `unbound-anchor -a /etc/unbound/root.key -4`

### 问题：后台更新任务不运行

**原因：**
- 应用未在 Linux 上运行
- sysManager 未初始化

**解决：**
1. 确认运行在 Linux 系统上
2. 查看启动日志中的 `Root key update scheduler started` 消息

## 相关文档

- [ROOT_KEY_IMPLEMENTATION.md](ROOT_KEY_IMPLEMENTATION.md) - 详细实现文档
- [关于递归root_key的问题.txt](../关于递归root_key的问题.txt) - 原始需求文档
