"# Root.zone 实施指南

## 快速开始

### 1. 文件结构

系统会自动管理以下文件：

```
recursor/
├── data/
│   ├── root.key      # DNSSEC 信任锚
│   └── root.zone     # 根域名 zone 文件（自动下载）
└── manager_rootzone.go  # root.zone 管理逻辑
```

### 2. 主要功能

✅ **自动下载**：首次启动时，如果 root.zone 不存在，自动从官方源下载
✅ **不覆盖策略**：如果文件已存在且未过期，不会覆盖
✅ **定期更新**：每 7 天自动检查并更新 root.zone
✅ **原子更新**：使用临时文件确保更新过程不会损坏已有文件
✅ **权限管理**：自动设置文件权限为 0644
✅ **验证机制**：下载后验证文件格式和大小

### 3. Unbound 配置

系统自动在 Unbound 配置中添加：

```unbound
auth-zone:
    name: "."
    zonefile: "recursor/data/root.zone"
    for-downstream: yes
    fallback: yes
```

## 重要参数说明

### auth-zone 配置参数

| 参数 | 值 | 说明 |
|------|-----|------|
| name | \.\" | 表示根 zone |
| zonefile | 路径 | 本地 root.zone 文件路径 |
| for-downstream | yes | 允许提供给下游查询 |
| fallback | yes | 如果本地没有，允许递归查询（安全防护） |

注意：**不配置 master**，这样 Unbound 完全依赖本地文件，不会尝试 AXFR/IXFR 传输。

## 文件权限

```
root.zone: 0644 (rw-r--r--)
data/目录: 0755 (rwxr-xr-x)
```

这样可以确保：
- Unbound 进程可以读取和更新文件
- Unbound 可以创建临时文件
- 其他用户无法修改

## 启动流程

### 首次启动

1. **检查文件** → 不存在
2. **下载文件** → 从 `https://www.internic.net/domain/root.zone`
3. **验证文件** → 检查格式和大小
4. **生成配置** → 在 Unbound 配置中添加 auth-zone
5. **启动 Unbound** → 使用本地 root.zone
6. **启动定时更新** → 每 7 天检查一次

### 后续启动

1. **检查文件** → 已存在
2. **检查过期** → 未过期（未超过 7 天）
3. **使用现有文件** → 不覆盖
4. **正常启动** → 使用已有的 root.zone

### 自动更新

1. **定时触发** → 7 天后
2. **下载新版本** → 到临时文件 root.zone.tmp
3. **验证新文件** → 格式和大小检查
4. **原子替换** → 重命名 root.zone.tmp → root.zone
5. **自动生效** → Unbound 自动重新加载

## 可调参数

在 `recursor/manager_rootzone.go` 修改：

```go
const (
    // 更新源（一般不建议修改）
    RootZoneURL = \"https://www.internic.net/domain/root.zone\"
    
    // 更新间隔（推荐 7-14 天）
    RootZoneUpdateInterval = 7 * 24 * time.Hour
)
```

## 日志输出

系统在运行时会输出以下关键日志：

```
[Recursor] Ensuring root.zone file...
[Recursor] root.zone not found, downloading from https://www.internic.net/domain/root.zone
[Recursor] root.zone downloaded successfully
[Recursor] New root.zone file created: recursor/data/root.zone
[Recursor] Started periodic root.zone update (interval: 168h0m0s)
```

更新时的日志：

```
[RootZone] Checking for root.zone update...
[RootZone] root.zone is outdated, updating...
[RootZone] root.zone updated successfully
["