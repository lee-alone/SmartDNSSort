# Root.Zone 文件位置说明

## 概述

root.zone 文件的存储位置取决于操作系统平台。

## 平台特定位置

### Linux

**位置**：`/etc/unbound/root.zone`

**说明**：
- 使用系统标准目录
- 与系统 unbound 配置目录一致
- 需要 root 权限才能写入

**验证方法**：
```bash
ls -la /etc/unbound/root.zone
```

**预期输出**：
```
-rw-r--r-- 1 root root 2097152 Feb  3 10:14 /etc/unbound/root.zone
```

### Windows

**位置**：`unbound/root.zone`（相对于程序工作目录）

**说明**：
- 使用程序目录下的 unbound 子目录
- 与 unbound.exe 和 unbound.conf 在同一目录
- 无需特殊权限

**验证方法**：
```cmd
dir unbound\root.zone
```

**预期输出**：
```
02/03/2026  10:14 AM       2,097,152 root.zone
```

## 文件来源

### 初始化

root.zone 文件在应用启动时由 `RootZoneManager.EnsureRootZone()` 创建：

1. **检查文件是否存在**
   - 如果存在且大小正确，使用现有文件
   - 如果不存在或大小不正确，下载新文件

2. **下载文件**
   - 源：https://www.internic.net/domain/root.zone
   - 最大重试次数：3 次
   - 超时时间：30 秒

3. **验证文件**
   - 检查文件大小（最小 100KB，最大 10MB）
   - 检查文件格式（必须包含 $ORIGIN 或 $TTL）
   - 检查必要记录（SOA 和 NS）

### 自动更新

启动后，unbound 会自动从根服务器同步 root.zone：

```unbound
auth-zone:
    name: "."
    zonefile: "/etc/unbound/root.zone"  # Linux
    # 或
    zonefile: "unbound/root.zone"       # Windows
    
    primary: 192.0.32.132      # 根服务器
    primary: 192.0.47.132      # 根服务器
    primary: 2001:500:12::d0d  # 根服务器 IPv6
    primary: 2001:500:1::53    # 根服务器 IPv6
    
    fallback-enabled: yes      # 网络故障时回退
```

## 代码实现

### RootZoneManager 初始化

**文件**：`recursor/manager_rootzone.go`

```go
func NewRootZoneManager() *RootZoneManager {
    var configDir string
    
    if runtime.GOOS == "linux" {
        // Linux 上使用系统目录
        configDir = "/etc/unbound"
    } else {
        // Windows 和其他平台使用程序目录
        configDir, _ = GetUnboundConfigDir()  // "unbound"
    }
    
    return &RootZoneManager{
        dataDir:      configDir,
        rootZonePath: filepath.Join(configDir, "root.zone"),
        client:       &http.Client{Timeout: 30 * time.Second},
    }
}
```

### 文件路径获取

**方法**：`RootZoneManager.rootZonePath`

- Linux：`/etc/unbound/root.zone`
- Windows：`unbound/root.zone`

### 配置生成

**方法**：`RootZoneManager.GetRootZoneConfig()`

生成的 unbound 配置会使用正确的平台特定路径：

```unbound
# Linux
zonefile: "/etc/unbound/root.zone"

# Windows
zonefile: "unbound/root.zone"
```

## 故障排查

### 问题：找不到 root.zone 文件

**检查步骤**：

1. **确认平台**
   ```bash
   # Linux
   uname -s
   
   # Windows
   systeminfo | findstr /I "OS"
   ```

2. **检查正确的位置**
   ```bash
   # Linux
   ls -la /etc/unbound/root.zone
   
   # Windows
   dir unbound\root.zone
   ```

3. **检查应用日志**
   ```bash
   # 查看启动日志
   grep -i "root.zone" application.log
   ```

4. **检查权限**
   ```bash
   # Linux - 检查目录权限
   ls -la /etc/unbound/
   
   # Windows - 检查目录权限
   icacls unbound
   ```

### 问题：文件存在但 unbound 无法读取

**原因**：权限问题

**解决方案**：

```bash
# Linux - 确保 unbound 用户可读
sudo chmod 644 /etc/unbound/root.zone
sudo chown root:root /etc/unbound/root.zone

# Windows - 确保程序有读写权限
icacls unbound /grant:r "%USERNAME%":F
```

### 问题：文件大小异常

**检查方法**：
```bash
# Linux
ls -lh /etc/unbound/root.zone

# Windows
dir unbound\root.zone
```

**预期大小**：2-3 MB

**异常情况**：
- 文件过小（< 100KB）：下载不完整
- 文件过大（> 10MB）：文件损坏

**解决方案**：
```bash
# 删除文件，重启应用会重新下载
rm /etc/unbound/root.zone  # Linux
del unbound\root.zone      # Windows
```

## 文件内容

### 格式

root.zone 是标准的 DNS zone 文件格式：

```
$ORIGIN .
$TTL 518400
.   IN  SOA ns.icann.org. nstld.icann.org. (
            2026020301  ; serial
            1800        ; refresh
            900         ; retry
            1814400     ; expire
            86400 )     ; minimum

.   IN  NS  a.root-servers.net.
.   IN  NS  b.root-servers.net.
...
```

### 大小

- 典型大小：2-3 MB
- 包含所有根域名服务器的 A 和 AAAA 记录
- 包含 DNSSEC 签名

## 监控

### 检查文件更新时间

```bash
# Linux
stat /etc/unbound/root.zone | grep Modify

# Windows
dir unbound\root.zone | findstr root.zone
```

### 监控 unbound 日志

```bash
# Linux
tail -f /var/log/unbound.log | grep "auth-zone"

# Windows
# 查看 unbound 启动日志
```

## 总结

| 方面 | Linux | Windows |
|------|-------|---------|
| 位置 | `/etc/unbound/root.zone` | `unbound/root.zone` |
| 权限 | 需要 root | 需要程序权限 |
| 大小 | 2-3 MB | 2-3 MB |
| 更新 | unbound 自动 | unbound 自动 |
| 初始化 | 应用启动时 | 应用启动时 |

root.zone 文件由应用在启动时初始化，之后由 unbound 自动管理更新。
