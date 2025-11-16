# SmartDNSSort Linux 安装指南

## 目录
1. [系统要求](#系统要求)
2. [快速安装](#快速安装)
3. [详细安装步骤](#详细安装步骤)
4. [配置管理](#配置管理)
5. [服务管理](#服务管理)
6. [日志查看](#日志查看)
7. [故障排除](#故障排除)
8. [卸载](#卸载)

## 系统要求

### 操作系统
- **支持系统**: Debian、Ubuntu、Fedora 等主流 Linux 发行版
- **要求**: systemd 服务管理器（现代 Linux 系统标配）
- **权限**: 需要 root 或 sudo 权限

### 硬件要求
- **CPU**: 1+ 核心
- **内存**: 最小 128MB，建议 256MB+
- **存储**: 最小 50MB

### 网络要求
- **DNS 端口**: 53/UDP（DNS 查询）、53/TCP（大型查询）
- **Web UI 端口**: 8080（可自定义）
- **上游 DNS**: 至少需要 2 个可用的上游 DNS 服务器

## 快速安装

### 方式 1：使用安装脚本（推荐）

```bash
# 1. 下载二进制文件和安装脚本
wget https://github.com/lee-alone/SmartDNSSort/releases/download/v1.0/SmartDNSSort
wget https://github.com/lee-alone/SmartDNSSort/releases/download/v1.0/install.sh

# 2. 给脚本加执行权限
chmod +x SmartDNSSort install.sh

# 3. 执行安装（推荐先预览）
sudo ./install.sh --dry-run

# 4. 正式安装
sudo ./install.sh

# 5. 查看安装结果
sudo systemctl status SmartDNSSort
```

### 方式 2：直接使用二进制命令

```bash
# 1. 下载二进制文件
wget https://github.com/lee-alone/SmartDNSSort/releases/download/v1.0/SmartDNSSort
chmod +x SmartDNSSort

# 2. 预览安装流程
sudo ./SmartDNSSort -s install --dry-run

# 3. 执行安装
sudo ./SmartDNSSort -s install

# 4. 查看安装结果
./SmartDNSSort -s status
```

## 详细安装步骤

### 步骤 1：检查系统环境

```bash
# 检查 systemd 是否可用
systemctl --version

# 检查 root 权限（安装需要）
sudo whoami
# 应该输出：root
```

### 步骤 2：下载安装文件

**选择合适的架构：**
- **x86_64**（大多数服务器）: `SmartDNSSort` 或 `SmartDNSSort-amd64`
- **ARM64**（树莓派 4B+、云服务器）: `SmartDNSSort-arm64`
- **ARM32**（旧树莓派）: `SmartDNSSort-armv7`

```bash
# 创建安装目录
mkdir -p ~/smartdnssort
cd ~/smartdnssort

# 下载（根据你的架构选择）
wget https://github.com/lee-alone/SmartDNSSort/releases/download/v1.0/SmartDNSSort

# 给执行权限
chmod +x SmartDNSSort
```

### 步骤 3：预览安装流程（强烈推荐）

```bash
# 预览默认安装
sudo ./SmartDNSSort -s install --dry-run

# 预览自定义配置的安装
sudo ./SmartDNSSort -s install \
  -c /custom/path/config.yaml \
  -w /custom/path/data \
  -user smartdns \
  --dry-run
```

**干运行模式会显示：**
- 将创建的目录
- 将写入的文件
- 将执行的系统命令
- 不会实际修改任何系统设置

### 步骤 4：执行安装

```bash
# 标准安装（使用默认路径）
sudo ./SmartDNSSort -s install

# 或者使用脚本安装
sudo ./install.sh
```

**安装过程会：**
1. 创建系统目录（/etc/SmartDNSSort、/var/lib/SmartDNSSort、/var/log/SmartDNSSort）
2. 复制二进制文件到 /usr/local/bin/SmartDNSSort
3. 生成默认配置文件
4. 创建 systemd 服务文件
5. 启用服务开机自启
6. 启动服务

### 步骤 5：验证安装

```bash
# 查看服务状态
./SmartDNSSort -s status

# 或使用 systemctl
sudo systemctl status SmartDNSSort

# 验证 DNS 服务是否运行
sudo netstat -ulnp | grep 53
# 或（systemd-free 系统）
sudo ss -ulnp | grep 53

# 测试 DNS 查询
dig @127.0.0.1 www.google.com
```

## 配置管理

### 默认配置路径
```
/etc/SmartDNSSort/config.yaml
```

### 编辑配置文件

```bash
# 使用 nano 编辑
sudo nano /etc/SmartDNSSort/config.yaml

# 或使用 vi/vim
sudo vim /etc/SmartDNSSort/config.yaml
```

### 重要配置项说明

```yaml
# DNS 监听端口（默认 53）
dns:
  listen_port: 53
  enable_tcp: true
  enable_ipv6: true

# 上游 DNS 服务器列表
upstream:
  servers:
    - "8.8.8.8"           # Google DNS
    - "8.8.4.4"           # Google DNS
    - "1.1.1.1"           # Cloudflare DNS
    - "208.67.222.222"    # OpenDNS
  strategy: "random"      # 查询策略：parallel 或 random
  timeout_ms: 3000        # 超时时间（毫秒）
  concurrency: 4          # 并发数

# Ping 检测配置
ping:
  count: 3                # 每次检测的包数
  timeout_ms: 500         # Ping 超时时间
  concurrency: 16         # 并发检测数
  strategy: "min"         # 选择最低延迟

# DNS 缓存配置
cache:
  min_ttl_seconds: 3600   # 最小缓存时间
  max_ttl_seconds: 84600  # 最大缓存时间

# Web UI 管理界面
webui:
  enabled: true           # 是否启用
  listen_port: 8080       # 监听端口
```

### 配置修改后生效

```bash
# 修改配置后需要重启服务
sudo systemctl restart SmartDNSSort

# 查看重启后的状态
sudo systemctl status SmartDNSSort
```

## 服务管理

### 查看服务状态

```bash
# 方式 1：使用 SmartDNSSort 命令（推荐）
./SmartDNSSort -s status

# 方式 2：使用 systemctl
sudo systemctl status SmartDNSSort

# 方式 3：检查监听端口
sudo netstat -ulnp | grep SmartDNSSort
```

### 启动、停止、重启服务

```bash
# 启动服务
sudo systemctl start SmartDNSSort

# 停止服务
sudo systemctl stop SmartDNSSort

# 重启服务
sudo systemctl restart SmartDNSSort

# 重新加载配置（不停止服务，如果支持）
sudo systemctl reload SmartDNSSort
```

### 开机自启设置

```bash
# 启用开机自启
sudo systemctl enable SmartDNSSort

# 禁用开机自启
sudo systemctl disable SmartDNSSort

# 查看是否设置开机自启
sudo systemctl is-enabled SmartDNSSort
# 输出：enabled 或 disabled
```

### 获取详细日志

```bash
# 查看最近的日志（最后 50 行）
sudo journalctl -u SmartDNSSort -n 50

# 实时跟踪日志
sudo journalctl -u SmartDNSSort -f

# 查看特定时间范围的日志
sudo journalctl -u SmartDNSSort --since "2 hours ago"

# 查看按优先级的日志（仅错误）
sudo journalctl -u SmartDNSSort -p err
```

## 日志查看

### 日志位置

**systemd 日志**（推荐）
```bash
# 通过 journalctl 查看
sudo journalctl -u SmartDNSSort -f

# 或查看本地日志
less /var/log/journal/*/smartdnssort*
```

**应用日志文件**（如果配置）
```bash
# 默认日志目录
/var/log/SmartDNSSort/

# 查看日志
ls -la /var/log/SmartDNSSort/
```

### 日志级别

系统会记录以下类型的日志：
- **DEBUG**: 详细调试信息
- **INFO**: 一般信息消息
- **WARN**: 警告信息
- **ERROR**: 错误信息
- **FATAL**: 致命错误

## 故障排除

### 问题 1：Permission Denied

```bash
# 错误：Permission denied
# 解决：确保有 root 权限
sudo ./SmartDNSSort -s install

# 或检查文件权限
ls -la SmartDNSSort
chmod +x SmartDNSSort
```

### 问题 2：systemd not found

```bash
# 错误：系统不支持 systemd
# 解决：检查系统是否有 systemd
systemctl --version

# 如果没有，请升级系统或使用其他管理方式
```

### 问题 3：Port 53 already in use

```bash
# 查看占用 53 端口的进程
sudo lsof -i :53
# 或
sudo netstat -tulnp | grep :53

# 解决方案：
# 1. 停止占用的服务
sudo systemctl stop systemd-resolved
# 2. 修改 SmartDNSSort 的端口（不推荐）
# 3. 或修改另一个服务的端口
```

### 问题 4：启动失败

```bash
# 查看详细错误信息
sudo systemctl status SmartDNSSort

# 或查看完整日志
sudo journalctl -u SmartDNSSort --no-pager

# 检查配置文件
cat /etc/SmartDNSSort/config.yaml

# 测试二进制是否可执行
/usr/local/bin/SmartDNSSort --help
```

### 问题 5：DNS 查询不工作

```bash
# 测试 DNS 查询
dig @127.0.0.1 www.google.com

# 测试特定上游 DNS
dig @8.8.8.8 www.google.com

# 检查网络连接
ping -c 1 8.8.8.8

# 查看服务日志
sudo journalctl -u SmartDNSSort -f
```

### 问题 6：Web UI 无法访问

```bash
# 检查 Web UI 是否启用
grep -A 3 "webui:" /etc/SmartDNSSort/config.yaml

# 检查 8080 端口是否监听
sudo netstat -tulnp | grep 8080
# 或
sudo ss -tulnp | grep 8080

# 尝试访问 Web UI
curl http://127.0.0.1:8080/

# 查看防火墙设置
sudo iptables -L -n | grep 8080
```

## 卸载

### 使用命令卸载

```bash
# 方式 1：使用 SmartDNSSort 命令
sudo ./SmartDNSSort -s uninstall

# 方式 2：使用脚本
sudo ./install.sh -s uninstall
```

### 手动卸载

如果自动卸载失败，可以手动执行以下步骤：

```bash
# 1. 停止服务
sudo systemctl stop SmartDNSSort

# 2. 禁用服务
sudo systemctl disable SmartDNSSort

# 3. 删除服务文件
sudo rm /etc/systemd/system/SmartDNSSort.service

# 4. 重新加载 systemd
sudo systemctl daemon-reload

# 5. 删除程序和数据
sudo rm /usr/local/bin/SmartDNSSort
sudo rm -rf /etc/SmartDNSSort
sudo rm -rf /var/lib/SmartDNSSort
sudo rm -rf /var/log/SmartDNSSort
```

### 保留配置文件（可选）

如果要卸载但保留配置：

```bash
# 备份配置文件
sudo cp -r /etc/SmartDNSSort ~/smartdnssort-backup

# 执行卸载
sudo ./SmartDNSSort -s uninstall

# 或手动删除其他内容，保留配置
sudo rm /usr/local/bin/SmartDNSSort
sudo rm -rf /var/lib/SmartDNSSort
sudo rm -rf /var/log/SmartDNSSort
```

## 常见操作速查表

| 操作 | 命令 |
|------|------|
| 查看状态 | `./SmartDNSSort -s status` |
| 启动服务 | `sudo systemctl start SmartDNSSort` |
| 停止服务 | `sudo systemctl stop SmartDNSSort` |
| 重启服务 | `sudo systemctl restart SmartDNSSort` |
| 查看日志 | `sudo journalctl -u SmartDNSSort -f` |
| 编辑配置 | `sudo nano /etc/SmartDNSSort/config.yaml` |
| 卸载服务 | `sudo ./SmartDNSSort -s uninstall` |
| 测试 DNS | `dig @127.0.0.1 www.google.com` |

## 高级配置

### 使用非 root 用户运行

```bash
# 1. 创建专用用户
sudo useradd -r -s /bin/false smartdns

# 2. 调整目录权限
sudo chown smartdns:smartdns /var/lib/SmartDNSSort
sudo chown smartdns:smartdns /var/log/SmartDNSSort

# 3. 编辑服务文件
sudo nano /etc/systemd/system/SmartDNSSort.service

# 4. 修改 User 字段为 smartdns
# User=smartdns

# 5. 重新加载服务
sudo systemctl daemon-reload
sudo systemctl restart SmartDNSSort
```

### 使用自定义配置路径

```bash
# 安装时指定配置路径
sudo ./SmartDNSSort -s install -c /etc/smartdnssort/custom.yaml

# 运行时指定配置路径
/usr/local/bin/SmartDNSSort -c /etc/smartdnssort/custom.yaml
```

### Docker 部署（未来支持）

待实现...

---

## 获取帮助

- **项目地址**: https://github.com/lee-alone/SmartDNSSort
- **问题反馈**: https://github.com/lee-alone/SmartDNSSort/issues
- **讨论区**: https://github.com/lee-alone/SmartDNSSort/discussions

最后更新：2025 年 11 月 15 日
