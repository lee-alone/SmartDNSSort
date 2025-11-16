# 🎯 快速参考 - Web UI 自动安装修复

## 问题 → 解决方案 → 结果

```
问题：
├─ Web 目录不存在 (/var/lib/SmartDNSSort/web/)
├─ Web 文件未复制
└─ 404 错误

↓

解决方案：
├─ webapi/api.go:       增强路径查找能力
└─ sysinstall/installer.go: 自动创建和复制

↓

结果：
✅ 安装后自动可用
✅ 无需手动干预
✅ 文档完整
```

---

## 💾 关键文件位置

### 修改的源代码
```
d:\gb\SmartDNSSort\webapi\api.go              ← 路径查找
d:\gb\SmartDNSSort\sysinstall\installer.go   ← 安装流程
```

### 编译输出的二进制
```
d:\gb\SmartDNSSort\bin\SmartDNSSort.exe       ← Windows
d:\gb\SmartDNSSort\bin\SmartDNSSort-linux-x64 ← Linux (用于 Debian)
```

### 文档
```
项目根目录:
├─ README_FIX_SUMMARY.md          ← 快速概览 (推荐先读)
├─ SOLUTION_SUMMARY.md            ← 完整方案
├─ DEBIAN_DEPLOYMENT_GUIDE.md     ← Debian 部署指南
├─ WEB_INSTALLATION_FIX.md        ← 修复技术说明
├─ CHANGELOG_WEB_FIX.md           ← 修改日志
└─ docs/guides/INSTALLATION_FIX.md ← 详细技术

更新的文件:
├─ README.md                      ← 添加安装方式
└─ docs/linux/LINUX_INSTALL.md   ← 添加问题 7
```

---

## 📦 部署命令速查

### 1️⃣ 准备（Windows）
```powershell
# 查看已编译的二进制
ls d:\gb\SmartDNSSort\bin\SmartDNSSort*
```

### 2️⃣ 上传到 Debian
```powershell
# PowerShell (Windows)
scp d:\gb\SmartDNSSort\bin\SmartDNSSort-linux-x64 root@debian-ip:/root/
```

### 3️⃣ 在 Debian 上安装
```bash
# SSH 连接
ssh root@debian-ip

# 进入文件目录
cd ~

# 给权限
chmod +x SmartDNSSort-linux-x64

# 预览（强烈推荐）
sudo ./SmartDNSSort-linux-x64 -s install --dry-run

# 实际安装
sudo ./SmartDNSSort-linux-x64 -s install
```

### 4️⃣ 验证
```bash
# 检查服务
sudo systemctl status SmartDNSSort

# 检查 Web 目录（关键）
ls -la /var/lib/SmartDNSSort/web/
# 应该看到 index.html

# 测试 Web UI
curl http://127.0.0.1:8080/
# 应该返回 HTML，不是 404
```

---

## 🔍 关键路径查找优先级

程序启动时会按这个顺序查找 Web 文件：

```
1. /var/lib/SmartDNSSort/web           ← 生产环境位置 (推荐)
   ↓ (如果不存在)
2. <可执行文件目录>/web                ← 新增: 可执行文件同目录
   ↓ (如果不存在)
3. /usr/share/smartdnssort/web         ← FHS 标准
   ↓ (如果不存在)
4. /etc/SmartDNSSort/web               ← 备选位置
   ↓ (如果不存在)
5. ./web                                ← 开发环境相对路径
   ↓ (如果不存在)
6. web                                  ← 开发环境相对路径
   ↓ (都不存在)
❌ 返回空，Web UI 无法启动
```

---

## 🛠️ 常见操作

### 查看 Web 文件是否正确复制
```bash
# 检查目录
ls -la /var/lib/SmartDNSSort/web/

# 应该看到类似：
# -rw-r--r-- 1 root root  12345 Nov 15 10:00 index.html
# 或其他 Web 文件

# 如果为空
ls -la /var/lib/SmartDNSSort/
# 检查 web 目录是否存在但为空
```

### 重新安装（如果有问题）
```bash
# 卸载旧版本
sudo ./SmartDNSSort-linux-x64 -s uninstall

# 重新安装新版本
sudo ./SmartDNSSort-linux-x64 -s install

# 验证
curl http://127.0.0.1:8080/
```

### 手动修复（临时方案）
```bash
# 如果 Web 文件未复制，手动复制
sudo mkdir -p /var/lib/SmartDNSSort/web

# 需要找到 web 源文件（通常在二进制附近）
# 或从项目目录复制：
# sudo cp ./web/* /var/lib/SmartDNSSort/web/

# 重启服务
sudo systemctl restart SmartDNSSort
```

### 查看日志
```bash
# 实时日志
sudo journalctl -u SmartDNSSort -f

# 最后 50 行
sudo journalctl -u SmartDNSSort -n 50

# 错误级别
sudo journalctl -u SmartDNSSort -p err
```

---

## ❓ 故障排除快速表

| 症状 | 检查 | 解决 |
|------|------|------|
| 404 错误 | `ls /var/lib/SmartDNSSort/web/` | 检查目录是否存在和有文件 |
| 权限错误 | `ls -la /var/lib/SmartDNSSort/` | `sudo chown -R root:root /var/lib/SmartDNSSort/` |
| 8080 端口错误 | `sudo netstat -tulnp \| grep 8080` | 检查防火墙或修改配置 |
| 启动失败 | `sudo journalctl -u SmartDNSSort -n 20` | 查看日志找原因 |
| DNS 不工作 | `dig @127.0.0.1 www.google.com` | 检查上游 DNS 配置 |

---

## 📊 修改前后对比

| 方面 | 修改前 | 修改后 |
|------|--------|--------|
| **Web 目录创建** | ❌ 手动 | ✅ 自动 |
| **Web 文件复制** | ❌ 手动 | ✅ 自动 |
| **首次启动** | ❌ 404 | ✅ 正常 |
| **路径查找** | ❌ 固定 | ✅ 灵活 |
| **容错能力** | ❌ 差 | ✅ 好 |
| **文档** | ❌ 缺少 | ✅ 完整 |

---

## 📚 文档快速导航

| 需要 | 阅读文档 |
|------|---------|
| **5分钟快速了解** | README_FIX_SUMMARY.md |
| **部署到 Debian** | DEBIAN_DEPLOYMENT_GUIDE.md |
| **理解技术细节** | CHANGELOG_WEB_FIX.md |
| **完整的修复说明** | WEB_INSTALLATION_FIX.md |
| **Linux 完整安装** | docs/linux/LINUX_INSTALL.md |
| **项目概览** | README.md |

---

## ✅ 安装检查清单

安装后确保以下所有项都 ✓：

```
□ 服务正在运行
  sudo systemctl status SmartDNSSort → 应显示 active

□ Web 目录已创建
  ls /var/lib/SmartDNSSort/web/ → 应有 index.html

□ Web 目录非空
  ls -la /var/lib/SmartDNSSort/web/ → 应有文件列表

□ DNS 端口监听
  sudo netstat -ulnp | grep :53 → 应显示 SmartDNSSort

□ Web 端口监听  
  sudo netstat -tulnp | grep 8080 → 应显示 8080

□ Web UI 可访问
  curl http://127.0.0.1:8080/ → 应返回 HTML，不是 404

□ 无错误日志
  sudo journalctl -u SmartDNSSort -p err → 应为空
```

---

## 🎯 成功标志

✅ **安装成功的标志：**
```bash
# 1. 这个命令返回 HTML（不是 404）
curl http://127.0.0.1:8080/

# 2. 这个目录存在且有文件
ls /var/lib/SmartDNSSort/web/

# 3. 浏览器访问能打开页面
http://<debian-ip>:8080
```

---

## 💡 提示

- **总是先预览：** 用 `--dry-run` 看安装会做什么
- **保存日志：** 安装时截图或保存日志以备需要
- **备份配置：** 升级前备份 `/etc/SmartDNSSort/config.yaml`
- **循序渐进：** 先本地测试，再部署到生产

---

**最后更新：** 2025 年 11 月 15 日  
**版本：** SmartDNSSort v1.0+ (Web UI 自动安装修复版)  
**支持：** Debian/Ubuntu x86_64 系统
