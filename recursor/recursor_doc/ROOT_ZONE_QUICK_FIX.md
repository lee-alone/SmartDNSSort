# Root.zone 快速修复指南

## 🔴 必须立即修复的问题

### 问题 1：验证逻辑 Bug（最严重）

**文件**：`recursor/manager_rootzone.go` 第 165-170 行

**当前代码**：
```go
if !strings.Contains(content, "$ORIGIN") && !strings.Contains(content, ".") {
    return fmt.Errorf("invalid root.zone format")
}
```

**问题**：逻辑错误，条件应该用 `||` 而不是 `&&`

**快速修复**：
```go
// 改为：至少包含 $ORIGIN 或 $TTL
if !strings.Contains(content, "$ORIGIN") && !strings.Contains(content, "$TTL") {
    return fmt.Errorf("invalid root.zone format: missing zone file markers")
}

// 添加 SOA 和 NS 记录检查
if !strings.Contains(content, "SOA") {
    return fmt.Errorf("invalid root.zone format: missing SOA record")
}
if !strings.Contains(content, "NS") {
    return fmt.Errorf("invalid root.zone format: missing NS records")
}
```

**影响**：高 - 可能导致无效文件被使用

---

### 问题 2：文件大小检查不足

**文件**：`recursor/manager_rootzone.go` 第 145-155 行

**当前代码**：
```go
func (rm *RootZoneManager) fileExists() (bool, error) {
    _, err := os.Stat(rm.rootZonePath)
    if err == nil {
        return true, nil  // 只检查存在，不检查大小
    }
    // ...
}
```

**快速修复**：
```go
func (rm *RootZoneManager) fileExists() (bool, error) {
    info, err := os.Stat(rm.rootZonePath)
    if err == nil {
        // 检查文件大小（root.zone 通常 > 2MB，最小 100KB）
        if info.Size() < 100*1024 {
            logger.Warnf("[RootZone] root.zone file too small (%d bytes), will re-download", info.Size())
            os.Remove(rm.rootZonePath)
            return false, nil
        }
        return true, nil
    }
    if os.IsNotExist(err) {
        return false, nil
    }
    return false, err
}
```

**影响**：高 - 防止损坏文件被使用

---

### 问题 3：验证文件大小阈值太低

**文件**：`recursor/manager_rootzone.go` 第 172-174 行

**当前代码**：
```go
if len(data) < 1000 {
    return fmt.Errorf("root.zone file too small")
}
```

**快速修复**：
```go
// root.zone 通常 2-3MB，最小应该 100KB
if len(data) < 100*1024 {
    return fmt.Errorf("root.zone file too small: %d bytes (expected >= 100KB)", len(data))
}
// 添加最大值检查
if len(data) > 10*1024*1024 {
    return fmt.Errorf("root.zone file too large: %d bytes (expected <= 10MB)", len(data))
}
```

**影响**：中 - 防止异常大小的文件

---

## 🟡 应该改进的问题

### 问题 4：缺少错误分类

**文件**：`recursor/manager_rootzone.go` 第 120-140 行

**当前代码**：
```go
if err := rm.downloadRootZone(); err != nil {
    return "", false, fmt.Errorf("failed to download root.zone: %w", err)
}
```

**改进方案**：
```go
// 添加方法
func (rm *RootZoneManager) isTemporaryDownloadError(err error) bool {
    if err == nil {
        return false
    }
    errStr := strings.ToLower(err.Error())
    temporaryPatterns := []string{
        "timeout", "connection refused", "connection reset",
        "network unreachable", "temporary failure",
    }
    for _, pattern := range temporaryPatterns {
        if strings.Contains(errStr, pattern) {
            return true
        }
    }
    return false
}

// 在 UpdateRootZonePeriodically 中使用
if err := rm.downloadRootZone(); err != nil {
    if rm.isTemporaryDownloadError(err) {
        logger.Warnf("[RootZone] Temporary error, will retry: %v", err)
    } else {
        logger.Errorf("[RootZone] Permanent error: %v", err)
    }
}
```

**影响**：中 - 提高可靠性

---

### 问题 5：缺少重试机制

**文件**：`recursor/manager_rootzone.go` 第 195-210 行

**当前代码**：
```go
for {
    select {
    case <-stopCh:
        return
    case <-ticker.C:
        _, updated, err := rm.EnsureRootZone()
        if err != nil {
            logger.Errorf("[RootZone] Failed to update root.zone: %v", err)
            continue  // 直接继续，没有重试
        }
    }
}
```

**改进方案**：
```go
const MaxRetries = 3
const RetryDelay = 5 * time.Second

func (rm *RootZoneManager) downloadRootZoneWithRetry() error {
    for attempt := 1; attempt <= MaxRetries; attempt++ {
        if attempt > 1 {
            logger.Infof("[RootZone] Retry attempt %d/%d", attempt, MaxRetries)
            time.Sleep(RetryDelay)
        }
        
        err := rm.downloadRootZone()
        if err == nil {
            return nil
        }
        
        if !rm.isTemporaryDownloadError(err) {
            return err  // 永久错误，不重试
        }
    }
    return fmt.Errorf("failed after %d attempts", MaxRetries)
}

// 在 EnsureRootZone 中使用
if err := rm.downloadRootZoneWithRetry(); err != nil {
    return "", false, fmt.Errorf("failed to download root.zone: %w", err)
}
```

**影响**：中 - 提高成功率

---

### 问题 6：ConfigGenerator 重复创建实例

**文件**：`recursor/config_generator.go` 第 18-24 行

**当前代码**：
```go
func NewConfigGenerator(version string, sysInfo SystemInfo, port int) *ConfigGenerator {
    return &ConfigGenerator{
        version:     version,
        sysInfo:     sysInfo,
        port:        port,
        rootZoneMgr: NewRootZoneManager(),  // 每次都创建新实例
    }
}
```

**改进方案**：
```go
// 不自动创建
func NewConfigGenerator(version string, sysInfo SystemInfo, port int) *ConfigGenerator {
    return &ConfigGenerator{
        version:     version,
        sysInfo:     sysInfo,
        port:        port,
        rootZoneMgr: nil,  // 不自动创建
    }
}

// 添加新方法
func NewConfigGeneratorWithRootZone(version string, sysInfo SystemInfo, port int, rootZoneMgr *RootZoneManager) *ConfigGenerator {
    return &ConfigGenerator{
        version:     version,
        sysInfo:     sysInfo,
        port:        port,
        rootZoneMgr: rootZoneMgr,
    }
}

// 在 Manager.Start() 中使用
if m.rootZoneMgr == nil {
    m.rootZoneMgr = NewRootZoneManager()
}
generator := NewConfigGeneratorWithRootZone(version, sysInfo, m.port, m.rootZoneMgr)
```

**影响**：低 - 优化资源使用

---

## 📋 修复清单

### 第一步：修复验证逻辑（5 分钟）
- [ ] 修改 `validateRootZone()` 方法
- [ ] 修复 `$ORIGIN` 和 `$TTL` 的检查逻辑
- [ ] 添加 SOA 和 NS 记录检查
- [ ] 增加文件大小范围检查（100KB - 10MB）

### 第二步：增强文件检查（5 分钟）
- [ ] 修改 `fileExists()` 方法
- [ ] 添加文件大小检查（最小 100KB）
- [ ] 删除损坏的文件

### 第三步：添加错误分类（10 分钟）
- [ ] 添加 `isTemporaryDownloadError()` 方法
- [ ] 在 `downloadRootZone()` 中使用
- [ ] 在 `UpdateRootZonePeriodically()` 中使用

### 第四步：添加重试机制（10 分钟）
- [ ] 添加 `downloadRootZoneWithRetry()` 方法
- [ ] 修改 `EnsureRootZone()` 使用重试
- [ ] 修改 `UpdateRootZonePeriodically()` 添加失败计数

### 第五步：统一实例管理（10 分钟）
- [ ] 修改 `NewConfigGenerator()` 不自动创建
- [ ] 添加 `NewConfigGeneratorWithRootZone()` 方法
- [ ] 修改 `Manager.Start()` 创建单一实例

**总耗时**：约 40 分钟

---

## 🧪 快速测试

修复后运行以下测试：

```bash
# 编译检查
go build -o /dev/null ./recursor/...

# 运行现有测试
go test ./recursor/... -v

# 手动测试
# 1. 删除 recursor/data/root.zone
# 2. 启动程序，观察日志
# 3. 验证文件是否正确下载
# 4. 检查文件大小是否合理（> 2MB）
# 5. 验证 Unbound 配置是否包含 auth-zone
```

---

## 📊 修复前后对比

| 方面 | 修复前 | 修复后 |
|------|--------|--------|
| 验证逻辑 | 有 bug | ✅ 正确 |
| 文件大小检查 | 无 | ✅ 有 |
| 损坏文件处理 | 无 | ✅ 自动删除 |
| 错误分类 | 无 | ✅ 有 |
| 重试机制 | 无 | ✅ 有 |
| 实例管理 | 多个 | ✅ 单一 |
| 可靠性 | 低 | ✅ 高 |

---

## 🎯 验证修复

修复完成后，验证以下场景：

### 场景 1：首次启动
```
预期：
1. 日志显示 "root.zone not found"
2. 下载 root.zone
3. 验证文件大小 > 100KB
4. 创建 auth-zone 配置
5. Unbound 启动成功
```

### 场景 2：文件已存在
```
预期：
1. 日志显示 "root.zone exists and is up to date"
2. 不重新下载
3. 使用现有文件
```

### 场景 3：文件过期（7 天后）
```
预期：
1. 日志显示 "root.zone is outdated"
2. 下载新版本
3. 验证新文件
4. 原子替换
```

### 场景 4：下载失败（网络错误）
```
预期：
1. 日志显示 "Temporary error"
2. 自动重试 3 次
3. 如果仍失败，使用现有文件
4. 继续运行
```

### 场景 5：文件损坏
```
预期：
1. 日志显示 "root.zone file too small"
2. 自动删除损坏文件
3. 重新下载
```

---

## 💡 提示

1. **修复顺序很重要**：先修复验证逻辑，再添加其他功能
2. **测试很关键**：每个修复后都要测试
3. **日志很有用**：通过日志可以快速定位问题
4. **向后兼容**：修复不应该破坏现有功能

---

## 📞 常见问题

**Q: 修复会影响现有的 root.zone 文件吗？**
A: 不会。修复只是改进验证逻辑，现有的有效文件仍然可以使用。

**Q: 需要删除现有的 root.zone 文件吗？**
A: 不需要。如果文件有效，会继续使用。如果文件损坏，会自动删除并重新下载。

**Q: 修复后需要重新启动吗？**
A: 是的。修复代码后需要重新编译和启动程序。

**Q: 如何验证修复是否成功？**
A: 查看日志输出，验证文件大小，检查 Unbound 配置。

---

## 📚 相关文档

- 详细审核报告：`ROOT_ZONE_CODE_REVIEW.md`
- 改进实现方案：`ROOT_ZONE_IMPROVEMENTS.md`
- 使用指南：`ROOTZONE_GUIDE.md`
- 实现说明：`ROOTZONE_IMPLEMENTATION.md`
