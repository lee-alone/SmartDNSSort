# Web 界面路径问题 - 解决方案

## 问题诊断

Web 界面返回 404 的原因是 **静态文件路径问题**。在 Debian 系统中，二进制文件位于不同的目录，导致相对路径 `./web` 找不到文件。

### ✅ 已解决的问题

1. **自动路径查找** - 代码现在按优先级自动查找多个位置
2. **自动文件复制** - 安装时自动将 web 文件复制到标准位置
3. **清晰的日志提示** - 启动时显示实际使用的 web 目录

## 新的架构

### 推荐的目录结构（Debian FHS 标准）

```
/var/lib/SmartDNSSort/
├── config.yaml          (配置文件)
├── cache.db             (缓存数据)
└── web/                 (Web 静态文件) ← 推荐位置
    ├── index.html
    ├── css/
    ├── js/
    └── ...
```

### 查找优先级（按顺序）

代码现在会按以下顺序查找 web 目录，**找到第一个包含 index.html 的就使用**：

1. `/var/lib/SmartDNSSort/web` ⭐ **Debian 服务器首选**
2. `/usr/share/smartdnssort/web` (FHS 标准)
3. `/etc/SmartDNSSort/web` (备选)
4. `./web` (开发环境)
5. `web` (开发环境)

## 代码改动说明

### 1. webapi/api.go - 改进的 Web 服务器启动

```go
// 新增 findWebDirectory() 方法
// 自动查找 Web 目录，支持多个可能的位置
func (s *Server) findWebDirectory() string {
    possiblePaths := []string{
        "/var/lib/SmartDNSSort/web",
        "/usr/share/smartdnssort/web",
        "/etc/SmartDNSSort/web",
        "./web",
        "web",
    }
    // 返回第一个包含 index.html 的目录
}

// 改进的 Start() 方法
// - API 路由在前注册（优先级高）
// - Web 文件服务在后注册
// - 自动查找 web 目录
// - 友好的错误提示
```

**优点**：
- ✅ 支持多种部署环境
- ✅ 自动检测文件位置
- ✅ 启动时清晰日志提示

### 2. sysinstall/installer.go - 安装时自动复制 Web 文件

```go
// 新增 CopyWebFiles() 方法
// 在安装时自动将 web 目录复制到 /var/lib/SmartDNSSort/web

// 新增 copyDirRecursive() 方法
// 递归复制目录树
```

**安装流程改进**：
```
CheckSystemd → CreateDirectories → GenerateDefaultConfig
→ CopyBinary → CopyWebFiles ← 新增
→ WriteServiceFile → ...
```

## 使用说明

### 在 Debian 系统上安装

```bash
# 下载二进制和 web 文件
wget https://github.com/lee-alone/SmartDNSSort/releases/SmartDNSSort
wget -r https://github.com/lee-alone/SmartDNSSort/raw/main/web

# 给权限
chmod +x SmartDNSSort

# 安装（web 文件会自动复制）
sudo ./SmartDNSSort -s install

# 验证 web 文件是否成功复制
ls -la /var/lib/SmartDNSSort/web/

# 启动后访问
curl http://localhost:8080/
```

### 启动日志示例

安装和启动时会看到类似日志：

```
[INFO] 复制 Web 文件到：/var/lib/SmartDNSSort/web
[INFO] 创建目录：/var/lib/SmartDNSSort/web
[INFO] 使用 web 目录: /var/lib/SmartDNSSort/web
Web API server started on http://localhost:8080
```

如果找不到 web 文件：

```
Warning: Could not find web directory. Web UI may not work properly.
Expected locations: /var/lib/SmartDNSSort/web, /usr/share/smartdnssort/web, or ./web
```

## 故障排查

### 检查 Web 文件是否成功复制

```bash
# 查看目录结构
ls -la /var/lib/SmartDNSSort/

# 检查 web 文件是否存在
ls -la /var/lib/SmartDNSSort/web/
ls -la /var/lib/SmartDNSSort/web/index.html

# 查看权限
stat /var/lib/SmartDNSSort/web/index.html
```

### 测试 API 是否工作

```bash
# 测试 API 端点
curl http://localhost:8080/api/stats
curl http://localhost:8080/health

# 测试静态文件服务
curl http://localhost:8080/index.html
curl http://localhost:8080/
```

### 查看启动日志

```bash
# 查看完整的启动日志
sudo journalctl -u SmartDNSSort -n 50

# 实时跟踪日志
sudo journalctl -u SmartDNSSort -f

# 查找 web 目录相关的日志
sudo journalctl -u SmartDNSSort | grep -i "web"
```

## 手动复制 Web 文件（如果安装脚本失败）

如果自动复制失败，可以手动执行：

```bash
# 创建目录
sudo mkdir -p /var/lib/SmartDNSSort/web

# 复制文件（假设 web 目录在当前目录）
sudo cp -r web/* /var/lib/SmartDNSSort/web/

# 设置权限
sudo chmod 755 /var/lib/SmartDNSSort/web
sudo chmod 644 /var/lib/SmartDNSSort/web/*

# 重启服务
sudo systemctl restart SmartDNSSort
```

## 文件权限

安装后的文件权限应该是：

```bash
$ ls -ld /var/lib/SmartDNSSort/web
drwxr-xr-x  3 root root 4096 Nov 15 10:00 /var/lib/SmartDNSSort/web

$ ls -la /var/lib/SmartDNSSort/web/
total 20
drwxr-xr-x  3 root root 4096 Nov 15 10:00 .
drwxr-xr-x  4 root root 4096 Nov 15 10:00 ..
-rw-r--r--  1 root root 5000 Nov 15 10:00 index.html
-rw-r--r--  1 root root  123 Nov 15 10:00 style.css
```

## 总结

| 功能 | 状态 | 说明 |
|------|------|------|
| 自动路径查找 | ✅ | 代码会自动查找 web 目录 |
| 自动文件复制 | ✅ | 安装时自动复制到标准位置 |
| 多环境支持 | ✅ | 开发/生产环境都支持 |
| 错误提示 | ✅ | 清晰的日志和错误信息 |
| FHS 兼容 | ✅ | 遵循 Linux 文件系统标准 |

---

**建议**: 在 Debian 系统上测试新版本，确认 Web 界面能正常加载！
