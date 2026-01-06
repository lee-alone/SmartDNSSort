# 递归DNS解析器部署指南

本指南提供了在不同环境中部署递归DNS解析器的详细说明。

## 目录

1. [部署模式](#部署模式)
2. [开发环境部署](#开发环境部署)
3. [生产环境部署](#生产环境部署)
4. [Docker部署](#docker部署)
5. [高可用部署](#高可用部署)
6. [监控和维护](#监控和维护)
7. [故障恢复](#故障恢复)

## 部署模式

递归DNS解析器支持三种部署模式：

### 模式A: 内嵌模式（开发/轻量使用）

递归解析器作为主系统的一部分运行在同一进程中。

**优点**:
- 配置简单
- 资源消耗少
- 部署快速

**缺点**:
- 不支持独立扩展
- 主系统故障会影响解析器
- 不适合高可用部署

**适用场景**:
- 开发和测试
- 轻量级部署
- 单机部署

### 模式B: 独立进程模式（生产/高可用）

递归解析器作为独立进程运行，通过UDS或TCP与主系统通信。

**优点**:
- 独立扩展
- 故障隔离
- 支持高可用
- 支持多进程

**缺点**:
- 配置复杂
- 需要进程管理
- 通信开销

**适用场景**:
- 生产环境
- 高可用部署
- 高并发场景

### 模式C: 容器化部署

使用Docker容器部署递归解析器。

**优点**:
- 环境一致性
- 易于扩展
- 支持编排
- 资源隔离

**缺点**:
- 需要Docker环境
- 性能开销
- 学习曲线

**适用场景**:
- 云环境
- Kubernetes部署
- 微服务架构

## 开发环境部署

### 步骤1: 安装依赖

```bash
# 安装Go 1.25或更高版本
# 下载地址: https://golang.org/dl/

# 验证安装
go version
```

### 步骤2: 克隆项目

```bash
git clone <repository-url>
cd SmartDNSSort
```

### 步骤3: 构建项目

```bash
# 在Linux/macOS上
./build.sh

# 在Windows上
./build.bat

# 或手动构建
go build -o SmartDNSSort ./cmd/main.go
```

### 步骤4: 创建配置文件

```bash
# 创建主配置文件
cat > config.yaml << 'EOF'
resolver:
  enabled: true
  config_file: resolver.yaml
  transport: auto
EOF

# 创建递归解析器配置文件
cat > resolver.yaml << 'EOF'
server:
  transport: auto
  timeout_ms: 5000
  mode: recursive

resolver:
  cache:
    size: 10000
    expiry: true
  max_depth: 30

optimization:
  enabled: true

performance:
  workers: 4
  max_concurrent: 100

logging:
  level: debug
  file: logs/resolver.log
EOF
```

### 步骤5: 启动服务

```bash
# 创建日志目录
mkdir -p logs

# 启动主系统（内嵌模式）
./SmartDNSSort -c config.yaml

# 或启动独立的递归解析器
./SmartDNSSort resolver -c resolver.yaml
```

### 步骤6: 验证部署

```bash
# 测试DNS查询
dig @127.0.0.1 -p 5353 example.com

# 或使用nslookup
nslookup example.com 127.0.0.1:5353
```

## 生产环境部署

### 步骤1: 系统准备

```bash
# 更新系统
sudo apt-get update
sudo apt-get upgrade -y

# 安装必要的工具
sudo apt-get install -y curl wget git

# 创建专用用户
sudo useradd -r -s /bin/false smartdns
```

### 步骤2: 安装应用

```bash
# 创建应用目录
sudo mkdir -p /opt/smartdns
sudo chown smartdns:smartdns /opt/smartdns

# 复制二进制文件
sudo cp SmartDNSSort /opt/smartdns/
sudo chmod +x /opt/smartdns/SmartDNSSort

# 创建配置目录
sudo mkdir -p /etc/smartdns
sudo chown smartdns:smartdns /etc/smartdns

# 创建日志目录
sudo mkdir -p /var/log/smartdns
sudo chown smartdns:smartdns /var/log/smartdns
```

### 步骤3: 配置应用

```bash
# 创建主配置文件
sudo tee /etc/smartdns/config.yaml > /dev/null << 'EOF'
resolver:
  enabled: true
  config_file: /etc/smartdns/resolver.yaml
  transport: auto
EOF

# 创建递归解析器配置文件
sudo tee /etc/smartdns/resolver.yaml > /dev/null << 'EOF'
server:
  transport: unix
  unix_socket:
    path: /tmp/smartdns-resolver.sock
    permissions: "0600"
  timeout_ms: 3000
  mode: recursive

resolver:
  cache:
    size: 50000
    expiry: true
  max_depth: 30

optimization:
  enabled: true

performance:
  workers: 8
  max_concurrent: 200

logging:
  level: warn
  file: /var/log/smartdns/resolver.log
EOF

# 设置权限
sudo chmod 644 /etc/smartdns/config.yaml
sudo chmod 644 /etc/smartdns/resolver.yaml
```

### 步骤4: 创建systemd服务

```bash
# 创建主系统服务
sudo tee /etc/systemd/system/smartdns.service > /dev/null << 'EOF'
[Unit]
Description=SmartDNS Sort Service
After=network.target

[Service]
Type=simple
User=smartdns
Group=smartdns
WorkingDirectory=/opt/smartdns
ExecStart=/opt/smartdns/SmartDNSSort -c /etc/smartdns/config.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# 创建递归解析器服务（独立模式）
sudo tee /etc/systemd/system/smartdns-resolver.service > /dev/null << 'EOF'
[Unit]
Description=SmartDNS Recursive Resolver Service
After=network.target

[Service]
Type=simple
User=smartdns
Group=smartdns
WorkingDirectory=/opt/smartdns
ExecStart=/opt/smartdns/SmartDNSSort resolver -c /etc/smartdns/resolver.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# 重新加载systemd配置
sudo systemctl daemon-reload
```

### 步骤5: 启动服务

```bash
# 启动递归解析器（如果使用独立模式）
sudo systemctl start smartdns-resolver
sudo systemctl enable smartdns-resolver

# 启动主系统
sudo systemctl start smartdns
sudo systemctl enable smartdns

# 检查服务状态
sudo systemctl status smartdns
sudo systemctl status smartdns-resolver
```

### 步骤6: 验证部署

```bash
# 检查服务是否运行
sudo systemctl is-active smartdns
sudo systemctl is-active smartdns-resolver

# 查看日志
sudo journalctl -u smartdns -f
sudo journalctl -u smartdns-resolver -f

# 测试DNS查询
dig @127.0.0.1 -p 53 example.com
```

## Docker部署

### 步骤1: 创建Dockerfile

```dockerfile
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY . .

RUN go build -o SmartDNSSort ./cmd/main.go

FROM alpine:latest

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/SmartDNSSort .

# 创建必要的目录
RUN mkdir -p /etc/smartdns /var/log/smartdns

EXPOSE 53/udp 53/tcp 8080/tcp 5353/tcp

ENTRYPOINT ["./SmartDNSSort"]
CMD ["-c", "/etc/smartdns/config.yaml"]
```

### 步骤2: 创建Docker Compose配置

```yaml
version: '3.8'

services:
  resolver:
    build: .
    container_name: smartdns-resolver
    volumes:
      - ./resolver.yaml:/etc/smartdns/resolver.yaml:ro
      - resolver-socket:/tmp
      - resolver-logs:/var/log/smartdns
    environment:
      - LOG_LEVEL=info
    command: resolver -c /etc/smartdns/resolver.yaml
    restart: unless-stopped

  smartdns:
    build: .
    container_name: smartdns
    ports:
      - "53:53/udp"
      - "53:53/tcp"
      - "8080:8080/tcp"
    volumes:
      - ./config.yaml:/etc/smartdns/config.yaml:ro
      - resolver-socket:/tmp
      - smartdns-logs:/var/log/smartdns
    environment:
      - LOG_LEVEL=info
    depends_on:
      - resolver
    restart: unless-stopped

volumes:
  resolver-socket:
  resolver-logs:
  smartdns-logs:
```

### 步骤3: 构建和运行

```bash
# 构建镜像
docker-compose build

# 启动容器
docker-compose up -d

# 查看日志
docker-compose logs -f

# 停止容器
docker-compose down
```

## 高可用部署

### 架构

```
┌─────────────────────────────────────────┐
│         负载均衡器 (HAProxy)             │
│         监听 53/udp, 53/tcp             │
└────────────────┬────────────────────────┘
                 │
        ┌────────┼────────┐
        ▼        ▼        ▼
    ┌────────┐┌────────┐┌────────┐
    │SmartDNS││SmartDNS││SmartDNS│
    │ 实例1  ││ 实例2  ││ 实例3  │
    └────────┘└────────┘└────────┘
        │        │        │
        └────────┼────────┘
                 ▼
    ┌──────────────────────────┐
    │  共享存储 (NFS/S3)        │
    │  - 配置文件              │
    │  - 日志文件              │
    └──────────────────────────┘
```

### 步骤1: 安装HAProxy

```bash
sudo apt-get install -y haproxy

# 配置HAProxy
sudo tee /etc/haproxy/haproxy.cfg > /dev/null << 'EOF'
global
    log stdout local0
    maxconn 4096

defaults
    log     global
    mode    tcp
    timeout connect 5000
    timeout client  50000
    timeout server  50000

frontend dns_in
    bind *:53
    default_backend dns_servers

backend dns_servers
    balance roundrobin
    server dns1 127.0.0.1:5353 check
    server dns2 127.0.0.1:5354 check
    server dns3 127.0.0.1:5355 check
EOF

# 启动HAProxy
sudo systemctl start haproxy
sudo systemctl enable haproxy
```

### 步骤2: 部署多个实例

```bash
# 创建多个实例的配置
for i in 1 2 3; do
    PORT=$((5352 + i))
    
    # 创建配置文件
    cat > /etc/smartdns/resolver-$i.yaml << EOF
server:
  transport: tcp
  tcp:
    listen_addr: 127.0.0.1
    listen_port: $PORT
  timeout_ms: 3000
  mode: recursive

resolver:
  cache:
    size: 50000
    expiry: true

performance:
  workers: 8
  max_concurrent: 200

logging:
  level: warn
  file: /var/log/smartdns/resolver-$i.log
EOF
    
    # 创建systemd服务
    sudo tee /etc/systemd/system/smartdns-resolver-$i.service > /dev/null << EOF
[Unit]
Description=SmartDNS Recursive Resolver Service $i
After=network.target

[Service]
Type=simple
User=smartdns
Group=smartdns
ExecStart=/opt/smartdns/SmartDNSSort resolver -c /etc/smartdns/resolver-$i.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF
done

# 重新加载systemd
sudo systemctl daemon-reload

# 启动所有实例
for i in 1 2 3; do
    sudo systemctl start smartdns-resolver-$i
    sudo systemctl enable smartdns-resolver-$i
done
```

### 步骤3: 配置监控

```bash
# 安装Prometheus客户端库
go get github.com/prometheus/client_golang/prometheus

# 配置Prometheus
cat > /etc/prometheus/prometheus.yml << 'EOF'
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'smartdns'
    static_configs:
      - targets: ['localhost:9090']
EOF
```

## 监控和维护

### 日志监控

```bash
# 实时查看日志
tail -f /var/log/smartdns/resolver.log

# 搜索错误
grep ERROR /var/log/smartdns/resolver.log

# 统计查询数
grep "query" /var/log/smartdns/resolver.log | wc -l
```

### 性能监控

```bash
# 监控进程资源使用
top -p $(pgrep -f SmartDNSSort)

# 监控网络连接
netstat -an | grep 5353

# 监控磁盘使用
du -sh /var/log/smartdns
```

### 定期维护

```bash
# 清理旧日志（每周）
find /var/log/smartdns -name "*.log" -mtime +7 -delete

# 备份配置文件
tar -czf /backup/smartdns-config-$(date +%Y%m%d).tar.gz /etc/smartdns

# 检查磁盘空间
df -h /var/log/smartdns
```

## 故障恢复

### 常见问题

#### 问题1: 服务无法启动

```bash
# 检查日志
sudo journalctl -u smartdns-resolver -n 50

# 检查配置文件
sudo /opt/smartdns/SmartDNSSort resolver -c /etc/smartdns/resolver.yaml

# 检查端口占用
sudo netstat -tlnp | grep 5353
```

#### 问题2: 查询超时

```bash
# 检查网络连接
ping 8.8.8.8

# 检查DNS根服务器连接
dig @a.root-servers.net

# 增加超时时间
# 编辑 /etc/smartdns/resolver.yaml
# 修改 timeout_ms: 10000
```

#### 问题3: 高CPU使用率

```bash
# 检查并发查询数
netstat -an | grep 5353 | wc -l

# 减少工作协程数
# 编辑 /etc/smartdns/resolver.yaml
# 修改 workers: 2

# 重启服务
sudo systemctl restart smartdns-resolver
```

### 备份和恢复

```bash
# 备份配置和日志
tar -czf /backup/smartdns-backup-$(date +%Y%m%d).tar.gz \
    /etc/smartdns \
    /var/log/smartdns

# 恢复配置
tar -xzf /backup/smartdns-backup-20260105.tar.gz -C /

# 重启服务
sudo systemctl restart smartdns-resolver
```

## 总结

递归DNS解析器支持多种部署模式，可以根据需求选择合适的部署方式：

- **开发环境**: 使用内嵌模式，简单快速
- **生产环境**: 使用独立进程模式，支持高可用
- **云环境**: 使用Docker容器，易于扩展
- **高可用**: 使用多实例 + 负载均衡，确保可靠性

定期监控和维护是确保系统稳定运行的关键。

</content>
