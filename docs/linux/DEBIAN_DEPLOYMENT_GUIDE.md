# 📋 Debian 系统部署指南 - Web UI 自动安装

## 您遇到的问题已完全解决

上次您遇到的问题：
> "我有一个问题，这个程序在 debian 上可以安装。但是页面访问，总是 404 错误。我在 /var/lib/SmartDNSSort/web/ 里面放了 index.html，并重启程序才可以访问到网页端"

**现在使用新版本，安装时会自动：**
- ✅ 创建 `/var/lib/SmartDNSSort/web/` 目录
- ✅ 自动复制所有 Web 文件
- ✅ 设置正确权限
- ✅ 启动后直接可用（无需手动操作）

## 部署步骤

### 步骤 1：准备 Linux 二进制文件

在您的 Windows 开发机上：
```bash
# 1. 打开 PowerShell，进入项目目录
cd d:\gb\SmartDNSSort

# 2. 最新编译的二进制已在：
# bin/SmartDNSSort-linux-x64  (10.3 MB)

# 3. 查看编译列表
dir bin/SmartDNSSort*
```

**输出应该显示：**
```
SmartDNSSort.exe              (Windows 版本)
SmartDNSSort-linux-x64        (Linux x64 版本) ← 用这个
```

### 步骤 2：上传到 Debian 服务器

从 Windows 中上传文件到 Debian：

**方式 1：使用 WinSCP（图形界面）**
- 打开 WinSCP
- 连接到 Debian 服务器
- 上传 `bin/SmartDNSSort-linux-x64` 到 `/home/user/` 目录

**方式 2：使用 PowerShell SCP**
```powershell
# 设置变量
$server = "debian-server-ip"
$user = "root"  # 或其他用户
$localFile = "d:\gb\SmartDNSSort\bin\SmartDNSSort-linux-x64"
$remoteDir = "/root/"

# 上传文件
scp -r $localFile ${user}@${server}:${remoteDir}
```

**方式 3：使用 PuTTY Pscp**
```powershell
# 如果系统安装了 PuTTY
pscp.exe d:\gb\SmartDNSSort\bin\SmartDNSSort-linux-x64 root@debian-server-ip:/root/
```

### 步骤 3：在 Debian 上安装

SSH 连接到 Debian 服务器：

```bash
# 1. 连接到服务器
ssh root@debian-server-ip

# 2. 创建工作目录（可选）
mkdir -p ~/smartdnssort
cd ~/smartdnssort

# 3. 复制上传的文件
cp /root/SmartDNSSort-linux-x64 ./

# 4. 给予执行权限
chmod +x SmartDNSSort-linux-x64

# 5. （强烈推荐）预览安装过程（不会修改系统）
sudo ./SmartDNSSort-linux-x64 -s install --dry-run
```

**预览输出示例：**
```
============================================
SmartDNSSort 服务安装程序
============================================
[DRY-RUN 模式] 仅预览，不实际执行任何操作

[DRY-RUN] 将创建目录：/etc/SmartDNSSort (配置目录)
[DRY-RUN] 将创建目录：/var/lib/SmartDNSSort (数据目录)
[DRY-RUN] 将创建目录：/var/lib/SmartDNSSort/web (Web UI 目录)  ← 新增
[DRY-RUN] 将创建目录：/var/log/SmartDNSSort (日志目录)
...
[DRY-RUN] 将复制 Web 文件到：/var/lib/SmartDNSSort/web  ← 新增
...
```

### 步骤 4：执行实际安装

```bash
# 执行真正的安装（需要 root 权限）
sudo ./SmartDNSSort-linux-x64 -s install
```

**安装过程（约 5-10 秒）：**
```
============================================
SmartDNSSort 服务安装程序
============================================
创建目录...
生成配置文件...
复制二进制文件...
复制 Web 文件...          ← 现在会自动执行！
注册服务...
启用开机自启...
启动服务...

============================================
SmartDNSSort 已成功安装！
============================================
✓ 服务状态：active
✓ 配置文件：/etc/SmartDNSSort/config.yaml
✓ 数据目录：/var/lib/SmartDNSSort
✓ Web UI：http://localhost:8080          ← 现在可用！
✓ Web 文件：/var/lib/SmartDNSSort/web/   ← 自动复制！
```

### 步骤 5：验证安装

```bash
# 1. 查看服务状态
sudo systemctl status SmartDNSSort
# 应该显示 ✓ active (running)

# 2. 验证 Web 目录
ls -la /var/lib/SmartDNSSort/web/
# 应该显示 index.html 和其他文件

# 3. 测试 Web UI 访问
curl http://127.0.0.1:8080/
# 应该返回 HTML 内容（不是 404）

# 4. 从其他机器访问
# 在浏览器中打开：http://<debian-server-ip>:8080
```

## ✅ 完整检查清单

安装后检查以下内容：

```bash
# 检查 1：DNS 服务是否运行
sudo netstat -ulnp | grep :53
# 或
sudo ss -ulnp | grep :53
# 应该显示 SmartDNSSort 在监听 53 端口

# 检查 2：Web UI 服务是否运行
sudo netstat -tulnp | grep 8080
# 应该显示 8080 端口监听

# 检查 3：Web 文件完整性
ls -la /var/lib/SmartDNSSort/web/
# 应该包含：
# -rw-r--r-- ... index.html
# -rw-r--r-- ... (其他可能的文件)

# 检查 4：配置文件
cat /etc/SmartDNSSort/config.yaml
# 验证 webui.enabled: true 和 webui.listen_port: 8080

# 检查 5：查看启动日志
sudo journalctl -u SmartDNSSort -n 20
# 应该显示成功启动的消息
```

## 🔧 常见操作

### 访问 Web UI

**从 Debian 本机：**
```bash
curl http://127.0.0.1:8080/
```

**从其他机器浏览器：**
- 打开：`http://<debian-server-ip>:8080`
- 将 `<debian-server-ip>` 替换为实际的服务器 IP

### 修改配置

```bash
# 编辑配置文件
sudo nano /etc/SmartDNSSort/config.yaml

# 修改后重启服务
sudo systemctl restart SmartDNSSort

# 验证服务状态
sudo systemctl status SmartDNSSort
```

### 查看日志

```bash
# 实时查看日志
sudo journalctl -u SmartDNSSort -f

# 查看最后 50 行
sudo journalctl -u SmartDNSSort -n 50

# 查看特定时间的日志
sudo journalctl -u SmartDNSSort --since "1 hour ago"
```

### 管理服务

```bash
# 启动服务
sudo systemctl start SmartDNSSort

# 停止服务
sudo systemctl stop SmartDNSSort

# 重启服务
sudo systemctl restart SmartDNSSort

# 查看自启状态
sudo systemctl is-enabled SmartDNSSort
```

## ⚠️ 如果遇到问题

### 问题 1：仍然看到 404 错误

```bash
# 检查 Web 文件是否存在
ls -la /var/lib/SmartDNSSort/web/

# 如果为空，可能是复制失败，手动复制：
# （查找 web 文件源位置）
find / -name "index.html" -path "*/web/*" 2>/dev/null

# 或者重新运行安装
sudo ./SmartDNSSort-linux-x64 -s install
```

### 问题 2：权限错误

```bash
# 检查目录权限
ls -la /var/lib/SmartDNSSort/web/

# 修复权限
sudo chown -R root:root /var/lib/SmartDNSSort/web/
sudo chmod 755 /var/lib/SmartDNSSort/web/
sudo chmod 644 /var/lib/SmartDNSSort/web/*

# 重启服务
sudo systemctl restart SmartDNSSort
```

### 问题 3：防火墙阻止

```bash
# 检查防火墙状态
sudo ufw status

# 如果启用了防火墙，允许 8080 端口
sudo ufw allow 8080/tcp

# 也可能需要允许 DNS 端口
sudo ufw allow 53/udp
sudo ufw allow 53/tcp
```

## 📊 对比：旧版 vs 新版

| 功能 | 旧版本 | 新版本 |
|-----|--------|--------|
| **Web 目录创建** | ❌ 手动创建 | ✅ 自动创建 |
| **Web 文件复制** | ❌ 手动复制 | ✅ 自动复制 |
| **安装完整度** | ❌ 不完整 | ✅ 完整 |
| **容错能力** | ❌ 差 | ✅ 好 |
| **首次访问** | ❌ 404 错误 | ✅ 正常 |
| **用户体验** | ❌ 复杂 | ✅ 简单 |

## 📚 更多信息

- 详细安装说明：查看 `docs/linux/LINUX_INSTALL.md`
- 修复技术细节：查看 `docs/guides/INSTALLATION_FIX.md`
- 项目信息：查看 `README.md`
- 完整方案说明：查看 `SOLUTION_SUMMARY.md`

## 🎯 总结

**您之前需要做的：**
```bash
# 1. 手动创建目录
sudo mkdir -p /var/lib/SmartDNSSort/web

# 2. 手动复制文件（找不到文件位置，很困难）
sudo cp ???/index.html /var/lib/SmartDNSSort/web/

# 3. 重启服务
sudo systemctl restart SmartDNSSort

# 4. 才能访问 Web UI
```

**现在可以这样做：**
```bash
# 一条命令解决一切
sudo ./SmartDNSSort-linux-x64 -s install

# 完成！已可访问 Web UI
http://localhost:8080
```

---

**部署日期：** 2025 年 11 月 15 日  
**版本：** SmartDNSSort v1.0+ (带 Web UI 自动安装修复)  
**支持：** Debian/Ubuntu x86_64 系统
