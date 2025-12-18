# Config Package Structure

## Overview
config 包中的 config.go 文件已被拆分为多个专注的文件，以提高代码的可维护性和可读性。

## File Organization

### 1. **config.go**
主配置加载和工具函数。
- `AvgBytesPerDomain` 常量 - 每个域名的平均字节数
- `CalculateMaxEntries()` - 根据最大内存限制计算最大缓存条目数
- `CreateDefaultConfig()` - 创建默认配置文件
- `ValidateAndRepairConfig()` - 验证并修复配置文件
- `LoadConfig()` - 从 YAML 文件加载配置

### 2. **config_types.go**
所有配置类型定义。
- `Config` - 主配置结构
- `DNSConfig` - DNS 服务器配置
- `UpstreamConfig` - 上游 DNS 服务器配置
- `HealthCheckConfig` - 健康检查配置
- `PingConfig` - Ping 检测配置
- `CacheConfig` - DNS 缓存配置
- `PrefetchConfig` - 预取配置
- `WebUIConfig` - Web UI 管理界面配置
- `AdBlockConfig` - 广告拦截配置
- `SystemConfig` - 系统资源配置
- `StatsConfig` - 统计配置

### 3. **config_defaults.go**
默认值设置逻辑。
- `setDefaultValues()` - 设置所有配置的默认值
- `setHealthCheckDefaults()` - 设置健康检查默认值
- `setPingDefaults()` - 设置 Ping 配置默认值
- `setCacheDefaults()` - 设置缓存配置默认值
- `setAdBlockDefaults()` - 设置广告拦截默认值
- `setSystemDefaults()` - 设置系统配置默认值
- `setStatsDefaults()` - 设置统计配置默认值

### 4. **config_content.go**
默认配置文件内容。
- `DefaultConfigContent` 常量 - 包含详细说明的默认配置文件内容

## Configuration Hierarchy

```
Config (主配置)
├── DNS (DNS 服务器配置)
│   ├── ListenPort
│   ├── EnableTCP
│   └── EnableIPv6
├── Upstream (上游 DNS 配置)
│   ├── Servers
│   ├── BootstrapDNS
│   ├── Strategy (parallel, random, sequential, racing)
│   ├── TimeoutMs
│   ├── Concurrency
│   ├── SequentialTimeout
│   ├── RacingDelay
│   ├── RacingMaxConcurrent
│   ├── NxdomainForErrors
│   ├── Dnssec
│   └── HealthCheck
│       ├── Enabled
│       ├── FailureThreshold
│       ├── CircuitBreakerThreshold
│       ├── CircuitBreakerTimeout
│       └── SuccessThreshold
├── Ping (Ping 检测配置)
│   ├── Enabled
│   ├── Count
│   ├── TimeoutMs
│   ├── Concurrency
│   ├── Strategy
│   ├── MaxTestIPs
│   ├── RttCacheTtlSeconds
│   └── EnableHttpFallback
├── Cache (缓存配置)
│   ├── FastResponseTTL
│   ├── UserReturnTTL
│   ├── MinTTLSeconds
│   ├── MaxTTLSeconds
│   ├── NegativeTTLSeconds
│   ├── ErrorCacheTTL
│   ├── MaxMemoryMB
│   ├── KeepExpiredEntries
│   ├── EvictionThreshold
│   ├── EvictionBatchPercent
│   ├── ProtectPrefetchDomains
│   ├── SaveToDiskIntervalMinutes
│   └── MsgCacheSizeMB
├── Prefetch (预取配置)
│   └── Enabled
├── WebUI (Web UI 配置)
│   ├── Enabled
│   └── ListenPort
├── AdBlock (广告拦截配置)
│   ├── Enable
│   ├── Engine
│   ├── RuleURLs
│   ├── CustomRulesFile
│   ├── CustomResponseFile
│   ├── CacheDir
│   ├── UpdateIntervalHours
│   ├── MaxCacheAgeHours
│   ├── MaxCacheSizeMB
│   ├── BlockMode
│   ├── BlockedResponseIP
│   ├── BlockedTTL
│   ├── LastUpdate
│   └── FailedSources
├── System (系统配置)
│   ├── MaxCPUCores
│   ├── SortQueueWorkers
│   ├── RefreshWorkers
│   └── LogLevel
└── Stats (统计配置)
    ├── HotDomainsWindowHours
    ├── HotDomainsBucketMinutes
    ├── HotDomainsShardCount
    └── HotDomainsMaxPerBucket
```

## Default Values

### DNS
- `ListenPort`: 53
- `EnableTCP`: true
- `EnableIPv6`: true

### Upstream
- `TimeoutMs`: 5000
- `Concurrency`: 3
- `BootstrapDNS`: ["8.8.8.8", "1.1.1.1", "8.8.4.4", "1.0.0.1"]
- `HealthCheck.FailureThreshold`: 3
- `HealthCheck.CircuitBreakerThreshold`: 5
- `HealthCheck.CircuitBreakerTimeout`: 30
- `HealthCheck.SuccessThreshold`: 2

### Ping
- `Enabled`: true
- `Count`: 3
- `TimeoutMs`: 1000
- `Concurrency`: 16
- `RttCacheTtlSeconds`: 60

### Cache
- `FastResponseTTL`: 15
- `UserReturnTTL`: 600
- `NegativeTTLSeconds`: 300
- `ErrorCacheTTL`: 30
- `MaxMemoryMB`: 128
- `EvictionThreshold`: 0.9
- `EvictionBatchPercent`: 0.1
- `MsgCacheSizeMB`: MaxMemoryMB / 10 (最小 1MB)
- `SaveToDiskIntervalMinutes`: 60

### AdBlock
- `Engine`: "urlfilter"
- `CustomRulesFile`: "./custom_rules.txt"
- `CustomResponseFile`: "./adblock_cache/custom_response_rules.txt"
- `CacheDir`: "./adblock_cache"
- `UpdateIntervalHours`: 168
- `MaxCacheAgeHours`: 168
- `MaxCacheSizeMB`: 30
- `BlockMode`: "nxdomain"
- `BlockedTTL`: 3600

### System
- `MaxCPUCores`: runtime.NumCPU()
- `SortQueueWorkers`: MaxCPUCores
- `RefreshWorkers`: MaxCPUCores
- `LogLevel`: "info"

### Stats
- `HotDomainsWindowHours`: 24
- `HotDomainsBucketMinutes`: 60
- `HotDomainsShardCount`: 16
- `HotDomainsMaxPerBucket`: 5000

## Key Features

1. **职责分离** - 类型定义、默认值、内容分离
2. **易于维护** - 相关逻辑聚集在一起
3. **易于扩展** - 添加新配置只需修改相关文件
4. **自动创建** - 配置文件不存在时自动创建
5. **默认值管理** - 集中管理所有默认值
6. **YAML 支持** - 完整的 YAML 配置文件支持
7. **JSON 支持** - 所有配置都支持 JSON 序列化

## Configuration Loading Flow

```
LoadConfig(filePath)
    ↓
1. 读取 YAML 文件
    ↓
2. 如果文件不存在，创建默认配置
    ↓
3. 解析 YAML 到 Config 结构
    ↓
4. 设置默认值 (setDefaultValues)
    ├─ DNS 默认值
    ├─ Upstream 默认值
    ├─ Ping 默认值
    ├─ Cache 默认值
    ├─ AdBlock 默认值
    ├─ System 默认值
    └─ Stats 默认值
    ↓
5. 返回配置对象
```

## Testing

所有现有的测试文件保持不变：
- `config_test.go` - 配置加载测试
