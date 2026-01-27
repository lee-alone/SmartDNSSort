# 设计更新：参数覆盖机制与策略选择优化

## 📢 重要更新

基于用户反馈，我们对参数消除方案进行了重要更新，引入了**参数覆盖机制**和**策略选择优化**。

---

## 更新内容

### 1. 参数覆盖机制（Override Mechanism）

**原始方案**
- 所有参数都自动计算
- 用户无法手动调整

**更新方案**
- 默认自动计算参数
- 用户可以在配置文件中覆盖任何参数
- 保留极端场景下的手动微调空间

**优势**
- ✅ 99% 的用户享受自动化
- ✅ 1% 的特殊场景可以手动微调
- ✅ 避免"一刀切"的问题
- ✅ 为开发者保留"后门"

### 2. 策略选择优化

**原始方案**
- 系统自动选择查询策略
- 用户无法选择

**更新方案**
- 默认自动选择最优策略
- 用户可以选择特定策略
- 用户选择策略时，参数由系统自动配置

**优势**
- ✅ 降低自动选择的风险
- ✅ 给用户充分的权限
- ✅ 参数仍然由系统自动配置
- ✅ 平衡自动化和灵活性

---

## 配置对比

### 原始方案

```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  # 所有参数都自动计算，用户无法调整
```

### 更新方案

**方案 1：完全自动化（推荐）**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  # 所有参数都自动计算
```

**方案 2：用户选择策略**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  strategy: "parallel"  # 用户选择
  # 其他参数由系统自动配置
```

**方案 3：用户手动微调**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  strategy: "parallel"
  concurrency: 20       # 用户覆盖
  maxConnections: 50    # 用户覆盖
  # 其他参数由系统自动配置
```

**方案 4：完全手动控制**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  strategy: "parallel"
  concurrency: 20
  maxConnections: 50
  sequentialTimeoutMs: 1000
  racingDelayMs: 100
  # 所有参数都由用户指定
```

---

## 配置优先级

```
用户配置 > 自动计算 > 默认值
```

### 示例

**并发数**
```
如果用户配置了 concurrency: 10
  → 使用 10

如果用户未配置 concurrency
  → 自动计算 = max(len(servers), min(20, CPU核心数 * 2))

如果自动计算失败
  → 使用默认值 5
```

**查询策略**
```
如果用户配置了 strategy: "parallel"
  → 使用 parallel

如果用户配置了 strategy: "auto" 或未配置
  → 自动选择 = sequential/racing/parallel（根据服务器数）

如果自动选择失败
  → 使用默认值 "parallel"
```

---

## 参数覆盖机制详解

### 支持覆盖的参数

| 参数 | 类型 | 默认行为 | 覆盖方式 |
|------|------|---------|---------|
| `concurrency` | int | 自动计算 | 在配置文件中指定 |
| `maxConnections` | int | 自动计算 | 在配置文件中指定 |
| `sequentialTimeoutMs` | int | 自动计算 | 在配置文件中指定 |
| `racingDelayMs` | int | 动态计算 | 在配置文件中指定 |
| `strategy` | string | 自动选择 | 在配置文件中指定 |

### 覆盖机制的实现

```go
// 配置结构
type ManagerConfig struct {
    Concurrency           *int    // nil = 自动计算
    MaxConnections        *int    // nil = 自动计算
    SequentialTimeoutMs   *int    // nil = 自动计算
    RacingDelayMs         *int    // nil = 动态计算
    Strategy              string  // "" = 自动选择
}

// 初始化逻辑
func NewManager(config ManagerConfig) *Manager {
    // 优先级 1：用户配置
    if config.Concurrency != nil && *config.Concurrency > 0 {
        concurrency = *config.Concurrency
    } else {
        // 优先级 2：自动计算
        concurrency = max(len(servers), min(20, runtime.NumCPU() * 2))
    }
    
    // 其他参数类似处理...
}
```

---

## 策略选择优化

### 三层策略选择

**层级 1：用户明确指定**
```yaml
strategy: "parallel"  # 用户选择
```

**层级 2：用户选择自动**
```yaml
strategy: "auto"  # 显式选择自动
```

**层级 3：用户未指定**
```yaml
# strategy 不配置
# 系统自动选择
```

### 自动选择逻辑

```go
func selectStrategy(serverCount int) string {
    if serverCount == 1 {
        return "sequential"  // 单服务器用顺序
    } else if serverCount <= 3 {
        return "racing"      // 少数服务器用竞速
    } else {
        return "parallel"    // 多个服务器用并行
    }
}
```

### 用户选择策略时的参数配置

```yaml
# 用户选择策略
upstream:
  strategy: "parallel"

# 系统自动配置参数
# concurrency = max(len(servers), min(20, CPU核心数 * 2))
# maxConnections = max(20, CPU核心数 * 5)
# sequentialTimeoutMs = max(500, timeoutMs / len(servers))
# racingDelayMs = avgLatency / 10（范围 50-200ms）
```

---

## 使用场景

### 场景 1：标准部署（推荐）

**配置**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
```

**说明**
- 所有参数自动计算
- 查询策略自动选择
- 无需用户干预

**适用**：99% 的用户

---

### 场景 2：特殊硬件环境

**配置**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  concurrency: 50  # 高并发环境
  maxConnections: 100
```

**说明**
- 用户根据硬件特性手动调整
- 其他参数仍然自动计算

**适用**：高并发场景、特殊硬件配置

---

### 场景 3：特殊网络环境

**配置**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  strategy: "sequential"  # 网络不稳定，使用顺序
  sequentialTimeoutMs: 2000  # 增加超时
```

**说明**
- 用户选择特定策略
- 用户调整超时参数
- 其他参数自动计算

**适用**：网络不稳定的环境

---

### 场景 4：完全手动控制

**配置**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  strategy: "parallel"
  concurrency: 20
  maxConnections: 50
  sequentialTimeoutMs: 1000
  racingDelayMs: 100
```

**说明**
- 用户完全控制所有参数
- 适合有特殊需求的场景

**适用**：极端特殊场景

---

## 风险评估

### 低风险

- ✅ 参数覆盖机制
- ✅ 用户选择策略
- ✅ 参数验证和日志记录

### 中风险

- ⚠️ 自动选择策略（已通过用户选择降低风险）
- ⚠️ 用户配置错误（已通过验证和日志降低风险）

### 高风险

- ❌ 无

---

## 日志记录

### 参数初始化日志

```
[Manager] 初始化上游管理器
[Manager] 使用用户配置的并发数: 10
[Manager] 使用自动计算的最大连接数: 40 (CPU核心数: 8)
[Manager] 使用自动计算的单次超时: 1000ms (全局超时: 5000ms, 服务器数: 5)
[Manager] 使用用户指定的查询策略: parallel
[Manager] 初始化完成
```

### 参数覆盖日志

```
[Manager] 使用用户配置的并发数: 15
[Manager] 使用用户配置的最大连接数: 50
[Manager] 使用用户指定的查询策略: sequential
```

---

## 验证方案

### 1. 参数验证

```go
func validateConfig(config ManagerConfig) error {
    if config.Concurrency != nil && *config.Concurrency < 1 {
        return fmt.Errorf("concurrency must be >= 1, got %d", *config.Concurrency)
    }
    
    if config.MaxConnections != nil && *config.MaxConnections < 1 {
        return fmt.Errorf("maxConnections must be >= 1, got %d", *config.MaxConnections)
    }
    
    return nil
}
```

### 2. 策略验证

```go
func validateStrategy(strategy string) error {
    validStrategies := map[string]bool{
        "auto":       true,
        "sequential": true,
        "parallel":   true,
        "racing":     true,
        "random":     true,
    }
    
    if strategy != "" && !validStrategies[strategy] {
        return fmt.Errorf("invalid strategy: %s", strategy)
    }
    
    return nil
}
```

### 3. 性能监控

```go
func (m *Manager) monitorPerformance() {
    // 监控响应时间
    if m.avgLatency > 1*time.Second {
        logger.Warnf("[Manager] 响应时间过长: %v", m.avgLatency)
    }
    
    // 监控错误率
    if m.errorRate > 0.05 {
        logger.Warnf("[Manager] 错误率过高: %.2f%%", m.errorRate*100)
    }
    
    // 监控连接池使用率
    if m.connectionPoolUtilization > 0.9 {
        logger.Warnf("[Manager] 连接池使用率过高: %.2f%%", 
            m.connectionPoolUtilization*100)
    }
}
```

---

## 实施步骤

### 第 1 步：实现参数覆盖机制

1. 修改配置结构，使用 `*Type` 表示可选参数
2. 实现覆盖逻辑
3. 添加日志记录
4. 添加验证函数

### 第 2 步：实现策略选择

1. 支持 "auto" 策略选项
2. 实现自动选择逻辑
3. 允许用户选择特定策略
4. 用户选择策略时，参数由系统自动配置

### 第 3 步：测试和验证

1. 测试参数覆盖
2. 测试策略选择
3. 测试性能监控
4. 测试日志记录

### 第 4 步：文档和发布

1. 更新用户文档
2. 更新开发者文档
3. 发布新版本
4. 收集用户反馈

---

## 总结

### 更新的优势

✅ **灵活性**：99% 的用户享受自动化，1% 的用户可以手动微调
✅ **安全性**：有合理的默认值和验证机制
✅ **可观测性**：清晰的日志记录覆盖情况
✅ **可维护性**：易于添加新的覆盖参数
✅ **用户权限**：充分的权限和"后门"机制

### 推荐配置

**标准部署**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
```

**特殊场景**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  strategy: "parallel"  # 用户选择
  concurrency: 20       # 用户微调
```

---

## 相关文档

- [PARAMETER_ELIMINATION_ANALYSIS.md](PARAMETER_ELIMINATION_ANALYSIS.md) - 参数消除分析
- [OVERRIDE_MECHANISM_DESIGN.md](OVERRIDE_MECHANISM_DESIGN.md) - 参数覆盖机制详细设计
- [INTEGRATION_GUIDE.md](INTEGRATION_GUIDE.md) - 集成实施指南

