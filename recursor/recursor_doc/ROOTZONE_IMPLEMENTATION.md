"# Root.zone 实现说明

## 概述

为了减少递归进程对上游的查询压力，本文档说明如何在 Unbound 中配置本地 root.zone 文件，实现高效的本地 DNS 解析。

## 实现架构

### 核心组件

1. **RootZoneManager** (`recursor/manager_rootzone.go`)
   - 管理 root.zone 文件的下载、验证和更新
   - 支持文件存在性检查，避免覆盖已有文件
   - 自动定期更新（默认7天）

2. **ConfigGenerator** (`recursor/config_generator.go`)
   - 在生成 Unbound 配置时自动添加 auth-zone 配置
   - 集成 root.zone 管理器

3. **Manager** (`recursor/manager.go`)
   - 在启动时初始化 root.zone 文件
   - 启动后台定期更新任务
   - 在停止时清理资源

## 关键特性

### 1. 文件管理策略

- **首次启动**：如果 recursor/data/root.zone 不存在，自动从官方源下载
- **存在检查**：启动时检查文件是否存在和有效性
- **不覆盖策略**：如果文件已存在且未过期，不会覆盖现有文件
- **定期更新**：默认每7天检查并更新一次

### 2. Unbound 配置方式

使用 `auth-zone` 方式配置 root.zone：
```unbound
auth-zone:
    name: \".\"
    zonefile: \"/path/to/root.zone\"
    for-downstream: yes
    fallback: yes
```

**关键参数说明：**
- `name: "."` - 根 zone
- `zonefile` - 本地文件路径
- `for-downstream: yes` - 允许提供给下游查询
- `fallback: yes` - 如果本地没有记录，允许递归查询（避免误操作影响）
- **不配置 master** - 这样 unbound 不会尝试从 master 更新，完全使用本地文件

### 3. 文件权限管理

- 权限设置为 `0644` (rw-r--r--)
- 确保文件可以被 unbound 进程读写（用于定时更新）
- 同时保证其他用户无法修改

### 4. 下载和验证

**下载源：** `https://www.internic.net/domain/root.zone`

**验证机制：**
1. HTTP 状态码检查（必须为 200 OK）
2. 文件大小验证（必须大于 1KB）
3. 基本格式检查（包含 DNS zone 记录）

**原子性保证：**
- 先下载到临时文件（root.zone.tmp）
- 验证通过后原子重命名替换
- 避免写入过程中断导致文件损坏

## 配置参数

### 可调参数

在 `manager_rootzone.go` 中可以调整：

```go
const (
    // 官方root.zone下载源
    RootZoneURL = \"https://www.internic.net/domain/root.zone\"
    
    // root.zone文件名
    RootZoneFilename = \"root.zone\"
    
    // 更新间隔（7天）
    RootZoneUpdateInterval = 7 * 24 * time.Hour
)
```

### 修改更新间隔

可以根据实际需求调整更新频率：
- 生产环境：建议 7-14 天
- 高安全要求：可以设置为 1 天
- 如果上游稳定：可以设置为 30 天

## 使用流程

### 首次启动

1. Manager.Start() 调用
2. 初始化 RootZoneManager
3. 检查 recursor/data/root.zone 是否存在
4. 如果不存在，从官网下载
5. 验证文件有效性
6. 生成包含 auth-zone 的 Unbound 配置
7. 启动 periodic update goroutine

### 日常运行

1. 每 7 天检查更新
2. 如果文件过期，下载新版本到临时文件
3. 验证新文件
4. 原子替换旧文件
5. Unbound 自动重载 zone 文件

### 停止

1. 关闭 periodic update goroutine
2. 停止 Unbound 进程
3. 清理配置文件
4. **保留 root.zone 文件**（供下次启动使用）

## 优势对比

### root.zone vs root-hints

| 特性 | root.zone (auth-zone) | root-hints |
|------|----------------------|------------|
| 查询速度 | 最快（本地权威） | 快（本地缓存） |
| 上游压力 | 最小（仅更新时） | 较小（仍需查询递归） |
| 更新机制 | 可配置定时更新 | 依赖递归服务器 |
| 配置复杂度 | 中等 | 简单 |
| 占用内存 | 稍高（加载完整zone） | 低（仅NS记录） |

## 监控和维护

### 日志输出

系统会输出以下关键日志：

```
[Recursor] Ensuring root.zone file...
[Recursor] New root.zone file created: /path/to/root.zone
[Recursor] Using existing root.zone file: /path/to/root.zone
[Recursor] Started periodic root.zone update (interval: 168h0m0s)
[RootZone] Checking for root.zone update...
[RootZone] root.zone updated successfully
```

### 故障处理

1. **下载失败**：使用现有文件继续运行
2. **验证失败**：保留旧文件，记录警告日志
3. **更新失败**：不影响当前服务，下次尝试

## 注意事项

1. **存储空间**：root.zone 文件大小约为 2-3MB
2. **网络访问**：需要能够访问 internic.net（如果需要下载）
3. **文件权限**：确保 recursor/data 目录有写入权限
4. **更新时间**：更新通常只需要几秒钟，对服务影响极小

## 故障排查

### 问题：root.zone 文件不存在

**检查步骤：**
1. 检查 recursor/data 目录权限
2. 检查网络连接
3. 查看日志中的错误信息

### 问题：Unbound 无法启动

**检查步骤：**
1. 确认 root.zone 文件存在
2. 确认文件权限为 0644
3. 使用 `unbound-checkconf` 验证配置
4. 查看配置文件中的路径是否正确

### 问题：root.zone 没有自动更新

**检查步骤：**
1. 确认 periodic update goroutine 正常运行
2. 检查日志中是否有更新尝试
3. 手动检查文件修改时间
4. 确认 RootZoneUpdateInterval 设置正确

## 性能影响

### 内存占用
- root.zone 加载后约占用 50-100MB 内存
- 对大多数服务器影响很小

### 查询性能
- 根域名查询完全本地化，延迟降至 1-2ms
- 减少对根服务器的查询压力

## 安全考虑

1. **下载源验证**：使用官方 internic.net 源
2. **文件完整性**：下载后进行格式和大小验证
3. **权限控制**：限制文件写入权限
4. **更新隔离**：使用临时文件确保原子性

## 未来改进方向

1. 支持多个镜像源下载（高可用）
2. 支持 DNSSEC 验证 root.zone
3. 支持自定义更新策略
4. 添加监控指标（最后更新时间、文件大小等）
5. 支持手动触发更新 API
"