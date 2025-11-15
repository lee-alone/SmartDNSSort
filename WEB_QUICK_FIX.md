# Web 文件部署快速指南

## 最快的解决方案

### 问题
Web 界面访问时返回 404。

### 原因
Web 静态文件（`index.html` 等）找不到。

### 解决方案

#### ✅ 方案 1：使用新版本（推荐）

```bash
# 1. 重新编译（包含自动复制功能）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o SmartDNSSort ./cmd/main.go

# 2. 重新安装
sudo ./SmartDNSSort -s uninstall 2>/dev/null
sudo ./SmartDNSSort -s install

# 3. 查看日志确认 web 文件已复制
sudo journalctl -u SmartDNSSort -n 20 | grep -i "web"

# 4. 访问 Web UI
curl http://localhost:8080/
```

#### ✅ 方案 2：手动复制 Web 文件（应急）

```bash
# 1. 创建目录
sudo mkdir -p /var/lib/SmartDNSSort/web

# 2. 复制 Web 文件
sudo cp -r web/* /var/lib/SmartDNSSort/web/

# 3. 设置权限
sudo chown -R root:root /var/lib/SmartDNSSort/web
sudo chmod 755 /var/lib/SmartDNSSort/web

# 4. 重启服务
sudo systemctl restart SmartDNSSort

# 5. 验证
curl http://localhost:8080/
```

#### ✅ 方案 3：指定 Web 目录配置

如果 web 文件在其他位置，可以修改代码中的路径查找逻辑。

## 验证步骤

```bash
# 检查 Web 文件是否存在
ls -la /var/lib/SmartDNSSort/web/

# 检查服务是否运行
sudo systemctl status SmartDNSSort

# 测试 API
curl http://localhost:8080/api/stats

# 测试 Web UI
curl http://localhost:8080/

# 实时查看日志
sudo journalctl -u SmartDNSSort -f
```

## 期望的日志输出

```
[INFO] 复制 Web 文件到：/var/lib/SmartDNSSort/web
[INFO] 使用 web 目录: /var/lib/SmartDNSSort/web
Web API server started on http://localhost:8080
```

## 如果仍然有问题

```bash
# 1. 检查是否真的是 404
curl -v http://localhost:8080/ 2>&1 | grep -E "HTTP|index.html"

# 2. 检查文件是否存在
file /var/lib/SmartDNSSort/web/index.html

# 3. 检查权限
stat /var/lib/SmartDNSSort/web/index.html

# 4. 查看完整日志
sudo journalctl -u SmartDNSSort --no-pager -n 100

# 5. 检查端口
sudo netstat -tulnp | grep 8080
```

---

**下一步**: 在 Debian 系统上测试这些步骤，应该能解决 Web 界面 404 的问题。
