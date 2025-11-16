# SmartDNSSort Linux 快速参考

## 安装

```bash
# 快速安装（推荐）
sudo ./SmartDNSSort -s install

# 预览安装流程
sudo ./SmartDNSSort -s install --dry-run

# 自定义安装
sudo ./SmartDNSSort -s install \
  -c /etc/smartdns/config.yaml \
  -w /var/lib/smartdns \
  -user smartdns
```

## 卸载

```bash
# 快速卸载
sudo ./SmartDNSSort -s uninstall

# 预览卸载流程
sudo ./SmartDNSSort -s uninstall --dry-run
```

## 状态查询

```bash
# 查看详细状态
./SmartDNSSort -s status

# 查看简要状态
sudo systemctl status SmartDNSSort

# 查看最近日志
sudo journalctl -u SmartDNSSort -n 20
```

## 服务管理

```bash
# 启动服务
sudo systemctl start SmartDNSSort

# 停止服务
sudo systemctl stop SmartDNSSort

# 重启服务
sudo systemctl restart SmartDNSSort

# 查看是否开机自启
sudo systemctl is-enabled SmartDNSSort
```

## 配置管理

```bash
# 编辑配置
sudo nano /etc/SmartDNSSort/config.yaml

# 配置修改后重启
sudo systemctl restart SmartDNSSort

# 验证配置
sudo SmartDNSSort -c /etc/SmartDNSSort/config.yaml --help
```

## DNS 测试

```bash
# 测试 DNS 解析
dig @127.0.0.1 www.google.com

# 测试特定上游 DNS
dig @8.8.8.8 www.google.com

# 测试 TCP 查询
dig @127.0.0.1 www.google.com +tcp
```

## 日志查询

```bash
# 实时查看日志
sudo journalctl -u SmartDNSSort -f

# 查看错误日志
sudo journalctl -u SmartDNSSort -p err

# 查看最近 1 小时的日志
sudo journalctl -u SmartDNSSort --since "1 hour ago"

# 查看特定关键词
sudo journalctl -u SmartDNSSort | grep "error"
```

## 故障排除

```bash
# 检查服务状态
sudo systemctl status SmartDNSSort

# 查看完整错误日志
sudo journalctl -u SmartDNSSort --no-pager

# 检查 53 端口占用
sudo netstat -tulnp | grep :53

# 检查配置文件语法
cat /etc/SmartDNSSort/config.yaml

# 强制重启服务
sudo systemctl kill -9 SmartDNSSort
sudo systemctl start SmartDNSSort
```

## 文件位置

| 用途 | 路径 |
|------|------|
| 配置文件 | `/etc/SmartDNSSort/config.yaml` |
| 可执行文件 | `/usr/local/bin/SmartDNSSort` |
| 数据目录 | `/var/lib/SmartDNSSort` |
| 日志目录 | `/var/log/SmartDNSSort` |
| 服务文件 | `/etc/systemd/system/SmartDNSSort.service` |

## Web UI

- **地址**: http://localhost:8080
- **功能**: 管理界面、统计信息、配置查看

## 常见配置

```yaml
# 基础配置
dns:
  listen_port: 53
  enable_tcp: true

# 上游 DNS（示例）
upstream:
  servers:
    - "8.8.8.8"
    - "8.8.4.4"
    - "1.1.1.1"
  strategy: "random"
  timeout_ms: 3000

# 缓存配置
cache:
  min_ttl_seconds: 300
  max_ttl_seconds: 86400
```

## 诊断命令

```bash
# 检查 systemd 支持
systemctl --version

# 检查二进制兼容性
file /usr/local/bin/SmartDNSSort

# 检查依赖
ldd /usr/local/bin/SmartDNSSort

# 运行诊断
/usr/local/bin/SmartDNSSort -h
```

## 获取帮助

```bash
# 查看完整帮助
./SmartDNSSort -h

# 安装帮助
./SmartDNSSort -s install --help

# 详细文档
cat LINUX_INSTALL.md
```

---

**提示**: 所有需要修改系统的操作需要 `sudo` 权限
