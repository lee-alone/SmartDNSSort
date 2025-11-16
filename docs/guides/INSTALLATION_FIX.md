# Web UI 自动安装修复说明

## 问题描述

在 Debian 系统上安装 SmartDNSSort 后，访问 Web UI 时出现 404 错误。用户需要手动将 `index.html` 复制到 `/var/lib/SmartDNSSort/web/` 目录才能正常使用。

## 根本原因

系统安装脚本在部署服务时：
1. 没有创建 `/var/lib/SmartDNSSort/web/` 目录
2. 没有将开发目录中的 Web 文件复制到安装位置

## 解决方案

### 1. 安装程序改进（sysinstall/installer.go）

#### 创建完整的目录结构
```go
dirs := []struct {
    path string
    mode os.FileMode
    desc string
}{
    {"/etc/SmartDNSSort", 0755, "配置目录"},
    {"/var/lib/SmartDNSSort", 0755, "数据目录"},
    {"/var/lib/SmartDNSSort/web", 0755, "Web UI 目录"},  // ← NEW
    {"/var/log/SmartDNSSort", 0755, "日志目录"},
}
```

#### 自动复制 Web 文件
`CopyWebFiles()` 方法会：
1. 查找源 Web 目录（开发位置）
2. 创建目标目录 `/var/lib/SmartDNSSort/web/`
3. 递归复制所有文件和目录结构

### 2. 程序启动改进（webapi/api.go）

#### 多层次路径查找
`findWebDirectory()` 现在按以下优先级查找 Web 文件：

```
1. /var/lib/SmartDNSSort/web          ← 服务安装位置（推荐）
2. /usr/share/smartdnssort/web        ← FHS 标准位置
3. /etc/SmartDNSSort/web              ← 备选位置
4. <可执行文件目录>/web               ← NEW：可执行文件所在目录
5. ./web                               ← 相对路径（开发环境）
6. web                                 ← 相对路径（开发环境）
```

#### 特点
- 自动发现可执行文件所在目录，直接使用其中的 Web 文件
- 即使安装不完整，程序仍可能找到 Web 文件
- 完整的错误处理和日志记录

## 安装流程现在包含

```
SmartDNSSort 服务安装程序
├── 检查权限（需要 root）
├── 检查 systemd 支持
├── ✓ 创建目录
│   ├── /etc/SmartDNSSort/
│   ├── /var/lib/SmartDNSSort/
│   ├── /var/lib/SmartDNSSort/web/   ← NEW
│   └── /var/log/SmartDNSSort/
├── 生成默认配置文件
├── 复制二进制文件到 /usr/local/bin/
├── ✓ 复制 Web 文件到 /var/lib/SmartDNSSort/web/  ← NEW
├── 写入 systemd 服务文件
├── 重新加载 systemd
├── 启用开机自启
└── 启动服务
```

## 测试验证

### 验证安装
```bash
# 查看 Web 目录是否存在
ls -la /var/lib/SmartDNSSort/web/

# 应该看到 index.html 和其他 Web 文件
# 例如：
# -rw-r--r-- 1 root root  12345 Nov 15 10:00 index.html
```

### 验证访问
```bash
# 访问 Web UI（默认 8080 端口）
curl http://localhost:8080/

# 应该返回 HTML 内容，而不是 404 错误
```

## 完整安装命令

```bash
# 1. 下载和编译（如果需要）
make build-linux-x64

# 2. 以 root 身份运行安装
sudo ./SmartDNSSort -s install

# 3. 检查服务状态
sudo systemctl status SmartDNSSort

# 4. 查看日志
sudo journalctl -u SmartDNSSort -f
```

## 卸载程序

卸载时会删除：
- `/etc/SmartDNSSort/` - 配置目录
- `/var/lib/SmartDNSSort/` - 数据目录（包括 Web 文件）
- `/var/log/SmartDNSSort/` - 日志目录
- `/usr/local/bin/SmartDNSSort` - 二进制文件

```bash
sudo ./SmartDNSSort -s uninstall
```

## 故障排除

### 问题：仍然无法访问 Web UI

**可能原因 1：服务未启动**
```bash
sudo systemctl status SmartDNSSort
sudo systemctl start SmartDNSSort
```

**可能原因 2：Web 文件未被复制**
```bash
# 检查文件是否存在
ls -la /var/lib/SmartDNSSort/web/

# 如果为空，手动复制（仅作为临时解决方案）
sudo cp -r ./web/* /var/lib/SmartDNSSort/web/
sudo systemctl restart SmartDNSSort
```

**可能原因 3：配置错误**
```bash
# 检查配置文件中 webui 是否启用
sudo cat /etc/SmartDNSSort/config.yaml | grep -A3 webui

# 应该看到：
# webui:
#   enabled: true
#   listen_port: 8080
```

**可能原因 4：防火墙阻止**
```bash
# 允许 8080 端口通过防火墙
sudo ufw allow 8080/tcp
```

## 更新日志

- **2025-11-15**: 
  - 修改 `sysinstall/installer.go`：添加 Web 目录创建
  - 修改 `webapi/api.go`：添加可执行文件目录查找
  - 安装流程现在自动复制 Web 文件到 `/var/lib/SmartDNSSort/web/`
