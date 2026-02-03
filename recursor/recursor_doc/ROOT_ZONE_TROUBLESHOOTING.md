# Root.Zone 文件故障排查指南

## 快速诊断

### 1. 检查文件是否存在

**Linux**：
```bash
# 检查文件
ls -lh /etc/unbound/root.zone

# 如果不存在，应该看到：
# ls: cannot access '/etc/unbound/root.zone': No such file or directory

# 如果存在，应该看到：
# -rw-r--r-- 1 root root 2.0M Feb  3 10:14 /etc/unbound/root.zone
```

**Windows**：
```cmd
# 检查文件
dir unbound\root.zone

# 如果不存在，应该看到：
# File Not Found

# 如果存在，应该看到：
# 02/03/2026  10:14 AM       2,097,152 root.zone
```

### 2. 检查应用日志

```bash
# 查看启动日志中关于 root.zone 的信息
grep -i "root.zone" /var/log/smartdnssort.log

# 应该看到类似的日志：
# [Recursor] Ensuring root.zone file...
# [Recursor] Using existing root.zone file: /etc/unbound/root.zone
# [Recursor] Unbound will automatically sync root.zone from root servers
```

### 3. 检查 unbound 配置

```bash
# Linux
grep -A 15 "auth-zone" /etc/unbound/unbound.conf.d/smartdnssort.conf

# Windows
grep -A 15 "auth-zone" unbound\unbound.conf
```

**应该看到**：
```unbound
auth-zone:
    name: "."
    zonefile: "/etc/unbound/root.zone"
    primary: 192.0.32.132
    ...
```

## 常见问题及解决方案

### 问题 1：文件不存在

**症状**：
```
[ERROR] Failed to ensure root.zone file: root.zone not found in embedded data
```

**原因**：
- 应用编译时未包含 root.zone 文件
- 网络下载失败

**解决方案**：

1. **检查网络连接**
   ```bash
   # 测试网络
   ping 8.8.8.8
   
   # 测试 DNS
   nslookup www.internic.net
   ```

2. **手动下载**
   ```bash
   # Linux
   sudo mkdir -p /etc/unbound
   sudo curl -o /etc/unbound/root.zone https://www.internic.net/domain/root.zone
   
   # Windows
   mkdir unbound
   curl -o unbound\root.zone https://www.internic.net/domain/root.zone
   ```

3. **检查文件大小**
   ```bash
   # 应该是 2-3 MB
   ls -lh /etc/unbound/root.zone
   ```

4. **重启应用**
   ```bash
   systemctl restart smartdnssort
   ```

### 问题 2：权限不足

**症状**：
```
[ERROR] Failed to write root.zone: permission denied
```

**原因**：
- 应用没有写入权限
- 目录权限不正确

**解决方案**：

**Linux**：
```bash
# 检查目录权限
ls -la /etc/unbound/

# 如果目录不存在，创建它
sudo mkdir -p /etc/unbound
sudo chmod 755 /etc/unbound

# 如果文件存在但权限不对，修改权限
sudo chmod 644 /etc/unbound/root.zone
sudo chown root:root /etc/unbound/root.zone

# 如果应用以特定用户运行，确保该用户有读权限
sudo usermod -a -G unbound smartdnssort
```

**Windows**：
```cmd
# 检查目录权限
icacls unbound

# 授予当前用户完全权限
icacls unbound /grant:r "%USERNAME%":F

# 授予 SYSTEM 完全权限
icacls unbound /grant:r SYSTEM:F
```

### 问题 3：文件损坏

**症状**：
```
[ERROR] root.zone validation failed: invalid root.zone format
```

**原因**：
- 下载不完整
- 文件被修改
- 磁盘错误

**解决方案**：

1. **检查文件大小**
   ```bash
   # Linux
   ls -lh /etc/unbound/root.zone
   
   # Windows
   dir unbound\root.zone
   ```

2. **检查文件内容**
   ```bash
   # Linux
   head -20 /etc/unbound/root.zone
   
   # Windows
   type unbound\root.zone | more
   ```

3. **删除并重新下载**
   ```bash
   # Linux
   sudo rm /etc/unbound/root.zone
   
   # Windows
   del unbound\root.zone
   
   # 重启应用
   systemctl restart smartdnssort
   ```

### 问题 4：Unbound 无法读取文件

**症状**：
```
[ERROR] unbound: error reading zonefile
```

**原因**：
- 文件路径不正确
- 文件权限不足
- unbound 进程权限不足

**解决方案**：

1. **验证文件路径**
   ```bash
   # 检查 unbound 配置中的路径
   grep "zonefile:" /etc/unbound/unbound.conf.d/smartdnssort.conf
   
   # 确保文件存在于该路径
   ls -la /etc/unbound/root.zone
   ```

2. **检查 unbound 权限**
   ```bash
   # Linux - 检查 unbound 用户
   ps aux | grep unbound
   
   # 确保 unbound 用户可读文件
   sudo chmod 644 /etc/unbound/root.zone
   ```

3. **测试 unbound 配置**
   ```bash
   # 验证配置文件语法
   unbound-checkconf /etc/unbound/unbound.conf.d/smartdnssort.conf
   
   # 如果有错误，查看详细信息
   unbound -c /etc/unbound/unbound.conf.d/smartdnssort.conf -d
   ```

### 问题 5：网络下载超时

**症状**：
```
[WARN] Failed to ensure root.zone file: timeout waiting for root.zone download
```

**原因**：
- 网络连接慢
- 根服务器响应慢
- 防火墙阻止

**解决方案**：

1. **检查网络连接**
   ```bash
   # 测试连接
   ping www.internic.net
   
   # 测试 DNS
   nslookup www.internic.net
   ```

2. **测试下载**
   ```bash
   # 直接下载测试
   curl -I https://www.internic.net/domain/root.zone
   
   # 应该返回 200 OK
   ```

3. **检查防火墙**
   ```bash
   # Linux
   sudo iptables -L | grep 443
   
   # Windows
   netsh advfirewall show allprofiles
   ```

4. **增加超时时间**
   - 编辑代码中的 `DownloadTimeout` 常量
   - 默认值：30 秒
   - 可增加到 60 秒

## 诊断脚本

### Linux 诊断脚本

```bash
#!/bin/bash

echo "=== Root.Zone 诊断 ==="
echo

echo "1. 检查文件存在性"
if [ -f /etc/unbound/root.zone ]; then
    echo "✓ 文件存在"
    ls -lh /etc/unbound/root.zone
else
    echo "✗ 文件不存在"
fi
echo

echo "2. 检查文件大小"
SIZE=$(stat -f%z /etc/unbound/root.zone 2>/dev/null || stat -c%s /etc/unbound/root.zone 2>/dev/null)
if [ -n "$SIZE" ]; then
    echo "✓ 文件大小: $SIZE 字节"
    if [ $SIZE -lt 100000 ]; then
        echo "✗ 文件过小（< 100KB）"
    elif [ $SIZE -gt 10485760 ]; then
        echo "✗ 文件过大（> 10MB）"
    else
        echo "✓ 文件大小正常"
    fi
else
    echo "✗ 无法获取文件大小"
fi
echo

echo "3. 检查文件内容"
if grep -q "SOA" /etc/unbound/root.zone 2>/dev/null; then
    echo "✓ 文件包含 SOA 记录"
else
    echo "✗ 文件不包含 SOA 记录"
fi

if grep -q "NS" /etc/unbound/root.zone 2>/dev/null; then
    echo "✓ 文件包含 NS 记录"
else
    echo "✗ 文件不包含 NS 记录"
fi
echo

echo "4. 检查权限"
ls -la /etc/unbound/root.zone
echo

echo "5. 检查 unbound 配置"
if grep -q "zonefile.*root.zone" /etc/unbound/unbound.conf.d/smartdnssort.conf 2>/dev/null; then
    echo "✓ 配置中包含 root.zone"
    grep -A 2 "zonefile" /etc/unbound/unbound.conf.d/smartdnssort.conf
else
    echo "✗ 配置中不包含 root.zone"
fi
```

### Windows 诊断脚本

```powershell
# Root.Zone 诊断脚本

Write-Host "=== Root.Zone 诊断 ===" -ForegroundColor Green
Write-Host

Write-Host "1. 检查文件存在性"
if (Test-Path "unbound\root.zone") {
    Write-Host "✓ 文件存在" -ForegroundColor Green
    Get-Item "unbound\root.zone" | Format-List FullName, Length, LastWriteTime
} else {
    Write-Host "✗ 文件不存在" -ForegroundColor Red
}
Write-Host

Write-Host "2. 检查文件大小"
$file = Get-Item "unbound\root.zone" -ErrorAction SilentlyContinue
if ($file) {
    $size = $file.Length
    Write-Host "✓ 文件大小: $size 字节"
    if ($size -lt 100000) {
        Write-Host "✗ 文件过小（< 100KB）" -ForegroundColor Red
    } elseif ($size -gt 10485760) {
        Write-Host "✗ 文件过大（> 10MB）" -ForegroundColor Red
    } else {
        Write-Host "✓ 文件大小正常" -ForegroundColor Green
    }
} else {
    Write-Host "✗ 无法获取文件大小" -ForegroundColor Red
}
```

## 总结

| 问题 | 症状 | 解决方案 |
|------|------|---------|
| 文件不存在 | 启动失败 | 手动下载或检查网络 |
| 权限不足 | Permission denied | 修改目录/文件权限 |
| 文件损坏 | 验证失败 | 删除文件，重启应用 |
| 无法读取 | unbound 错误 | 检查路径和权限 |
| 下载超时 | 超时错误 | 检查网络，增加超时 |

## 获取帮助

如果问题仍未解决，请收集以下信息：

1. **应用日志**
   ```bash
   grep -i "root.zone\|recursor" /var/log/smartdnssort.log
   ```

2. **系统信息**
   ```bash
   uname -a
   ```

3. **unbound 版本**
   ```bash
   unbound -V
   ```

4. **网络信息**
   ```bash
   ping www.internic.net
   ```

5. **文件信息**
   ```bash
   ls -lh /etc/unbound/root.zone
   file /etc/unbound/root.zone
   ```
