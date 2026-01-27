# 配置文件文档化指南

## 概述

本文档说明如何将参数说明直接以注释形式放入生成的默认 config.yaml 中，使用户在编辑配置文件时能直接看到每个参数的含义和自动计算公式。

---

## 设计原则

### 1. 注释应该包含的信息

每个参数的注释应该包含：
- 参数的用途
- 参数的类型和范围
- 默认行为（自动计算或固定值）
- 自动计算公式（如果适用）
- 何时需要手动调整
- 推荐值范围

### 2. 注释的组织方式

```yaml
# 参数名称
# 
# 用途说明
# 
# 类型：类型名称
# 范围：最小值-最大值（如果适用）
# 默认：自动计算 | 固定值
# 公式：计算公式（如果适用）
# 
# 何时调整：
# - 场景 1
# - 场景 2
# 
# 推荐值：值范围
# 
# 示例：
# paramName: value
```

---

## 完整的 config.yaml 模板

```yaml
################################################################################
# SmartDNSSort DNS 上游查询配置文件
# 
# 本配置文件包含所有可配置的参数。大多数参数都有自动计算的默认值，
# 用户通常只需配置必需参数。对于特殊场景，可以手动覆盖任何参数。
#
# 配置优先级：用户配置 > 自动计算 > 默认值
################################################################################

################################################################################
# 上游 DNS 服务器配置
################################################################################
upstream:
  # 上游 DNS 服务器列表
  # 
  # 用途：指定要查询的上游 DNS 服务器
  # 
  # 类型：字符串数组
  # 格式：IP:PORT 或 DOMAIN:PORT
  # 
  # 必需：是
  # 
  # 示例：
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
    - "1.1.1.1:53"
  
  # 全局查询超时
  # 
  # 用途：控制所有 DNS 查询的最大等待时间
  # 
  # 类型：整数（毫秒）
  # 范围：1000-30000
  # 默认：5000
  # 
  # 说明：
  # - 这是所有查询的总超时时间
  # - 不会与服务器数相乘
  # - 影响所有查询策略
  # 
  # 何时调整：
  # - 网络延迟高：增加值（如 10000）
  # - 网络延迟低：减少值（如 3000）
  # 
  # 推荐值：5000-10000
  # 
  # 示例：
  timeoutMs: 5000
  
  # 查询策略
  # 
  # 用途：选择 DNS 查询的策略
  # 
  # 类型：字符串
  # 可选值：
  #   - auto: 根据服务器数自动选择最优策略（推荐）
  #   - sequential: 按健康度排序后依次尝试
  #   - parallel: 同时向所有服务器发起查询
  #   - racing: 为最佳服务器争取时间，延迟后发起备选
  #   - random: 随机选择服务器顺序
  # 
  # 默认：auto（自动选择）
  # 
  # 自动选择逻辑：
  #   - 1 个服务器 → sequential
  #   - 2-3 个服务器 → racing
  #   - 4+ 个服务器 → parallel
  # 
  # 何时手动指定：
  # - 网络不稳定：使用 sequential
  # - 需要最快响应：使用 parallel
  # - 需要平衡：使用 racing
  # 
  # 推荐值：auto（让系统自动选择）
  # 
  # 示例：
  # strategy: "auto"
  # strategy: "parallel"
  
  # 并发数
  # 
  # 用途：控制并行查询时的并发数量
  # 
  # 类型：整数
  # 范围：1-100
  # 默认：自动计算
  # 公式：max(len(servers), min(20, CPU核心数 * 2))
  # 
  # 说明：
  # - 仅在 strategy 为 parallel 时有效
  # - 自动计算会根据服务器数和 CPU 核心数调整
  # - 增加并发数会提升吞吐量，但消耗更多资源
  # 
  # 何时手动调整：
  # - 高并发环境：增加值（如 30-50）
  # - 资源受限环境：减少值（如 5-10）
  # - 需要精细控制：手动指定
  # 
  # 推荐值：
  # - 标准环境：不配置（使用自动计算）
  # - 高并发：20-50
  # - 低资源：5-10
  # 
  # 示例：
  # concurrency: 10
  
  # 最大连接数
  # 
  # 用途：控制连接池的最大连接数
  # 
  # 类型：整数
  # 范围：10-200
  # 默认：自动计算
  # 公式：max(20, CPU核心数 * 5)
  # 
  # 说明：
  # - 连接池用于复用 TCP/TLS 连接
  # - 自动计算会根据 CPU 核心数调整
  # - 增加连接数会提升吞吐量，但消耗更多内存
  # 
  # 何时手动调整：
  # - 高并发环境：增加值（如 50-100）
  # - 内存受限：减少值（如 10-20）
  # - 需要精细控制：手动指定
  # 
  # 推荐值：
  # - 标准环境：不配置（使用自动计算）
  # - 高并发：50-100
  # - 低内存：10-20
  # 
  # 示例：
  # maxConnections: 50
  
  # 单次超时（仅用于顺序查询）
  # 
  # 用途：控制顺序查询时每个服务器的超时时间
  # 
  # 类型：整数（毫秒）
  # 范围：500-5000
  # 默认：自动计算
  # 公式：max(500, timeoutMs / len(servers))
  # 
  # 说明：
  # - 仅在 strategy 为 sequential 时有效
  # - 自动计算会根据全局超时和服务器数调整
  # - 影响故障转移的速度
  # 
  # 何时手动调整：
  # - 网络延迟高：增加值（如 2000）
  # - 需要快速转移：减少值（如 500）
  # - 需要精细控制：手动指定
  # 
  # 推荐值：
  # - 标准环境：不配置（使用自动计算）
  # - 高延迟：1500-2000
  # - 低延迟：500-1000
  # 
  # 示例：
  # sequentialTimeoutMs: 1000
  
  # 竞速延迟（仅用于竞速查询）
  # 
  # 用途：控制竞速查询中备选请求的延迟时间
  # 
  # 类型：整数（毫秒）
  # 范围：50-200
  # 默认：动态计算
  # 公式：avgLatency / 10（范围限制在 50-200ms）
  # 
  # 说明：
  # - 仅在 strategy 为 racing 时有效
  # - 动态计算会根据平均延迟自动调整
  # - 影响竞速查询的平衡性
  # 
  # 何时手动调整：
  # - 需要更快的备选：减少值（如 50）
  # - 需要给主请求更多时间：增加值（如 150）
  # - 需要精细控制：手动指定
  # 
  # 推荐值：
  # - 标准环境：不配置（使用动态计算）
  # - 快速网络：50-100
  # - 慢速网络：100-150
  # 
  # 示例：
  # racingDelayMs: 100
  
  # 动态参数优化配置
  # 
  # 用途：优化动态参数的计算，防止系统抖动
  # 
  # 说明：
  # - 用于平滑动态参数的变化
  # - 防止网络波动导致的参数频繁变化
  # - 提升系统稳定性
  # 
  dynamicParamOptimization:
    # EWMA 平滑因子
    # 
    # 用途：控制动态参数变化的平滑程度
    # 
    # 类型：浮点数
    # 范围：0.0-1.0
    # 默认：0.2
    # 
    # 说明：
    # - 值越小，平滑效果越强，响应越慢
    # - 值越大，平滑效果越弱，响应越快
    # - 使用指数加权移动平均（EWMA）算法
    # 
    # 公式：smoothed = α * new + (1 - α) * old
    # 
    # 何时调整：
    # - 系统响应不够快：增加值（如 0.3-0.4）
    # - 系统抖动明显：减少值（如 0.1）
    # 
    # 推荐值：0.1-0.3
    # 
    # 示例：
    ewmaAlpha: 0.2
    
    # 最大步长
    # 
    # 用途：限制每次更新的最大变化幅度
    # 
    # 类型：整数（毫秒）
    # 范围：1-20
    # 默认：5
    # 
    # 说明：
    # - 防止参数频繁剧烈变化
    # - 限制每次更新的变化不超过此值
    # 
    # 何时调整：
    # - 系统响应不够快：减少值（如 3）
    # - 系统抖动明显：增加值（如 10）
    # 
    # 推荐值：5-10
    # 
    # 示例：
    maxStepMs: 5
    
    # 最小更新间隔
    # 
    # 用途：防止过于频繁的参数更新
    # 
    # 类型：整数（秒）
    # 范围：1-10
    # 默认：1
    # 
    # 说明：
    # - 参数最多每隔此时间更新一次
    # - 防止网络波动导致的频繁更新
    # 
    # 何时调整：
    # - 需要快速响应：减少值（如 0.5）
    # - 需要更稳定：增加值（如 2-5）
    # 
    # 推荐值：1-5
    # 
    # 示例：
    minUpdateIntervalSeconds: 1

################################################################################
# 缓存配置
################################################################################
cache:
  # ... 其他缓存参数 ...

################################################################################
# 健康检查配置
################################################################################
health:
  # ... 其他健康检查参数 ...

################################################################################
# 配置说明
################################################################################
# 
# 1. 必需参数：
#    - upstream.servers: 上游 DNS 服务器列表
#    - upstream.timeoutMs: 全局查询超时
# 
# 2. 可选参数（有自动计算的默认值）：
#    - upstream.strategy: 查询策略（默认：auto）
#    - upstream.concurrency: 并发数（默认：自动计算）
#    - upstream.maxConnections: 最大连接数（默认：自动计算）
#    - upstream.sequentialTimeoutMs: 单次超时（默认：自动计算）
#    - upstream.racingDelayMs: 竞速延迟（默认：动态计算）
# 
# 3. 配置优先级：
#    用户配置 > 自动计算 > 默认值
# 
# 4. 推荐配置方式：
#    - 标准部署：只配置 servers 和 timeoutMs
#    - 特殊场景：根据需要手动调整其他参数
# 
# 5. 日志记录：
#    系统会在启动时记录所有参数的初始化情况，
#    包括是否使用了用户配置或自动计算的值。
#
################################################################################
```

---

## 配置文件生成工具

### 生成默认配置文件

```go
// config/config_generator.go

package config

import (
    "os"
    "text/template"
)

const configTemplate = `
################################################################################
# SmartDNSSort DNS 上游查询配置文件
################################################################################

upstream:
  # 上游 DNS 服务器列表
  # 必需参数
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  
  # 全局查询超时（毫秒）
  # 默认：5000
  # 范围：1000-30000
  timeoutMs: 5000
  
  # 查询策略
  # 可选值：auto, sequential, parallel, racing, random
  # 默认：auto（自动选择）
  # strategy: "auto"
  
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
  
  # 动态参数优化配置
  dynamicParamOptimization:
    ewmaAlpha: 0.2
    maxStepMs: 5
    minUpdateIntervalSeconds: 1

cache:
  # ... 其他缓存参数 ...

health:
  # ... 其他健康检查参数 ...
`

// GenerateDefaultConfig 生成默认配置文件
func GenerateDefaultConfig(filePath string) error {
    file, err := os.Create(filePath)
    if err != nil {
        return err
    }
    defer file.Close()
    
    tmpl, err := template.New("config").Parse(configTemplate)
    if err != nil {
        return err
    }
    
    return tmpl.Execute(file, nil)
}
```

### 使用示例

```go
// cmd/main.go

func main() {
    // 生成默认配置文件
    if err := config.GenerateDefaultConfig("config.yaml"); err != nil {
        log.Fatalf("生成配置文件失败: %v", err)
    }
    
    // 加载配置文件
    cfg, err := config.LoadConfig("config.yaml")
    if err != nil {
        log.Fatalf("加载配置文件失败: %v", err)
    }
    
    // 使用配置...
}
```

---

## 配置文件验证

### 验证配置的合理性

```go
// config/config_validator.go

func ValidateConfig(cfg *Config) error {
    // 验证必需参数
    if len(cfg.Upstream.Servers) == 0 {
        return fmt.Errorf("upstream.servers 不能为空")
    }
    
    if cfg.Upstream.TimeoutMs <= 0 {
        return fmt.Errorf("upstream.timeoutMs 必须大于 0")
    }
    
    // 验证可选参数
    if cfg.Upstream.Concurrency != nil && *cfg.Upstream.Concurrency < 1 {
        return fmt.Errorf("upstream.concurrency 必须大于等于 1")
    }
    
    if cfg.Upstream.MaxConnections != nil && *cfg.Upstream.MaxConnections < 1 {
        return fmt.Errorf("upstream.maxConnections 必须大于等于 1")
    }
    
    // 验证动态参数优化配置
    if cfg.Upstream.DynamicParamOptimization.EWMAAlpha < 0 || 
       cfg.Upstream.DynamicParamOptimization.EWMAAlpha > 1 {
        return fmt.Errorf("dynamicParamOptimization.ewmaAlpha 必须在 0-1 之间")
    }
    
    return nil
}
```

---

## 配置文件示例

### 示例 1：标准部署

```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
  timeoutMs: 5000
```

### 示例 2：高并发部署

```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "8.8.4.4:53"
    - "1.1.1.1:53"
  timeoutMs: 5000
  strategy: "parallel"
  concurrency: 30
  maxConnections: 100
  dynamicParamOptimization:
    ewmaAlpha: 0.3
    maxStepMs: 10
    minUpdateIntervalSeconds: 1
```

### 示例 3：低资源部署

```yaml
upstream:
  servers:
    - "8.8.8.8:53"
  timeoutMs: 5000
  strategy: "sequential"
  concurrency: 5
  maxConnections: 10
  dynamicParamOptimization:
    ewmaAlpha: 0.1
    maxStepMs: 3
    minUpdateIntervalSeconds: 2
```

---

## 总结

### 优势

✅ 用户在编辑配置文件时能直接看到参数说明
✅ 参数含义和自动计算公式清晰可见
✅ 减少用户的学习成本
✅ 降低配置错误的可能性
✅ 提升用户体验

### 实施步骤

1. 创建详细的配置文件模板
2. 实现配置文件生成工具
3. 添加配置验证函数
4. 提供配置示例
5. 更新用户文档

### 推荐做法

- 在首次启动时自动生成默认配置文件
- 提供配置文件验证工具
- 在日志中记录配置的初始化情况
- 提供多个配置示例供用户参考

