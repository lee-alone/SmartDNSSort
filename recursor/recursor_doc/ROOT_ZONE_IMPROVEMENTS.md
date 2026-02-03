# Root.zone 改进实现方案

## 概述

本文档提供了 root.zone 代码的具体改进方案，包括代码示例和实现步骤。

---

## 改进方案 1：修复验证逻辑

### 问题
当前的验证逻辑有 bug：
```go
if !strings.Contains(content, "$ORIGIN") && !strings.Contains(content, ".") {
    return fmt.Errorf("invalid root.zone format")
}
```

这个条件要求**同时不包含** `$ORIGIN` 和 `.`，才返回错误。实际上应该是**至少包含其中一个**。

### 改进方案

```go
// validateRootZone 验证root.zone文件的有效性
func (rm *RootZoneManager) validateRootZone(path string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return fmt.Errorf("failed to read root.zone: %w", err)
    }

    content := string(data)
    fileSize := len(data)

    // 1. 检查文件大小
    // root.zone 通常 2-3MB，最小应该 100KB
    const minSize = 100 * 1024 // 100KB
    const maxSize = 10 * 1024 * 1024 // 10MB
    
    if fileSize < minSize {
        return fmt.Errorf("root.zone file too small: %d bytes (expected >= %d bytes)", fileSize, minSize)
    }
    if fileSize > maxSize {
        return fmt.Errorf("root.zone file too large: %d bytes (expected <= %d bytes)", fileSize, maxSize)
    }

    // 2. 检查 zone 文件标记（必须至少有一个）
    hasOrigin := strings.Contains(content, "$ORIGIN")
    hasTTL := strings.Contains(content, "$TTL")
    
    if !hasOrigin && !hasTTL {
        return fmt.Errorf("invalid root.zone format: missing zone file markers ($ORIGIN or $TTL)")
    }

    // 3. 检查 SOA 记录（root.zone 必须有）
    if !strings.Contains(content, "SOA") {
        return fmt.Errorf("invalid root.zone format: missing SOA record")
    }

    // 4. 检查 NS 记录（根域必须有）
    if !strings.Contains(content, "NS") {
        return fmt.Errorf("invalid root.zone format: missing NS records")
    }

    // 5. 检查是否包含根域标记
    if !strings.Contains(content, ".") {
        return fmt.Errorf("invalid root.zone format: missing root domain marker")
    }

    logger.Debugf("[RootZone] Validation passed: file size=%d bytes, has SOA and NS records", fileSize)
    return nil
}
```

### 实现步骤
1. 替换 `validateRootZone()` 方法
2. 运行测试验证逻辑正确性
3. 检查日志输出

---

## 改进方案 2：增强文件存在性检查

### 问题
当前只检查文件是否存在，不检查文件大小和完整性。

### 改进方案

```go
// fileExists 检查root.zone文件是否存在且有效
func (rm *RootZoneManager) fileExists() (bool, error) {
    info, err := os.Stat(rm.rootZonePath)
    if err != nil {
        if os.IsNotExist(err) {
            return false, nil
        }
        return false, fmt.Errorf("failed to stat root.zone: %w", err)
    }

    // 检查文件大小（root.zone 通常 > 2MB）
    const minSize = 100 * 1024 // 100KB
    
    if info.Size() < minSize {
        logger.Warnf("[RootZone] root.zone file too small (%d bytes), will re-download", info.Size())
        // 删除损坏的文件
        if err := os.Remove(rm.rootZonePath); err != nil {
            logger.Warnf("[RootZone] Failed to remove corrupted root.zone: %v", err)
        }
        return false, nil
    }

    // 检查文件是否可读
    if !info.Mode().IsRegular() {
        logger.Warnf("[RootZone] root.zone is not a regular file")
        return false, nil
    }

    return true, nil
}

// isFileValid 检查文件是否有效（可选的额外检查）
func (rm *RootZoneManager) isFileValid() (bool, error) {
    // 快速验证文件内容（不读取整个文件）
    file, err := os.Open(rm.rootZonePath)
    if err != nil {
        return false, err
    }
    defer file.Close()

    // 读取前 1KB 检查格式
    buf := make([]byte, 1024)
    n, err := file.Read(buf)
    if err != nil && err != io.EOF {
        return false, err
    }

    content := string(buf[:n])
    
    // 检查是否包含 zone 文件标记
    if !strings.Contains(content, "$ORIGIN") && !strings.Contains(content, "$TTL") {
        return false, fmt.Errorf("invalid zone file format")
    }

    return true, nil
}
```

### 实现步骤
1. 修改 `fileExists()` 方法
2. 添加 `isFileValid()` 方法（可选）
3. 在 `EnsureRootZone()` 中调用 `isFileValid()` 进行额外检查

---

## 改进方案 3：添加错误分类

### 问题
所有下载错误都被视为相同，无法区分临时错误和永久错误。

### 改进方案

```go
// isTemporaryDownloadError 判断是否是临时性错误
func (rm *RootZoneManager) isTemporaryDownloadError(err error) bool {
    if err == nil {
        return false
    }

    errStr := strings.ToLower(err.Error())
    
    // 临时错误列表
    temporaryPatterns := []string{
        "timeout",
        "connection refused",
        "connection reset",
        "connection timeout",
        "network unreachable",
        "no such host",
        "temporary failure",
        "i/o timeout",
        "broken pipe",
        "connection aborted",
    }

    for _, pattern := range temporaryPatterns {
        if strings.Contains(errStr, pattern) {
            return true
        }
    }

    // 检查 net.Error 接口
    var netErr net.Error
    if errors.As(err, &netErr) {
        return netErr.Temporary()
    }

    return false
}

// isPermanentDownloadError 判断是否是永久性错误
func (rm *RootZoneManager) isPermanentDownloadError(err error) bool {
    if err == nil {
        return false
    }

    errStr := strings.ToLower(err.Error())
    
    // 永久错误列表
    permanentPatterns := []string{
        "http 404",
        "http 403",
        "http 401",
        "not found",
        "forbidden",
        "unauthorized",
        "invalid url",
        "malformed",
    }

    for _, pattern := range permanentPatterns {
        if strings.Contains(errStr, pattern) {
            return true
        }
    }

    return false
}

// downloadRootZone 改进版本，包含错误分类
func (rm *RootZoneManager) downloadRootZone() error {
    tempPath := rm.rootZonePath + ".tmp"

    resp, err := rm.client.Get(RootZoneURL)
    if err != nil {
        if rm.isTemporaryDownloadError(err) {
            return fmt.Errorf("temporary download error: %w", err)
        }
        return fmt.Errorf("permanent download error: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
        if rm.isPermanentDownloadError(fmt.Errorf(errMsg)) {
            return fmt.Errorf("permanent HTTP error: %s", errMsg)
        }
        return fmt.Errorf("temporary HTTP error: %s", errMsg)
    }

    // 检查 Content-Length
    expectedSize := resp.ContentLength
    if expectedSize > 0 && expectedSize < 100000 {
        return fmt.Errorf("permanent error: root.zone size too small: %d bytes", expectedSize)
    }

    // 创建临时文件
    tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
    if err != nil {
        return fmt.Errorf("failed to create temp file: %w", err)
    }

    written, err := io.Copy(tempFile, resp.Body)
    tempFile.Close()

    if err != nil {
        _ = os.Remove(tempPath)
        if rm.isTemporaryDownloadError(err) {
            return fmt.Errorf("temporary write error: %w", err)
        }
        return fmt.Errorf("permanent write error: %w", err)
    }

    // 验证写入大小
    if expectedSize > 0 && written != expectedSize {
        _ = os.Remove(tempPath)
        return fmt.Errorf("permanent error: download incomplete: got %d bytes, expected %d bytes", written, expectedSize)
    }

    // 验证文件内容
    if err := rm.validateRootZone(tempPath); err != nil {
        _ = os.Remove(tempPath)
        return fmt.Errorf("permanent error: validation failed: %w", err)
    }

    // 原子替换
    if err := os.Rename(tempPath, rm.rootZonePath); err != nil {
        _ = os.Remove(tempPath)
        return fmt.Errorf("failed to replace root.zone: %w", err)
    }

    if err := os.Chmod(rm.rootZonePath, 0644); err != nil {
        logger.Warnf("[RootZone] Failed to set permissions on root.zone: %v", err)
    }

    return nil
}
```

### 实现步骤
1. 添加 `isTemporaryDownloadError()` 方法
2. 添加 `isPermanentDownloadError()` 方法
3. 修改 `downloadRootZone()` 方法
4. 在 `UpdateRootZonePeriodically()` 中使用错误分类

---

## 改进方案 4：添加重试机制

### 问题
更新失败时没有重试，导致可能的临时错误导致更新失败。

### 改进方案

```go
// 常量定义
const (
    MaxDownloadRetries = 3
    RetryDelay         = 5 * time.Second
)

// downloadRootZoneWithRetry 带重试的下载
func (rm *RootZoneManager) downloadRootZoneWithRetry() error {
    var lastErr error

    for attempt := 1; attempt <= MaxDownloadRetries; attempt++ {
        if attempt > 1 {
            logger.Infof("[RootZone] Retry attempt %d/%d after %v", attempt, MaxDownloadRetries, RetryDelay)
            time.Sleep(RetryDelay)
        }

        err := rm.downloadRootZone()
        if err == nil {
            return nil
        }

        lastErr = err

        // 如果是临时错误，继续重试
        if rm.isTemporaryDownloadError(err) {
            logger.Warnf("[RootZone] Temporary error on attempt %d: %v", attempt, err)
            continue
        }

        // 永久错误，不重试
        logger.Errorf("[RootZone] Permanent error on attempt %d: %v", attempt, err)
        return err
    }

    return fmt.Errorf("failed after %d attempts: %w", MaxDownloadRetries, lastErr)
}

// EnsureRootZone 改进版本，使用重试
func (rm *RootZoneManager) EnsureRootZone() (string, bool, error) {
    // 检查文件是否存在
    exists, err := rm.fileExists()
    if err != nil {
        return "", false, fmt.Errorf("failed to check root.zone existence: %w", err)
    }

    if !exists {
        logger.Infof("[RootZone] root.zone not found, downloading from %s", RootZoneURL)
        if err := rm.downloadRootZoneWithRetry(); err != nil {
            return "", false, fmt.Errorf("failed to download root.zone: %w", err)
        }
        logger.Infof("[RootZone] root.zone downloaded successfully")
        return rm.rootZonePath, true, nil
    }

    // 文件存在，检查是否需要更新
    shouldUpdate, err := rm.shouldUpdate()
    if err != nil {
        logger.Warnf("[RootZone] Failed to check if root.zone needs update: %v", err)
        return rm.rootZonePath, false, nil
    }

    if !shouldUpdate {
        logger.Debugf("[RootZone] root.zone exists and is up to date")
        return rm.rootZonePath, false, nil
    }

    // 文件需要更新
    logger.Infof("[RootZone] root.zone is outdated, updating...")
    if err := rm.downloadRootZoneWithRetry(); err != nil {
        logger.Warnf("[RootZone] Failed to update root.zone, using existing file: %v", err)
        return rm.rootZonePath, false, nil
    }
    logger.Infof("[RootZone] root.zone updated successfully")
    return rm.rootZonePath, true, nil
}

// UpdateRootZonePeriodically 改进版本，包含重试和失败计数
func (rm *RootZoneManager) UpdateRootZonePeriodically(stopCh <-chan struct{}) {
    ticker := time.NewTicker(RootZoneUpdateInterval)
    defer ticker.Stop()

    logger.Infof("[RootZone] Started periodic root.zone update (interval: %v)", RootZoneUpdateInterval)

    var consecutiveFailures int
    const maxConsecutiveFailures = 3

    for {
        select {
        case <-stopCh:
            logger.Infof("[RootZone] Stopping periodic update")
            return
        case <-ticker.C:
            logger.Debugf("[RootZone] Checking for root.zone update...")
            _, updated, err := rm.EnsureRootZone()

            if err != nil {
                consecutiveFailures++
                logger.Errorf("[RootZone] Failed to update root.zone (failure %d/%d): %v",
                    consecutiveFailures, maxConsecutiveFailures, err)

                if consecutiveFailures >= maxConsecutiveFailures {
                    logger.Warnf("[RootZone] Max consecutive failures reached, will retry next cycle")
                    consecutiveFailures = 0
                }
                continue
            }

            // 更新成功
            consecutiveFailures = 0

            if updated {
                logger.Infof("[RootZone] root.zone updated successfully at %s", time.Now().Format(time.RFC3339))
            } else {
                logger.Debugf("[RootZone] root.zone is already up to date")
            }
        }
    }
}
```

### 实现步骤
1. 添加 `downloadRootZoneWithRetry()` 方法
2. 修改 `EnsureRootZone()` 使用重试
3. 修改 `UpdateRootZonePeriodically()` 添加失败计数

---

## 改进方案 5：统一实例管理

### 问题
ConfigGenerator 中每次都创建新的 RootZoneManager，导致多个实例。

### 改进方案

**修改 config_generator.go**：
```go
// ConfigGenerator 生成 unbound 配置
type ConfigGenerator struct {
    version     string
    sysInfo     SystemInfo
    port        int
    rootZoneMgr *RootZoneManager // 可选的 root.zone 管理器
}

// NewConfigGenerator 创建新的 ConfigGenerator
func NewConfigGenerator(version string, sysInfo SystemInfo, port int) *ConfigGenerator {
    return &ConfigGenerator{
        version:     version,
        sysInfo:     sysInfo,
        port:        port,
        rootZoneMgr: nil, // 不自动创建
    }
}

// NewConfigGeneratorWithRootZone 创建 ConfigGenerator 并指定 RootZoneManager
func NewConfigGeneratorWithRootZone(version string, sysInfo SystemInfo, port int, rootZoneMgr *RootZoneManager) *ConfigGenerator {
    return &ConfigGenerator{
        version:     version,
        sysInfo:     sysInfo,
        port:        port,
        rootZoneMgr: rootZoneMgr,
    }
}

// GenerateConfig 生成配置文件内容
func (cg *ConfigGenerator) GenerateConfig() (string, error) {
    // ... 现有代码 ...

    // 添加root.zone配置（如果可用）
    if cg.rootZoneMgr != nil {
        rootZoneConfig, err := cg.rootZoneMgr.GetRootZoneConfig()
        if err == nil {
            config += rootZoneConfig
        } else {
            logger.Warnf("[Config] Failed to generate root.zone config: %v", err)
        }
    }

    return config, nil
}
```

**修改 manager_linux.go**：
```go
// generateConfigLinux Linux 特定的配置生成
func (m *Manager) generateConfigLinux() (string, error) {
    configPath := "/etc/unbound/unbound.conf.d/smartdnssort.conf"

    // 确保目录存在
    configDir := filepath.Dir(configPath)
    if err := os.MkdirAll(configDir, 0755); err != nil {
        return "", fmt.Errorf("failed to create config directory: %w", err)
    }

    // 获取版本信息
    version := ""
    if m.sysManager != nil {
        version = m.sysManager.unboundVer
    }

    // 使用 ConfigGenerator 生成配置
    sysInfo := SystemInfo{
        CPUCores: runtime.NumCPU(),
        MemoryGB: 0,
    }
    
    // 使用现有的 RootZoneManager 实例
    generator := NewConfigGeneratorWithRootZone(version, sysInfo, m.port, m.rootZoneMgr)
    config, err := generator.GenerateConfig()
    if err != nil {
        return "", fmt.Errorf("failed to generate config: %w", err)
    }

    if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
        return "", fmt.Errorf("failed to write config file: %w", err)
    }

    return configPath, nil
}
```

**修改 manager.go**：
```go
// Start 启动嵌入的 Unbound 进程
func (m *Manager) Start() error {
    m.mu.Lock()

    if m.enabled {
        m.mu.Unlock()
        return fmt.Errorf("recursor already running")
    }

    // ... 现有初始化代码 ...

    m.mu.Unlock()

    // ... 现有启动代码 ...

    // 8. 初始化并管理 root.zone（创建单一实例）
    if m.rootZoneMgr == nil {
        m.rootZoneMgr = NewRootZoneManager()
    }
    
    logger.Infof("[Recursor] Ensuring root.zone file...")
    rootZonePath, isNew, err := m.rootZoneMgr.EnsureRootZone()
    if err != nil {
        logger.Warnf("[Recursor] Failed to ensure root.zone file: %v", err)
    } else {
        if isNew {
            logger.Infof("[Recursor] New root.zone file created: %s", rootZonePath)
        } else {
            logger.Infof("[Recursor] Using existing root.zone file: %s", rootZonePath)
        }

        // 启动定期更新任务
        m.rootZoneStopCh = make(chan struct{})
        go m.rootZoneMgr.UpdateRootZonePeriodically(m.rootZoneStopCh)
    }

    return nil
}
```

### 实现步骤
1. 修改 `NewConfigGenerator()` 不自动创建 RootZoneManager
2. 添加 `NewConfigGeneratorWithRootZone()` 方法
3. 修改 `Manager.Start()` 创建单一实例
4. 修改 `generateConfigLinux()` 使用现有实例

---

## 改进方案 6：改进日志级别

### 问题
所有消息都用 `Infof`，难以区分重要程度。

### 改进方案

```go
// EnsureRootZone 改进版本
func (rm *RootZoneManager) EnsureRootZone() (string, bool, error) {
    exists, err := rm.fileExists()
    if err != nil {
        return "", false, fmt.Errorf("failed to check root.zone existence: %w", err)
    }

    if !exists {
        logger.Infof("[RootZone] root.zone not found, downloading from %s", RootZoneURL)
        if err := rm.downloadRootZoneWithRetry(); err != nil {
            return "", false, fmt.Errorf("failed to download root.zone: %w", err)
        }
        logger.Infof("[RootZone] root.zone downloaded successfully")
        return rm.rootZonePath, true, nil
    }

    shouldUpdate, err := rm.shouldUpdate()
    if err != nil {
        logger.Warnf("[RootZone] Failed to check if root.zone needs update: %v", err)
        return rm.rootZonePath, false, nil
    }

    if !shouldUpdate {
        // 调试信息
        logger.Debugf("[RootZone] root.zone exists and is up to date")
        return rm.rootZonePath, false, nil
    }

    logger.Infof("[RootZone] root.zone is outdated, updating...")
    if err := rm.downloadRootZoneWithRetry(); err != nil {
        logger.Warnf("[RootZone] Failed to update root.zone, using existing file: %v", err)
        return rm.rootZonePath, false, nil
    }
    logger.Infof("[RootZone] root.zone updated successfully")
    return rm.rootZonePath, true, nil
}

// UpdateRootZonePeriodically 改进版本
func (rm *RootZoneManager) UpdateRootZonePeriodically(stopCh <-chan struct{}) {
    ticker := time.NewTicker(RootZoneUpdateInterval)
    defer ticker.Stop()

    logger.Infof("[RootZone] Started periodic root.zone update (interval: %v)", RootZoneUpdateInterval)

    var consecutiveFailures int
    const maxConsecutiveFailures = 3

    for {
        select {
        case <-stopCh:
            logger.Infof("[RootZone] Stopping periodic update")
            return
        case <-ticker.C:
            // 调试信息
            logger.Debugf("[RootZone] Checking for root.zone update...")
            _, updated, err := rm.EnsureRootZone()

            if err != nil {
                consecutiveFailures++
                // 错误信息
                logger.Errorf("[RootZone] Failed to update root.zone (failure %d/%d): %v",
                    consecutiveFailures, maxConsecutiveFailures, err)

                if consecutiveFailures >= maxConsecutiveFailures {
                    // 警告信息
                    logger.Warnf("[RootZone] Max consecutive failures reached, will retry next cycle")
                    consecutiveFailures = 0
                }
                continue
            }

            consecutiveFailures = 0

            if updated {
                // 重要信息
                logger.Infof("[RootZone] root.zone updated successfully at %s", time.Now().Format(time.RFC3339))
            } else {
                // 调试信息
                logger.Debugf("[RootZone] root.zone is already up to date")
            }
        }
    }
}
```

### 日志级别指南
- **Infof**：重要事件（下载成功、更新成功、启动/停止）
- **Warnf**：警告（更新失败但继续使用旧文件、达到最大重试次数）
- **Errorf**：错误（永久性错误、验证失败）
- **Debugf**：调试信息（检查更新、文件已最新）

---

## 实现优先级和时间表

### 第 1 周：高优先级修复
- [ ] 修复验证逻辑（问题 2）
- [ ] 增强文件检查（问题 1）
- [ ] 统一实例管理（问题 5）

### 第 2 周：中优先级改进
- [ ] 添加错误分类（问题 3）
- [ ] 添加重试机制（问题 6）
- [ ] 改进日志级别（问题 7）

### 第 3 周：可选改进
- [ ] 超时控制优化（问题 8）
- [ ] 添加监控指标
- [ ] 完整的单元测试

---

## 测试建议

### 单元测试
```go
func TestValidateRootZone(t *testing.T) {
    // 测试有效的 root.zone
    // 测试太小的文件
    // 测试缺少 SOA 记录
    // 测试缺少 NS 记录
}

func TestIsTemporaryDownloadError(t *testing.T) {
    // 测试临时错误识别
    // 测试永久错误识别
}

func TestDownloadRootZoneWithRetry(t *testing.T) {
    // 测试成功下载
    // 测试临时错误重试
    // 测试永久错误不重试
}
```

### 集成测试
```go
func TestEnsureRootZone(t *testing.T) {
    // 测试首次下载
    // 测试文件已存在
    // 测试文件过期更新
}

func TestUpdateRootZonePeriodically(t *testing.T) {
    // 测试定期更新
    // 测试停止信号
    // 测试失败重试
}
```

---

## 总结

这些改进方案将使 root.zone 管理更加健壮和可靠：

1. **验证逻辑修复**：确保只有有效的文件被使用
2. **文件检查增强**：防止损坏文件被使用
3. **错误分类**：区分临时和永久错误，提高可靠性
4. **重试机制**：提高更新成功率
5. **实例管理**：优化资源使用
6. **日志改进**：提高可观测性

建议按照优先级逐步实施这些改进。
