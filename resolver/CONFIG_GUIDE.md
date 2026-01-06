# 递归DNS解析器配置指南

本指南详细说明了如何配置递归DNS解析器，包括所有配置选项、默认值和最佳实践。

## 目录

1. [配置文件位置](#配置文件位置)
2. [配置文件格式](#配置文件格式)
3. [配置选项详解](#配置选项详解)
4. [工作模式](#工作模式)
5. [传输方式](#传输方式)
6. [性能调优](#性能调优)
7. [常见配置场景](#常见配置场景)
8. [故障排查](#故障排查)

## 配置文件位置

### 主配置文件

主配置文件位于项目根目录，默认名称为 `config.yaml`。

```yaml
# config.yaml
resolver:
  enabled: false
  config_file: resolver.yaml
  transport: auto
```

### 递归解析器配置文件

递归解析器配置文件默认名称为 `resolver.yaml`，位置由主配置文件中的 `config_file` 指定。

```yaml
# resolver.yaml
server:
  transport: auto
  # ... 其他配置
```

## 配置文件格式

配置文件使用YAML格式。YAML是一种人类可读的数据序列化格式，具有以下特点：

- 使用缩进表示层级关系
- 使用冒号分隔键和值
- 使用井号表示注释
- 支持列表和字典

### 基本语法

```yaml
# 注释
key: value

# 嵌套对象
parent:
  child: value

# 列表
items:
  - item1
  - item2
  - item3

# 字符串
string: "value"
string_with_spaces: "value with spaces"

# 数字
number: 123
float: 123.45

# 布尔值
boolean: true
boolean: false
```

## 配置选项详解

### 服务器配置 (server)

#### transport

**类型**: 字符串  
**可选值**: `auto`, `unix`, `tcp`  
**默认值**: `auto`  
**说明**: 与递归解析器的通信方式

- `auto`: 自动选择最优方式（Linux/macOS优先UDS，Windows使用TCP）
- `unix`: 使用Unix Domain Socket（仅Linux/macOS）
- `tcp`: 使用TCP/IP（跨平台）

```yaml
server:
  transport: auto
```

#### unix_socket

**类型**: 对象  
**说明**: Unix Domain Socket配置（仅当transport为unix或auto时有效）

```yaml
server:
  unix_socket:
    path: /tmp/smartdns-resolver.sock
    permissions: "0600"
```

**子选项**:

- `path` (字符串): Unix socket文件路径
  - 默认值: `/tmp/smartdns-resolver.sock`
  - 建议: 使用 `/tmp` 或 `/var/run` 目录

- `permissions` (字符串): 文件权限（八进制）
  - 默认值: `0600`（仅所有者可读写）
  - 建议: 保持 `0600` 以确保安全性

#### tcp

**类型**: 对象  
**说明**: TCP配置（仅当transport为tcp或auto时有效）

```yaml
server:
  tcp:
    listen_addr: 127.0.0.1
    listen_port: 5353
```

**子选项**:

- `listen_addr` (字符串): 监听地址
  - 默认值: `127.0.0.1`
  - 建议: 仅监听本地地址以确保安全性
  - 可选值: `127.0.0.1` (本地), `0.0.0.0` (所有接口)

- `listen_port` (数字): 监听端口
  - 默认值: `5353`
  - 范围: `1024-65535`（非root用户）
  - 建议: 使用 `5353` 或其他非标准端口

#### timeout_ms

**类型**: 数字  
**默认值**: `5000`  
**单位**: 毫秒  
**说明**: DNS查询超时时间

```yaml
server:
  timeout_ms: 5000
```

**建议值**:
- 本地网络: `3000-5000`
- 互联网: `5000-10000`
- 高延迟网络: `10000-30000`

#### mode

**类型**: 字符串  
**可选值**: `recursive`, `forwarding`, `hybrid`  
**默认值**: `recursive`  
**说明**: 工作模式

```yaml
server:
  mode: recursive
```

详见 [工作模式](#工作模式) 部分。

### 解析器配置 (resolver)

#### cache

**类型**: 对象  
**说明**: DNS缓存配置

```yaml
resolver:
  cache:
    size: 10000
    expiry: true
```

**子选项**:

- `size` (数字): 缓存大小（条目数）
  - 默认值: `10000`
  - 范围: `100-1000000`
  - 建议: 根据内存可用性调整

- `expiry` (布尔值): 是否启用TTL过期
  - 默认值: `true`
  - `true`: 根据DNS记录的TTL自动过期
  - `false`: 使用固定的缓存过期时间

#### max_depth

**类型**: 数字  
**默认值**: `30`  
**说明**: 最大递归深度

```yaml
resolver:
  max_depth: 30
```

**建议值**:
- 标准配置: `30`
- 深层域名: `50`
- 限制资源: `15`

#### dnssec

**类型**: 对象  
**说明**: DNSSEC验证配置

```yaml
resolver:
  dnssec:
    enabled: false
    validate: true
```

**子选项**:

- `enabled` (布尔值): 是否启用DNSSEC
  - 默认值: `false`
  - `true`: 启用DNSSEC验证
  - `false`: 禁用DNSSEC验证

- `validate` (布尔值): 是否验证签名
  - 默认值: `true`
  - `true`: 验证DNSSEC签名
  - `false`: 不验证签名

### 优化配置 (optimization)

#### enabled

**类型**: 布尔值  
**默认值**: `true`  
**说明**: 是否启用IP优选

```yaml
optimization:
  enabled: true
```

- `true`: 对返回的IP进行速度测试和排序
- `false`: 返回原始IP顺序

### 混合模式规则 (hybrid_rules)

**类型**: 对象  
**说明**: 混合模式下的域名规则（仅当mode为hybrid时有效）

```yaml
hybrid_rules:
  recursive_domains:
    - example.com
    - "*.internal.company.com"
  forward_domains:
    - google.com
    - "*.cdn.example.com"
  default: recursive
```

**子选项**:

- `recursive_domains` (列表): 使用递归解析的域名列表
  - 支持通配符: `*.example.com`
  - 支持精确匹配: `example.com`

- `forward_domains` (列表): 使用转发模式的域名列表
  - 支持通配符: `*.example.com`
  - 支持精确匹配: `example.com`

- `default` (字符串): 默认行为
  - `recursive`: 不匹配规则时使用递归
  - `forwarding`: 不匹配规则时使用转发

### 性能配置 (performance)

#### workers

**类型**: 数字  
**默认值**: `4`  
**说明**: 工作协程数

```yaml
performance:
  workers: 4
```

**建议值**:
- 低端设备: `1-2`
- 标准配置: `4-8`
- 高端设备: `8-16`
- 超高并发: `16-32`

#### max_concurrent

**类型**: 数字  
**默认值**: `100`  
**说明**: 最大并发查询数

```yaml
performance:
  max_concurrent: 100
```

**建议值**:
- 低端设备: `10-20`
- 标准配置: `50-100`
- 高端设备: `100-500`
- 超高并发: `500-2000`

### 日志配置 (logging)

#### level

**类型**: 字符串  
**可选值**: `debug`, `info`, `warn`, `error`  
**默认值**: `info`  
**说明**: 日志级别

```yaml
logging:
  level: info
```

**日志级别说明**:
- `debug`: 详细的调试信息（最详细）
- `info`: 一般信息
- `warn`: 警告信息
- `error`: 仅错误信息（最简洁）

#### file

**类型**: 字符串  
**默认值**: `logs/resolver.log`  
**说明**: 日志文件路径

```yaml
logging:
  file: logs/resolver.log
```

**建议**:
- 使用绝对路径或相对于项目根目录的路径
- 确保日志目录存在且有写入权限
- 定期清理旧日志文件

## 工作模式

### Recursive（递归模式）

对所有查询使用递归解析，从根服务器开始迭代查询。

**配置**:
```yaml
server:
  mode: recursive
```

**适用场景**:
- 完全独立的DNS解析
- 不依赖上游DNS服务器
- 需要完整的DNS查询链

**优点**:
- 完全独立
- 可以解析任何域名
- 不受上游DNS限制

**缺点**:
- 查询延迟较高
- 需要更多资源
- 需要访问根服务器

### Forwarding（转发模式）

对所有查询使用转发模式，将查询转发到上游DNS服务器。

**配置**:
```yaml
server:
  mode: forwarding
```

**适用场景**:
- 使用上游DNS服务器
- 需要快速响应
- 资源受限的环境

**优点**:
- 查询延迟低
- 资源消耗少
- 配置简单

**缺点**:
- 依赖上游DNS
- 受上游DNS限制
- 可能被DNS污染

### Hybrid（混合模式）

根据域名规则选择递归或转发模式。

**配置**:
```yaml
server:
  mode: hybrid

hybrid_rules:
  recursive_domains:
    - example.com
    - "*.internal.company.com"
  forward_domains:
    - google.com
  default: recursive
```

**适用场景**:
- 需要灵活的解析策略
- 某些域名需要递归，某些需要转发
- 混合内部和外部域名

**优点**:
- 灵活的解析策略
- 可以优化性能
- 支持特殊域名处理

**缺点**:
- 配置复杂
- 需要维护规则列表
- 规则匹配有性能开销

## 传输方式

### Auto（自动选择）

系统自动检测操作系统并选择最优方式。

**配置**:
```yaml
server:
  transport: auto
```

**选择逻辑**:
- Linux/macOS: 优先使用Unix Domain Socket，如果不可用则使用TCP
- Windows: 使用TCP

**建议**: 大多数情况下使用此选项

### Unix Domain Socket

高性能的进程间通信方式（仅在Linux/macOS上可用）。

**配置**:
```yaml
server:
  transport: unix
  unix_socket:
    path: /tmp/smartdns-resolver.sock
    permissions: "0600"
```

**优点**:
- 性能最优
- 不占用网络端口
- 安全性高

**缺点**:
- 仅支持Linux/macOS
- 不支持远程连接
- 需要文件系统支持

### TCP

跨平台的网络通信方式。

**配置**:
```yaml
server:
  transport: tcp
  tcp:
    listen_addr: 127.0.0.1
    listen_port: 5353
```

**优点**:
- 跨平台支持
- 支持远程连接
- 兼容性好

**缺点**:
- 性能略低于UDS
- 占用网络端口
- 需要网络配置

## 性能调优

### 高并发场景

```yaml
server:
  transport: unix
  timeout_ms: 3000
  mode: recursive

resolver:
  cache:
    size: 50000
    expiry: true

performance:
  workers: 16
  max_concurrent: 500

logging:
  level: warn
```

**关键调整**:
- 使用Unix Domain Socket
- 增加缓存大小
- 增加工作协程数
- 增加最大并发数
- 降低日志级别

### 低资源环境

```yaml
server:
  transport: tcp
  timeout_ms: 10000
  mode: recursive

resolver:
  cache:
    size: 1000
    expiry: true

optimization:
  enabled: false

performance:
  workers: 2
  max_concurrent: 20

logging:
  level: error
```

**关键调整**:
- 使用TCP（更简单）
- 减少缓存大小
- 禁用IP优选
- 减少工作协程数
- 减少最大并发数
- 仅记录错误

### 平衡配置

```yaml
server:
  transport: auto
  timeout_ms: 5000
  mode: recursive

resolver:
  cache:
    size: 10000
    expiry: true

optimization:
  enabled: true

performance:
  workers: 4
  max_concurrent: 100

logging:
  level: info
```

## 常见配置场景

### 场景1: 开发环境

```yaml
server:
  transport: auto
  timeout_ms: 5000
  mode: recursive

resolver:
  cache:
    size: 5000
    expiry: true

optimization:
  enabled: true

performance:
  workers: 2
  max_concurrent: 50

logging:
  level: debug
  file: logs/resolver.log
```

### 场景2: 生产环境

```yaml
server:
  transport: unix
  timeout_ms: 3000
  mode: recursive

resolver:
  cache:
    size: 50000
    expiry: true

optimization:
  enabled: true

performance:
  workers: 8
  max_concurrent: 200

logging:
  level: warn
  file: /var/log/smartdns/resolver.log
```

### 场景3: 企业内网

```yaml
server:
  transport: auto
  timeout_ms: 5000
  mode: hybrid

resolver:
  cache:
    size: 20000
    expiry: true

hybrid_rules:
  recursive_domains:
    - "*.internal.company.com"
    - "*.local"
  forward_domains:
    - "*.example.com"
  default: recursive

optimization:
  enabled: true

performance:
  workers: 4
  max_concurrent: 100

logging:
  level: info
  file: logs/resolver.log
```

### 场景4: 公共DNS服务

```yaml
server:
  transport: tcp
  tcp:
    listen_addr: 0.0.0.0
    listen_port: 53
  timeout_ms: 3000
  mode: recursive

resolver:
  cache:
    size: 100000
    expiry: true

optimization:
  enabled: true

performance:
  workers: 16
  max_concurrent: 1000

logging:
  level: warn
  file: /var/log/smartdns/resolver.log
```

## 故障排查

### 问题1: 连接失败

**症状**: 无法连接到递归解析器

**排查步骤**:
1. 检查递归解析器是否启动
2. 检查传输方式配置
3. 检查Unix socket文件是否存在
4. 检查TCP端口是否被占用
5. 检查防火墙设置

**解决方案**:
```yaml
# 尝试使用TCP
server:
  transport: tcp
  tcp:
    listen_addr: 127.0.0.1
    listen_port: 5353
```

### 问题2: 查询超时

**症状**: 查询经常超时

**排查步骤**:
1. 检查网络连接
2. 检查超时时间设置
3. 检查递归深度限制
4. 检查缓存命中率

**解决方案**:
```yaml
# 增加超时时间
server:
  timeout_ms: 10000

# 增加缓存大小
resolver:
  cache:
    size: 50000
```

### 问题3: 高CPU使用率

**症状**: CPU使用率过高

**排查步骤**:
1. 检查工作协程数
2. 检查并发查询数
3. 检查日志级别
4. 检查缓存大小

**解决方案**:
```yaml
# 减少工作协程数
performance:
  workers: 2
  max_concurrent: 50

# 降低日志级别
logging:
  level: error
```

### 问题4: 高内存使用率

**症状**: 内存使用率过高

**排查步骤**:
1. 检查缓存大小
2. 检查并发查询数
3. 检查是否有内存泄漏

**解决方案**:
```yaml
# 减少缓存大小
resolver:
  cache:
    size: 5000

# 减少最大并发数
performance:
  max_concurrent: 50
```

## 配置验证

所有配置都会在加载时自动验证。验证规则包括：

| 配置项 | 验证规则 |
|--------|---------|
| transport | 必须是 `auto`, `unix`, `tcp` |
| mode | 必须是 `recursive`, `forwarding`, `hybrid` |
| timeout_ms | 必须为正数 |
| cache.size | 必须为正数 |
| max_depth | 必须为正数 |
| workers | 必须为正数 |
| max_concurrent | 必须为正数 |
| level | 必须是 `debug`, `info`, `warn`, `error` |

## 最佳实践

1. **使用自动传输方式**: 让系统自动选择最优方式
2. **合理设置缓存大小**: 根据内存可用性调整
3. **监控性能指标**: 定期检查统计信息
4. **定期更新规则**: 在混合模式下定期更新域名规则
5. **备份配置文件**: 保存重要配置的备份
6. **使用版本控制**: 跟踪配置文件的变更
7. **文档化配置**: 记录配置的原因和含义
8. **测试配置**: 在生产环境前充分测试

## 相关文件

- `README.md` - 模块文档
- `QUICK_START.md` - 快速开始指南
- `EXAMPLES.md` - 使用示例

</content>
