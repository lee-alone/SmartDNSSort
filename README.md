# SmartDNSSort

🚀 **智能 DNS 排序服务器** - 自动发现最快的上游DNS服务器，为用户提供快速可靠的DNS解析服务。

## 功能特性

- 🎯 **智能排序** - 自动测试多个上游DNS服务器的响应时间（RTT），选择最快的进行查询
- 🔄 **并发优化** - 支持自定义并发数和超时设置，灵活适配不同环境
- 📊 **缓存管理** - 支持DNS查询结果缓存，三阶段缓存设计
- 🌐 **跨平台支持** - Windows、Linux、ARM等多平台编译支持
- 🖥️ **Web UI** - 实时可视化管理界面，查看DNS统计信息
- 🔧 **系统集成** - Linux系统服务安装，开机自启支持

## 快速开始

### 系统要求

- **Go 1.16+** (用于从源码编译)
- **Linux** / **Windows** / **macOS**

### 安装

#### 方式1：使用预编译二进制文件

从 [GitHub Releases](https://github.com/lee-alone/SmartDNSSort/releases) 下载适合您平台的版本：

- `SmartDNSSort-windows-x64.exe` - Windows 64位
- `SmartDNSSort-windows-x86.exe` - Windows 32位
- `SmartDNSSort-debian-x64` - Linux 64位（Debian/Ubuntu）
- `SmartDNSSort-debian-x86` - Linux 32位
- `SmartDNSSort-debian-arm64` - Linux ARM64

#### 方式2：从源码编译

```bash
# 克隆仓库
git clone https://github.com/lee-alone/SmartDNSSort.git
cd SmartDNSSort

# 编译当前平台
make build

# 编译所有平台
make build-all

# 输出文件在 bin/ 目录下
ls -lh bin/
```

### 配置

编辑 `config.yaml` 配置文件：

```yaml
dns:
  listenPort: 53          # DNS 监听端口
  listenAddr: "0.0.0.0"  # 监听地址

upstream:
  servers:
    - "8.8.8.8:53"       # Google DNS
    - "1.1.1.1:53"       # Cloudflare DNS
    - "114.114.114.114:53" # 国内 DNS

ping:
  concurrency: 10         # 并发数
  timeoutMs: 3000        # 超时时间(毫秒)
  intervalSec: 60        # 更新间隔(秒)

cache:
  enabled: true          # 是否启用缓存
  ttlSec: 3600          # 缓存有效期(秒)

webUI:
  enabled: true          # 是否启用 Web UI
  listenAddr: "0.0.0.0" # Web UI 监听地址
  listenPort: 8080      # Web UI 监听端口
```

### 运行

#### Windows

```bash
# 直接运行
SmartDNSSort-windows-x64.exe

# 使用自定义配置
SmartDNSSort-windows-x64.exe -c config.yaml

# 显示帮助信息
SmartDNSSort-windows-x64.exe -h
```

#### Linux

```bash
# 直接运行
./SmartDNSSort-debian-x64

# 使用自定义配置
./SmartDNSSort-debian-x64 -c config.yaml

# 查看帮助信息
./SmartDNSSort-debian-x64 -h
```

#### 安装为系统服务（Linux）

```bash
# 安装服务
sudo ./SmartDNSSort-debian-x64 -s install -c /etc/SmartDNSSort/config.yaml

# 查看服务状态
./SmartDNSSort-debian-x64 -s status

# 卸载服务
sudo ./SmartDNSSort-debian-x64 -s uninstall
```

## 命令行参数

```
-s <命令>      系统服务管理（仅 Linux）
               - install    安装服务
               - uninstall  卸载服务
               - status     查看服务状态

-c <路径>     配置文件路径（默认：config.yaml）
-w <路径>     工作目录（默认：当前目录）
-user <用户>  运行用户（仅限 install，默认：root）
-dry-run      干运行模式，仅预览不执行（仅限 install/uninstall）
-v            详细输出
-h            显示帮助信息
```

## Web UI

启动应用后，访问 `http://localhost:8080` 查看：

- 📊 实时DNS查询统计
- ⏱️ 各上游服务器响应时间
- 📈 查询历史和缓存状态
- 🔧 快速设置调整

## 开发

### 编译特定平台

```bash
# Windows
make build-windows

# Linux（所有架构）
make build-linux

# 清理编译文件
make clean
```

### 运行测试

```bash
# 运行所有测试
make test

# 详细测试（含竞态检测）
make test-verbose
```

### 打包发布版本

```bash
# 编译所有平台并打包
make release

# 输出文件在 bin/ 目录
```

## 项目结构

```
SmartDNSSort/
├── cmd/              # 应用入口
├── dnsserver/        # DNS服务器核心
├── cache/            # 缓存模块
├── ping/             # 延迟测试模块
├── upstream/         # 上游服务器管理
├── web/              # Web UI 文件
├── webapi/           # Web API 接口
├── config/           # 配置管理
├── stats/            # 统计模块
├── sysinstall/       # 系统安装模块
├── config.yaml       # 配置文件
└── Makefile          # 构建脚本
```

## 文档

- 📖 [使用指南](docs/guides/USAGE_GUIDE.md) - 详细使用说明
- 🔧 [安装指南](docs/guides/TESTING.md) - 测试和安装步骤
- 💻 [开发文档](docs/development/DEVELOP.md) - 开发者指南
- 🐧 [Linux安装](docs/linux/LINUX_INSTALL.md) - Linux系统安装说明
- 📋 [项目概览](docs/general/OVERVIEW.md) - 项目整体说明

更多文档请查看 [docs/](docs/) 目录。

## 常见问题

### Q: 如何修改 DNS 监听端口？
A: 编辑 `config.yaml` 中的 `dns.listenPort` 字段。

### Q: 如何添加自定义上游 DNS 服务器？
A: 编辑 `config.yaml` 中的 `upstream.servers` 列表。

### Q: 如何禁用 Web UI？
A: 在 `config.yaml` 中设置 `webUI.enabled: false`。

### Q: Windows 上如何后台运行？
A: 可以创建计划任务或使用第三方工具（如 NSSM）。

### Q: Linux 上服务无法启动？
A: 检查权限、配置文件路径、日志文件位置等。运行 `./SmartDNSSort -s status` 查看状态。

## 性能指标

- **缓存命中率**: 通过三阶段缓存设计，典型场景命中率 > 80%
- **查询延迟**: 平均 < 50ms（取决于上游服务器）
- **并发能力**: 支持 > 1000 qps

## 故障排除

### DNS 查询超时

1. 检查上游服务器是否可达：`ping 8.8.8.8`
2. 增加 `ping.timeoutMs` 值
3. 检查防火墙规则

### Web UI 无法访问

1. 确保 `webUI.enabled: true`
2. 检查防火墙是否开放 8080 端口
3. 验证监听地址配置

### 服务启动失败（Linux）

1. 检查日志：`journalctl -u smartdnssort -n 50`
2. 确认配置文件权限正确
3. 尝试手动运行检查具体错误

## 贡献

欢迎提交 Issue 和 Pull Request！

- [GitHub Issues](https://github.com/lee-alone/SmartDNSSort/issues)
- [GitHub Discussions](https://github.com/lee-alone/SmartDNSSort/discussions)

## 许可证

MIT License - 详见 [LICENSE](LICENSE) 文件

## 作者

**lee-alone** - [GitHub](https://github.com/lee-alone)

---

**最后更新**: 2025-11-15

如有问题或建议，欢迎通过 GitHub Issues 联系我们！
