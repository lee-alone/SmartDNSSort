# 权限检查和自动修复功能

## 概述

启动时自动检查关键文件和目录的读写权限，如果权限不足则自动修改，确保 unbound 能正常运行。

## 检查的文件和目录

### Linux

| 路径 | 类型 | 检查项 | 修复权限 |
|------|------|--------|---------|
| `/etc/unbound` | 目录 | 读、写、执行 | 0755 |
| `/etc/unbound/root.key` | 文件 | 读、写 | 0644 |
| `/etc/unbound/unbound.conf.d/smartdnssort.conf` | 文件 | 读、写 | 0644 |
| `/etc/unbound/root.zone` | 文件 | 读、写 | 0644 |

### Windows

| 路径 | 类型 | 检查项 | 修复权限 |
|------|------|--------|---------|
| `unbound` | 目录 | 读、写 | 0777 |
| `unbound/root.key` | 文件 | 读、写 | 0666 |
| `unbound/unbound.conf` | 文件 | 读、写 | 0666 |
| `unbound/root.zone` | 文件 | 读、写 | 0666 |

## 工作流程

### 启动时

```
应用启动
  ↓
检查 /etc/unbound 目录权限
  ├─ 权限正常 → 继续
  └─ 权限不足 → 修改为 0755
  ↓
检查 root.key 文件权限
  ├─ 权限正常 → 继续
  └─ 权限不足 → 修改为 0644
  ↓
生成配置文件
  ↓
检查配置文件权限
  ├─ 权限正常 → 继续
  └─ 权限不足 → 修改为 0644
  ↓
启动 unbound
```

## 日志输出

### 权限正常

```
[DEBUG] [Permission] File permissions OK: /etc/unbound/root.key (mode: 0644)
[DEBUG] [Permission] Directory permissions OK: /etc/unbound (mode: 0755)
```

### 权限不足且修复成功

```
[WARN] [Permission] Permission issue detected for /etc/unbound/root.key, attempting to fix...
[INFO] [Permission] File permissions fixed: /etc/unbound/root.key (mode: 0644)
```

### 权限修复失败

```
[WARN] [Permission] Permission issue detected for /etc/unbound/root.key, attempting to fix...
[ERROR] [Permission] Failed to fix permissions: permission denied
[WARN] [Recursor] Root key permission issue: failed to fix file permissions: permission denied
```

## 权限检查方法

### 文件权限检查

1. **读权限**：尝试打开文件进行读取
2. **写权限**：尝试打开文件进行写入

### 目录权限检查

1. **读权限**：尝试列出目录内容
2. **写权限**：尝试在目录中创建临时文件

## 权限修复

### Linux

- **文件**：修改为 0644（所有者可读写，其他用户只读）
- **目录**：修改为 0755（所有者可读写执行，其他用户可读执行）

### Windows

- **文件**：修改为 0666（所有用户可读写）
- **目录**：修改为 0777（所有用户可读写执行）

## 常见问题

### Q: 为什么需要权限检查？

A: 确保 unbound 进程能够：
- 读取 root.key 和 root.zone 文件
- 写入日志和临时文件
- 访问配置文件

### Q: 如果权限修复失败怎么办？

A: 通常是因为：
1. 应用没有足够的权限修改文件
2. 文件系统是只读的
3. SELinux 或 AppArmor 限制

**解决方案**：
```bash
# Linux - 手动修改权限
sudo chmod 644 /etc/unbound/root.key
sudo chmod 755 /etc/unbound

# Windows - 以管理员身份运行应用
```

### Q: 临时文件 `.smartdnssort_perm_check` 是什么？

A: 这是权限检查时创建的临时文件，用于验证目录的写权限。检查完成后会自动删除。

### Q: 权限修复会影响 unbound 的安全性吗？

A: 不会。修复后的权限是标准的 unbound 配置：
- Linux：0644 文件，0755 目录
- Windows：0666 文件，0777 目录

这些权限允许 unbound 进程正常运行，同时保持合理的安全性。

## 故障排查

### 问题：权限检查失败

**日志**：
```
[WARN] [Permission] Permission issue detected for /etc/unbound/root.key
[ERROR] [Permission] Failed to fix permissions: permission denied
```

**原因**：应用没有足够的权限修改文件

**解决方案**：
```bash
# 以 root 身份运行应用
sudo systemctl restart smartdnssort

# 或手动修改权限
sudo chmod 644 /etc/unbound/root.key
sudo chmod 755 /etc/unbound
```

### 问题：目录权限检查失败

**日志**：
```
[WARN] [Permission] Permission issue detected for directory /etc/unbound
```

**原因**：目录权限不足

**解决方案**：
```bash
# 修改目录权限
sudo chmod 755 /etc/unbound

# 或重新创建目录
sudo rm -rf /etc/unbound
sudo mkdir -p /etc/unbound
sudo chmod 755 /etc/unbound
```

## 实现细节

### Linux 权限检查函数

```go
// 检查文件权限
checkFilePermissions(filePath string) error

// 检查目录权限
checkDirectoryPermissions(dirPath string) error

// 检查文件读权限
checkReadPermission(filePath string) error

// 检查文件写权限
checkWritePermission(filePath string) error

// 检查目录读权限
checkDirectoryReadPermission(dirPath string) error

// 检查目录写权限
checkDirectoryWritePermission(dirPath string) error
```

### Windows 权限检查函数

```go
// 检查文件权限
checkFilePermissionsWindows(filePath string) error

// 检查目录权限
checkDirectoryPermissionsWindows(dirPath string) error

// 检查文件读权限
checkReadPermissionWindows(filePath string) error

// 检查文件写权限
checkWritePermissionWindows(filePath string) error

// 检查目录读权限
checkDirectoryReadPermissionWindows(dirPath string) error

// 检查目录写权限
checkDirectoryWritePermissionWindows(dirPath string) error
```

## 总结

权限检查和自动修复功能：

✅ **自动检查** - 启动时自动检查关键文件和目录
✅ **自动修复** - 权限不足时自动修改
✅ **详细日志** - 记录检查和修复过程
✅ **平台特定** - Linux 和 Windows 分别处理
✅ **非侵入式** - 只检查和修复必要的权限

这确保了 unbound 能够正常运行，同时保持系统的安全性。
