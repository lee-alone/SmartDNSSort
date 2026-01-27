# 动态参数优化设计

## 概述

本文档说明如何优化动态参数的计算，防止参数频繁变化导致的系统抖动。

---

## 问题分析

### 当前问题

在 `getRacingDelay()` 中，如果 `avgLatency` 波动剧烈，会导致 `racingDelayMs` 频繁变化：

```go
func (m *Manager) getRacingDelay() time.Duration {
    avgLatency := m.getAverageLatency()
    calculated := avgLatency / 10
    
    // 限制范围
    if calculated < 50*time.Millisecond {
        calculated = 50 * time.Millisecond
    }
    if calculated > 200*time.Millisecond {
        calculated = 200 * time.Millisecond
    }
    
    return calculated
}
```

**问题**
- ❌ 每次调用都重新计算
- ❌ 网络波动导致频繁变化
- ❌ 可能导致系统抖动
- ❌ 不利于性能稳定

### 影响

```
时间线：
T=0ms:   avgLatency = 100ms → racingDelayMs = 10ms
T=100ms: avgLatency = 150ms → racingDelayMs = 15ms
T=200ms: avgLatency = 80ms  → racingDelayMs = 8ms
T=300ms: avgLatency = 120ms → racingDelayMs = 12ms

结果：racingDelayMs 频繁变化，系统不稳定
```

---

## 解决方案

### 方案 1：滑动窗口平滑处理（推荐）

#### 设计原理

使用指数加权移动平均（EWMA）平滑参数变化：

```
smoothedValue = α * newValue + (1 - α) * oldValue
```

其中 `α` 是平滑因子（通常 0.1-0.3）。

#### 代码实现

```go
// upstream/manager.go

type Manager struct {
    // ... 其他字段 ...
    
    // 动态参数平滑处理
    smoothedRacingDelay    time.Duration  // 平滑后的竞速延迟
    racingDelayAlpha       float64        // 平滑因子（0.2）
    lastRacingDelayUpdate  time.Time      // 最后更新时间
    minUpdateInterval      time.Duration  // 最小更新间隔（1 秒）
}

// 获取平滑后的竞速延迟
func (m *Manager) getRacingDelay() time.Duration {
    // 检查是否需要更新
    now := time.Now()
    if now.Sub(m.lastRacingDelayUpdate) < m.minUpdateInterval {
        // 更新间隔未到，返回上次的值
        return m.smoothedRacingDelay
    }
    
    // 计算新的竞速延迟
    avgLatency := m.getAverageLatency()
    newDelay := avgLatency / 10
    
    // 限制范围
    if newDelay < 50*time.Millisecond {
        newDelay = 50 * time.Millisecond
    }
    if newDelay > 200*time.Millisecond {
        newDelay = 200 * time.Millisecond
    }
    
    // 应用 EWMA 平滑
    if m.smoothedRacingDelay == 0 {
        // 第一次初始化
        m.smoothedRacingDelay = newDelay
    } else {
        // 平滑处理
        smoothed := time.Duration(
            m.racingDelayAlpha*float64(newDelay) +
            (1-m.racingDelayAlpha)*float64(m.smoothedRacingDelay),
        )
        m.smoothedRacingDelay = smoothed
    }
    
    m.lastRacingDelayUpdate = now
    
    logger.Debugf("[Manager] 更新竞速延迟: 原始=%v, 平滑=%v, 平均延迟=%v",
        newDelay, m.smoothedRacingDelay, avgLatency)
    
    return m.smoothedRacingDelay
}
```

#### 效果对比

```
原始方案（频繁变化）：
T=0ms:   avgLatency = 100ms → racingDelayMs = 10ms
T=100ms: avgLatency = 150ms → racingDelayMs = 15ms
T=200ms: avgLatency = 80ms  → racingDelayMs = 8ms
T=300ms: avgLatency = 120ms → racingDelayMs = 12ms

平滑方案（EWMA，α=0.2）：
T=0ms:   avgLatency = 100ms → smoothed = 10ms
T=100ms: avgLatency = 150ms → smoothed = 10 + 0.2*(15-10) = 11ms
T=200ms: avgLatency = 80ms  → smoothed = 11 + 0.2*(8-11) = 10.4ms
T=300ms: avgLatency = 120ms → smoothed = 10.4 + 0.2*(12-10.4) = 10.72ms

结果：变化平缓，系统稳定
```

---

### 方案 2：更新步长限制

#### 设计原理

限制每次更新的最大变化幅度：

```
newValue = clamp(
    calculatedValue,
    oldValue - maxStep,
    oldValue + maxStep
)
```

#### 代码实现

```go
// upstream/manager.go

type Manager struct {
    // ... 其他字段 ...
    
    // 动态参数步长限制
    smoothedRacingDelay    time.Duration  // 平滑后的竞速延迟
    maxRacingDelayStep     time.Duration  // 最大步长（5ms）
    lastRacingDelayUpdate  time.Time      // 最后更新时间
    minUpdateInterval      time.Duration  // 最小更新间隔（1 秒）
}

// 获取带步长限制的竞速延迟
func (m *Manager) getRacingDelay() time.Duration {
    // 检查是否需要更新
    now := time.Now()
    if now.Sub(m.lastRacingDelayUpdate) < m.minUpdateInterval {
        // 更新间隔未到，返回上次的值
        return m.smoothedRacingDelay
    }
    
    // 计算新的竞速延迟
    avgLatency := m.getAverageLatency()
    newDelay := avgLatency / 10
    
    // 限制范围
    if newDelay < 50*time.Millisecond {
        newDelay = 50 * time.Millisecond
    }
    if newDelay > 200*time.Millisecond {
        newDelay = 200 * time.Millisecond
    }
    
    // 应用步长限制
    if m.smoothedRacingDelay == 0 {
        // 第一次初始化
        m.smoothedRacingDelay = newDelay
    } else {
        // 限制变化幅度
        maxNew := m.smoothedRacingDelay + m.maxRacingDelayStep
        minNew := m.smoothedRacingDelay - m.maxRacingDelayStep
        
        if newDelay > maxNew {
            m.smoothedRacingDelay = maxNew
        } else if newDelay < minNew {
            m.smoothedRacingDelay = minNew
        } else {
            m.smoothedRacingDelay = newDelay
        }
    }
    
    m.lastRacingDelayUpdate = now
    
    logger.Debugf("[Manager] 更新竞速延迟: 原始=%v, 限制后=%v, 平均延迟=%v",
        newDelay, m.smoothedRacingDelay, avgLatency)
    
    return m.smoothedRacingDelay
}
```

#### 效果对比

```
原始方案（频繁变化）：
T=0ms:   avgLatency = 100ms → racingDelayMs = 10ms
T=100ms: avgLatency = 150ms → racingDelayMs = 15ms
T=200ms: avgLatency = 80ms  → racingDelayMs = 8ms
T=300ms: avgLatency = 120ms → racingDelayMs = 12ms

步长限制方案（maxStep=5ms）：
T=0ms:   avgLatency = 100ms → limited = 10ms
T=100ms: avgLatency = 150ms → limited = min(15, 10+5) = 15ms
T=200ms: avgLatency = 80ms  → limited = max(8, 15-5) = 10ms
T=300ms: avgLatency = 120ms → limited = min(12, 10+5) = 12ms

结果：变化受限，系统稳定
```

---

### 方案 3：组合方案（推荐）

结合 EWMA 和步长限制，获得最佳效果：

```go
func (m *Manager) getRacingDelay() time.Duration {
    // 检查是否需要更新
    now := time.Now()
    if now.Sub(m.lastRacingDelayUpdate) < m.minUpdateInterval {
        return m.smoothedRacingDelay
    }
    
    // 计算新的竞速延迟
    avgLatency := m.getAverageLatency()
    newDelay := avgLatency / 10
    
    // 限制范围
    newDelay = clamp(newDelay, 50*time.Millisecond, 200*time.Millisecond)
    
    // 应用步长限制
    if m.smoothedRacingDelay != 0 {
        maxNew := m.smoothedRacingDelay + m.maxRacingDelayStep
        minNew := m.smoothedRacingDelay - m.maxRacingDelayStep
        newDelay = clamp(newDelay, minNew, maxNew)
    }
    
    // 应用 EWMA 平滑
    if m.smoothedRacingDelay == 0 {
        m.smoothedRacingDelay = newDelay
    } else {
        smoothed := time.Duration(
            m.racingDelayAlpha*float64(newDelay) +
            (1-m.racingDelayAlpha)*float64(m.smoothedRacingDelay),
        )
        m.smoothedRacingDelay = smoothed
    }
    
    m.lastRacingDelayUpdate = now
    
    return m.smoothedRacingDelay
}

func clamp(value, min, max time.Duration) time.Duration {
    if value < min {
        return min
    }
    if value > max {
        return max
    }
    return value
}
```

---

## 配置参数

### 新增配置参数

```yaml
upstream:
  # ... 其他参数 ...
  
  # 动态参数优化
  dynamicParamOptimization:
    # 平滑因子（0.0-1.0，默认 0.2）
    # 值越小，平滑效果越强，响应越慢
    # 值越大，平滑效果越弱，响应越快
    ewmaAlpha: 0.2
    
    # 最大步长（毫秒，默认 5）
    # 限制每次更新的最大变化幅度
    maxStepMs: 5
    
    # 最小更新间隔（秒，默认 1）
    # 防止过于频繁的更新
    minUpdateIntervalSeconds: 1
```

---

## 配置文件文档化

### 生成的默认 config.yaml

```yaml
# DNS 上游查询配置

upstream:
  # 上游 DNS 服务器列表
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  
  # 全局查询超时（毫秒）
  # 必需参数，影响所有查询的最大等待时间
  timeoutMs: 5000
  
  # 查询策略
  # 可选值：auto, sequential, parallel, racing, random
  # 默认：auto（自动选择）
  # - auto: 根据服务器数自动选择最优策略
  # - sequential: 按健康度排序后依次尝试
  # - parallel: 同时向所有服务器发起查询
  # - racing: 为最佳服务器争取时间，延迟后发起备选
  # - random: 随机选择服务器顺序
  # strategy: "auto"
  
  # 并发数（可选）
  # 默认：自动计算 = max(len(servers), min(20, CPU核心数 * 2))
  # 用于控制并行查询时的并发数量
  # 如果需要手动调整，取消注释并修改值
  # concurrency: 10
  
  # 最大连接数（可选）
  # 默认：自动计算 = max(20, CPU核心数 * 5)
  # 用于控制连接池的最大连接数
  # 如果需要手动调整，取消注释并修改值
  # maxConnections: 50
  
  # 单次超时（毫秒，可选，仅用于顺序查询）
  # 默认：自动计算 = max(500, timeoutMs / len(servers))
  # 用于控制顺序查询时每个服务器的超时时间
  # 如果需要手动调整，取消注释并修改值
  # sequentialTimeoutMs: 1000
  
  # 竞速延迟（毫秒，可选，仅用于竞速查询）
  # 默认：动态计算 = avgLatency / 10（范围 50-200ms）
  # 用于控制竞速查询中备选请求的延迟时间
  # 如果需要手动调整，取消注释并修改值
  # racingDelayMs: 100
  
  # 动态参数优化配置
  dynamicParamOptimization:
    # EWMA 平滑因子（0.0-1.0，默认 0.2）
    # 用于平滑动态参数的变化，防止系统抖动
    # 值越小，平滑效果越强，响应越慢
    # 值越大，平滑效果越弱，响应越快
    # 推荐值：0.1-0.3
    ewmaAlpha: 0.2
    
    # 最大步长（毫秒，默认 5）
    # 限制每次更新的最大变化幅度
    # 防止参数频繁剧烈变化
    # 推荐值：5-10
    maxStepMs: 5
    
    # 最小更新间隔（秒，默认 1）
    # 防止过于频繁的参数更新
    # 推荐值：1-5
    minUpdateIntervalSeconds: 1

# 缓存配置
cache:
  # ... 其他缓存参数 ...

# 健康检查配置
health:
  # ... 其他健康检查参数 ...
```

---

## 实现步骤

### 第 1 步：添加配置结构

```go
// config/config_types.go

type DynamicParamOptimization struct {
    EWMAAlpha                float64 `yaml:"ewmaAlpha"`
    MaxStepMs                int     `yaml:"maxStepMs"`
    MinUpdateIntervalSeconds int     `yaml:"minUpdateIntervalSeconds"`
}

type UpstreamConfig struct {
    // ... 其他字段 ...
    DynamicParamOptimization DynamicParamOptimization `yaml:"dynamicParamOptimization"`
}
```

### 第 2 步：实现平滑处理

```go
// upstream/manager.go

func (m *Manager) initDynamicParamOptimization(config DynamicParamOptimization) {
    if config.EWMAAlpha <= 0 || config.EWMAAlpha > 1 {
        m.racingDelayAlpha = 0.2  // 默认值
    } else {
        m.racingDelayAlpha = config.EWMAAlpha
    }
    
    if config.MaxStepMs <= 0 {
        m.maxRacingDelayStep = 5 * time.Millisecond  // 默认值
    } else {
        m.maxRacingDelayStep = time.Duration(config.MaxStepMs) * time.Millisecond
    }
    
    if config.MinUpdateIntervalSeconds <= 0 {
        m.minUpdateInterval = 1 * time.Second  // 默认值
    } else {
        m.minUpdateInterval = time.Duration(config.MinUpdateIntervalSeconds) * time.Second
    }
}
```

### 第 3 步：更新 getRacingDelay()

```go
// upstream/manager_racing.go

func (m *Manager) getRacingDelay() time.Duration {
    // 检查是否需要更新
    now := time.Now()
    if now.Sub(m.lastRacingDelayUpdate) < m.minUpdateInterval {
        return m.smoothedRacingDelay
    }
    
    // 计算新的竞速延迟
    avgLatency := m.getAverageLatency()
    newDelay := avgLatency / 10
    
    // 限制范围
    newDelay = clamp(newDelay, 50*time.Millisecond, 200*time.Millisecond)
    
    // 应用步长限制
    if m.smoothedRacingDelay != 0 {
        maxNew := m.smoothedRacingDelay + m.maxRacingDelayStep
        minNew := m.smoothedRacingDelay - m.maxRacingDelayStep
        newDelay = clamp(newDelay, minNew, maxNew)
    }
    
    // 应用 EWMA 平滑
    if m.smoothedRacingDelay == 0 {
        m.smoothedRacingDelay = newDelay
    } else {
        smoothed := time.Duration(
            m.racingDelayAlpha*float64(newDelay) +
            (1-m.racingDelayAlpha)*float64(m.smoothedRacingDelay),
        )
        m.smoothedRacingDelay = smoothed
    }
    
    m.lastRacingDelayUpdate = now
    
    logger.Debugf("[Manager] 更新竞速延迟: 原始=%v, 平滑=%v, 平均延迟=%v",
        newDelay, m.smoothedRacingDelay, avgLatency)
    
    return m.smoothedRacingDelay
}
```

---

## 监控和调优

### 监控指标

```go
// 记录参数变化
logger.Infof("[Manager] 竞速延迟变化: %v → %v (平均延迟: %v)",
    oldDelay, newDelay, avgLatency)

// 记录平滑效果
logger.Debugf("[Manager] 平滑效果: 原始变化 %v, 实际变化 %v",
    newDelay - oldDelay, m.smoothedRacingDelay - oldDelay)
```

### 调优建议

**如果系统响应不够快**
- 增加 `ewmaAlpha`（从 0.2 改为 0.3-0.4）
- 减少 `maxStepMs`（从 5 改为 3）
- 减少 `minUpdateIntervalSeconds`（从 1 改为 0.5）

**如果系统抖动明显**
- 减少 `ewmaAlpha`（从 0.2 改为 0.1）
- 增加 `maxStepMs`（从 5 改为 10）
- 增加 `minUpdateIntervalSeconds`（从 1 改为 2）

---

## 总结

### 优势

✅ 防止参数频繁变化导致的系统抖动
✅ 平滑响应网络波动
✅ 提升系统稳定性
✅ 易于调优

### 推荐配置

```yaml
dynamicParamOptimization:
  ewmaAlpha: 0.2
  maxStepMs: 5
  minUpdateIntervalSeconds: 1
```

### 预期效果

- 参数变化更平缓
- 系统更稳定
- 性能更可预测

