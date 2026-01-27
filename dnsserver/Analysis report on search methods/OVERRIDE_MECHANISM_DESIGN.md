# 参数覆盖机制设计 - 为极端场景保留"后门"

## 概述

本文档说明如何在参数消除的基础上，为开发者保留**覆盖机制（Override）**，以便在极端特殊场景下进行手动微调。

---

## 设计原则

### 1. 默认自动化，允许手动覆盖

**原则**
```
如果用户配置了参数 → 使用用户配置的值
如果用户未配置参数 → 使用自动计算的值
```

**优势**
- ✅ 99% 的用户无需配置，享受自动化
- ✅ 1% 的特殊场景可以手动微调
- ✅ 避免"一刀切"的问题

### 2. 配置优先级

```
用户配置 > 自动计算 > 默认值
```

**示例**
```yaml
# 优先级 1：用户配置（最高）
upstream:
  concurrency: 10

# 优先级 2：自动计算
# 如果用户未配置，系统自动计算
# concurrency = max(len(servers), min(20, CPU核心数 * 2))

# 优先级 3：默认值（最低）
# 如果自动计算失败，使用默认值
# concurrency = 5
```

### 3. 策略选择的特殊处理

**原则**
```
用户选择策略 > 自动选择策略
用户选择策略时，参数由系统自动配置
```

**示例**
```yaml
# 方案 1：用户明确指定策略
upstream:
  strategy: "parallel"  # 用户选择
  # 其他参数由系统自动配置

# 方案 2：用户不指定策略
upstream:
  # strategy 不配置
  # 系统自动选择：sequential/racing/parallel

# 方案 3：用户选择"自动"
upstream:
  strategy: "auto"  # 显式选择自动
  # 系统自动选择最优策略
```

---

## 实现方案

### 方案 1：参数覆盖机制

#### 代码实现

```go
// upstream/manager.go

type ManagerConfig struct {
    // 用户配置的参数（可选）
    Concurrency           *int    // nil 表示未配置，使用自动计算
    MaxConnections        *int
    SequentialTimeoutMs   *int
    RacingDelayMs         *int
    
    // 其他必需参数
    Servers               []Upstream
    TimeoutMs             int
    Strategy              string  // "sequential", "parallel", "racing", "random", "auto"
}

func NewManager(config ManagerConfig) *Manager {
    // 1. 处理并发数
    concurrency := config.Concurrency
    if concurrency == nil || *concurrency <= 0 {
        // 自动计算
        concurrency = ptr(max(len(config.Servers), min(20, runtime.NumCPU() * 2)))
    }
    
    // 2. 处理连接池大小
    maxConnections := config.MaxConnections
    if maxConnections == nil || *maxConnections <= 0 {
        // 自动计算
        maxConnections = ptr(max(20, runtime.NumCPU() * 5))
    }
    
    // 3. 处理单次超时
    sequentialTimeoutMs := config.SequentialTimeoutMs
    if sequentialTimeoutMs == nil || *sequentialTimeoutMs <= 0 {
        // 自动计算
        sequentialTimeoutMs = ptr(max(500, config.TimeoutMs / len(config.Servers)))
    }
    
    // 4. 处理竞速延迟
    racingDelayMs := config.RacingDelayMs
    if racingDelayMs == nil || *racingDelayMs <= 0 {
        // 自动计算（稍后在运行时动态计算）
        racingDelayMs = ptr(100)  // 默认值
    }
    
    // 5. 处理查询策略
    strategy := config.Strategy
    if strategy == "" || strategy == "auto" {
        // 自动选择
        strategy = selectStrategy(len(config.Servers))
    }
    
    return &Manager{
        servers:             config.Servers,
        strategy:            strategy,
        timeoutMs:           config.TimeoutMs,
        concurrency:         *concurrency,
        racingDelayMs:       *racingDelayMs,
        sequentialTimeoutMs: *sequentialTimeoutMs,
        maxConnections:      *maxConnections,
    }
}

// 辅助函数
func ptr(v int) *int {
    return &v
}

func max(a, b int) int {
    if a > b {
        return a
    }
    return b
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}
```

#### 配置文件示例

**场景 1：完全自动化（推荐）**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  # 其他参数都自动计算
```

**场景 2：用户手动微调**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  concurrency: 10  # 用户覆盖
  # 其他参数自动计算
```

**场景 3：用户完全控制**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  concurrency: 10
  maxConnections: 30
  sequentialTimeoutMs: 1000
  racingDelayMs: 50
  # 所有参数都由用户指定
```

---

### 方案 2：策略选择机制

#### 三层策略选择

```go
// upstream/manager.go

type StrategyMode string

const (
    StrategyAuto       StrategyMode = "auto"       // 自动选择（推荐）
    StrategySequential StrategyMode = "sequential" // 用户选择顺序
    StrategyParallel   StrategyMode = "parallel"   // 用户选择并行
    StrategyRacing     StrategyMode = "racing"     // 用户选择竞速
    StrategyRandom     StrategyMode = "random"     // 用户选择随机
)

// 自动选择策略
func selectStrategy(serverCount int) string {
    if serverCount == 1 {
        return "sequential"
    } else if serverCount <= 3 {
        return "racing"
    } else {
        return "parallel"
    }
}

// 用户选择策略
func NewManager(config ManagerConfig) *Manager {
    strategy := config.Strategy
    
    // 如果用户选择 "auto" 或未指定，则自动选择
    if strategy == "" || strategy == "auto" {
        strategy = selectStrategy(len(config.Servers))
        logger.Infof("[Manager] 自动选择查询策略: %s (服务器数: %d)", 
            strategy, len(config.Servers))
    } else {
        logger.Infof("[Manager] 使用用户指定的查询策略: %s", strategy)
    }
    
    // 其他参数由系统自动配置
    // ...
    
    return &Manager{
        strategy: strategy,
        // ...
    }
}
```

#### 配置文件示例

**场景 1：自动选择（推荐）**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  # strategy 不配置或设为 "auto"
  # 系统自动选择最优策略
```

**场景 2：用户选择策略**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  strategy: "parallel"  # 用户选择并行
  # 其他参数由系统自动配置
```

**场景 3：用户选择策略 + 手动微调参数**
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
  strategy: "parallel"  # 用户选择并行
  concurrency: 15       # 用户微调并发数
  # 其他参数由系统自动配置
```

---

## 覆盖机制详细设计

### 1. 并发数覆盖

```go
// 配置结构
type ManagerConfig struct {
    Concurrency *int  // nil = 自动计算，否则使用指定值
}

// 处理逻辑
func (m *Manager) initConcurrency(config ManagerConfig) int {
    // 优先级 1：用户配置
    if config.Concurrency != nil && *config.Concurrency > 0 {
        logger.Infof("[Manager] 使用用户配置的并发数: %d", *config.Concurrency)
        return *config.Concurrency
    }
    
    // 优先级 2：自动计算
    calculated := max(len(m.servers), min(20, runtime.NumCPU() * 2))
    logger.Infof("[Manager] 使用自动计算的并发数: %d (服务器数: %d, CPU核心数: %d)",
        calculated, len(m.servers), runtime.NumCPU())
    return calculated
}
```

### 2. 连接池大小覆盖

```go
// 配置结构
type ConnectionPoolConfig struct {
    MaxConnections *int  // nil = 自动计算，否则使用指定值
}

// 处理逻辑
func (p *ConnectionPool) initMaxConnections(config ConnectionPoolConfig) int {
    // 优先级 1：用户配置
    if config.MaxConnections != nil && *config.MaxConnections > 0 {
        logger.Infof("[ConnectionPool] 使用用户配置的最大连接数: %d", 
            *config.MaxConnections)
        return *config.MaxConnections
    }
    
    // 优先级 2：自动计算
    calculated := max(20, runtime.NumCPU() * 5)
    logger.Infof("[ConnectionPool] 使用自动计算的最大连接数: %d (CPU核心数: %d)",
        calculated, runtime.NumCPU())
    return calculated
}
```

### 3. 单次超时覆盖

```go
// 配置结构
type ManagerConfig struct {
    SequentialTimeoutMs *int  // nil = 自动计算，否则使用指定值
}

// 处理逻辑
func (m *Manager) initSequentialTimeout(config ManagerConfig) int {
    // 优先级 1：用户配置
    if config.SequentialTimeoutMs != nil && *config.SequentialTimeoutMs > 0 {
        logger.Infof("[Manager] 使用用户配置的单次超时: %dms", 
            *config.SequentialTimeoutMs)
        return *config.SequentialTimeoutMs
    }
    
    // 优先级 2：自动计算
    calculated := max(500, m.timeoutMs / len(m.servers))
    logger.Infof("[Manager] 使用自动计算的单次超时: %dms (全局超时: %dms, 服务器数: %d)",
        calculated, m.timeoutMs, len(m.servers))
    return calculated
}
```

### 4. 竞速延迟覆盖

```go
// 配置结构
type ManagerConfig struct {
    RacingDelayMs *int  // nil = 动态计算，否则使用指定值
}

// 处理逻辑
func (m *Manager) getRacingDelay() time.Duration {
    // 优先级 1：用户配置
    if m.racingDelayMs > 0 {
        logger.Debugf("[Manager] 使用用户配置的竞速延迟: %dms", m.racingDelayMs)
        return time.Duration(m.racingDelayMs) * time.Millisecond
    }
    
    // 优先级 2：动态计算
    avgLatency := m.getAverageLatency()
    calculated := avgLatency / 10
    
    // 限制范围
    if calculated < 50*time.Millisecond {
        calculated = 50 * time.Millisecond
    }
    if calculated > 200*time.Millisecond {
        calculated = 200 * time.Millisecond
    }
    
    logger.Debugf("[Manager] 使用动态计算的竞速延迟: %v (平均延迟: %v)",
        calculated, avgLatency)
    return calculated
}
```

### 5. 查询策略覆盖

```go
// 配置结构
type ManagerConfig struct {
    Strategy string  // "auto", "sequential", "parallel", "racing", "random"
}

// 处理逻辑
func (m *Manager) initStrategy(config ManagerConfig) string {
    strategy := config.Strategy
    
    // 优先级 1：用户明确指定策略
    if strategy != "" && strategy != "auto" {
        logger.Infof("[Manager] 使用用户指定的查询策略: %s", strategy)
        return strategy
    }
    
    // 优先级 2：自动选择策略
    selected := selectStrategy(len(m.servers))
    logger.Infof("[Manager] 自动选择查询策略: %s (服务器数: %d)", 
        selected, len(m.servers))
    return selected
}

// 自动选择逻辑
func selectStrategy(serverCount int) string {
    if serverCount == 1 {
        return "sequential"
    } else if serverCount <= 3 {
        return "racing"
    } else {
        return "parallel"
    }
}
```

---

## 配置文件格式

### YAML 格式

```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  
  # 全局超时（必需）
  timeoutMs: 5000
  
  # 查询策略（可选）
  # 可选值：auto, sequential, parallel, racing, random
  # 默认：auto（自动选择）
  strategy: "auto"
  
  # 并发数（可选）
  # 默认：自动计算 = max(len(servers), min(20, CPU核心数 * 2))
  # concurrency: 10
  
  # 最大连接数（可选）
  # 默认：自动计算 = max(20, CPU核心数 * 5)
  # maxConnections: 50
  
  # 单次超时（可选，仅用于顺序查询）
  # 默认：自动计算 = max(500, timeoutMs / len(servers))
  # sequentialTimeoutMs: 1000
  
  # 竞速延迟（可选，仅用于竞速查询）
  # 默认：动态计算 = avgLatency / 10（范围 50-200ms）
  # racingDelayMs: 100
```

### JSON 格式

```json
{
  "upstream": {
    "servers": [
      "8.8.8.8:53",
      "8.8.4.4:53"
    ],
    "timeoutMs": 5000,
    "strategy": "auto",
    "concurrency": null,
    "maxConnections": null,
    "sequentialTimeoutMs": null,
    "racingDelayMs": null
  }
}
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

**适用**
- 99% 的用户

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

**适用**
- 高并发场景
- 特殊硬件配置

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

**适用**
- 网络不稳定的环境
- 需要特定查询策略的场景

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

**适用**
- 极端特殊场景
- 需要精细控制的场景

---

## 日志记录

### 参数初始化日志

```
[Manager] 初始化上游管理器
[Manager] 使用用户配置的并发数: 10
[Manager] 使用自动计算的最大连接数: 40 (CPU核心数: 8)
[Manager] 使用自动计算的单次超时: 1000ms (全局超时: 5000ms, 服务器数: 5)
[Manager] 自动选择查询策略: parallel (服务器数: 5)
[Manager] 初始化完成
```

### 参数覆盖日志

```
[Manager] 使用用户指定的查询策略: sequential
[Manager] 使用用户配置的竞速延迟: 50ms
[Manager] 使用用户配置的并发数: 15
```

---

## 风险管理

### 低风险：参数覆盖

- ✅ 用户可以覆盖任何参数
- ✅ 系统有合理的默认值
- ✅ 日志清晰记录覆盖情况

### 中风险：策略选择

- ⚠️ 用户可以选择任何策略
- ⚠️ 需要验证策略选择的合理性
- ⚠️ 需要监控策略选择的性能

**缓解措施**
1. 提供清晰的策略选择指南
2. 记录策略选择和性能指标
3. 提供性能告警

### 高风险：无

- ❌ 无

---

## 验证方案

### 1. 参数覆盖验证

```go
// 验证用户配置的参数是否合理
func validateConfig(config ManagerConfig) error {
    if config.Concurrency != nil && *config.Concurrency < 1 {
        return fmt.Errorf("concurrency must be >= 1, got %d", *config.Concurrency)
    }
    
    if config.MaxConnections != nil && *config.MaxConnections < 1 {
        return fmt.Errorf("maxConnections must be >= 1, got %d", *config.MaxConnections)
    }
    
    if config.SequentialTimeoutMs != nil && *config.SequentialTimeoutMs < 100 {
        return fmt.Errorf("sequentialTimeoutMs must be >= 100, got %d", 
            *config.SequentialTimeoutMs)
    }
    
    return nil
}
```

### 2. 策略选择验证

```go
// 验证用户选择的策略是否有效
func validateStrategy(strategy string) error {
    validStrategies := map[string]bool{
        "auto":       true,
        "sequential": true,
        "parallel":   true,
        "racing":     true,
        "random":     true,
    }
    
    if !validStrategies[strategy] {
        return fmt.Errorf("invalid strategy: %s", strategy)
    }
    
    return nil
}
```

### 3. 性能监控

```go
// 监控用户配置的参数是否导致性能问题
func (m *Manager) monitorPerformance() {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        // 检查响应时间
        if m.avgLatency > 1*time.Second {
            logger.Warnf("[Manager] 响应时间过长: %v", m.avgLatency)
        }
        
        // 检查错误率
        if m.errorRate > 0.05 {
            logger.Warnf("[Manager] 错误率过高: %.2f%%", m.errorRate*100)
        }
        
        // 检查连接池使用率
        if m.connectionPoolUtilization > 0.9 {
            logger.Warnf("[Manager] 连接池使用率过高: %.2f%%", 
                m.connectionPoolUtilization*100)
        }
    }
}
```

---

## 文档和指南

### 用户指南

**何时使用覆盖机制**
1. 硬件配置特殊（CPU 核心数异常）
2. 网络环境特殊（延迟高、丢包多）
3. 业务需求特殊（需要特定查询策略）
4. 性能调优（基于监控数据的微调）

**何时不使用覆盖机制**
1. 标准部署环境
2. 不确定参数含义
3. 没有性能问题

### 开发者指南

**添加新的覆盖参数**
1. 在配置结构中添加 `*Type` 字段
2. 在初始化函数中添加覆盖逻辑
3. 添加日志记录
4. 添加验证函数
5. 更新文档

**示例**
```go
// 1. 添加配置字段
type ManagerConfig struct {
    NewParam *int  // nil = 自动计算
}

// 2. 添加初始化逻辑
func (m *Manager) initNewParam(config ManagerConfig) int {
    if config.NewParam != nil && *config.NewParam > 0 {
        logger.Infof("[Manager] 使用用户配置的新参数: %d", *config.NewParam)
        return *config.NewParam
    }
    
    calculated := calculateNewParam()
    logger.Infof("[Manager] 使用自动计算的新参数: %d", calculated)
    return calculated
}

// 3. 添加验证函数
func validateNewParam(param *int) error {
    if param != nil && *param < 1 {
        return fmt.Errorf("newParam must be >= 1, got %d", *param)
    }
    return nil
}
```

---

## 总结

### 覆盖机制的优势

✅ **灵活性**：99% 的用户享受自动化，1% 的用户可以手动微调
✅ **安全性**：有合理的默认值和验证机制
✅ **可观测性**：清晰的日志记录覆盖情况
✅ **可维护性**：易于添加新的覆盖参数

### 策略选择的优势

✅ **自动化**：默认自动选择最优策略
✅ **灵活性**：用户可以选择特定策略
✅ **参数自动配置**：用户选择策略，参数由系统配置
✅ **风险可控**：中风险项有充分的验证和监控

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

## 实施步骤

### 第 1 步：实现覆盖机制

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

