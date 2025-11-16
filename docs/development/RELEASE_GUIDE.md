# GitHub 发布与构建流程指南

本文档为 SmartDNSSort 项目的自动化构建与发布流程优化总结。

## 📋 已完成的优化清单

### ✅ 1. 清理旧的二进制文件
- 已删除项目根目录下过时的可执行文件
- 保持仓库代码库的整洁

### ✅ 2. 优化编译与命名
实现了跨平台自动化编译，支持以下平台：

| 平台 | 文件名 | 架构 |
|------|--------|------|
| Windows | `SmartDNSSort-windows-x64.exe` | 64-bit |
| Windows | `SmartDNSSort-windows-x86.exe` | 32-bit |
| Debian/Linux | `SmartDNSSort-debian-x64` | 64-bit |
| Debian/Linux | `SmartDNSSort-debian-x86` | 32-bit |
| Linux ARM | `SmartDNSSort-debian-arm64` | ARM64 |

所有编译产物统一输出到 `bin/` 目录。

### ✅ 3. 更新 .gitignore
添加了规则避免提交编译产物：
```gitignore
bin/
build/
dist/
SmartDNSSort*
smartdnssort*
```

### ✅ 4. Makefile 增强
新增命令：
- `make build-windows` - 编译 Windows 版本
- `make build-linux` - 编译 Linux 版本（所有架构）
- `make build-all` - 全平台编译
- `make clean` - 清理编译文件
- `make release` - 打包发布版本

### ✅ 5. 创建根目录 README.md
- 完整的项目介绍和快速开始指南
- 系统要求、安装方法、配置说明
- 使用示例、命令行参数
- 常见问题解答

### ✅ 6. 添加构建脚本
为不同系统提供便捷的编译脚本：

#### Windows 用户
**build.bat** - 传统 CMD 脚本
```batch
build.bat              # 编译 Windows
build.bat all          # 全平台编译
build.bat help         # 显示帮助
```

**build.ps1** - PowerShell 脚本（推荐）
```powershell
.\build.ps1             # 编译 Windows
.\build.ps1 all         # 全平台编译
.\build.ps1 -Target linux # 编译 Linux
```

#### Linux/macOS 用户
**build.sh** - Bash 脚本
```bash
./build.sh              # 编译 Linux
./build.sh all          # 全平台编译
./build.sh windows      # 编译 Windows
./build.sh clean        # 清理
```

## 🚀 完整的发布工作流

### 第1步：准备编译环境
```bash
# 确保 Go 已安装
go version

# 克隆/进入项目目录
git clone https://github.com/lee-alone/SmartDNSSort.git
cd SmartDNSSort
```

### 第2步：编译所有平台版本

**选项 A：使用构建脚本（推荐）**

Windows:
```powershell
# PowerShell
.\build.ps1 all

# 或使用 CMD
build.bat all
```

Linux/macOS:
```bash
chmod +x build.sh
./build.sh all
```

**选项 B：使用 Makefile**

需要安装 `make` 工具（仅 Linux/macOS）：
```bash
make build-all
make release
```

**选项 C：手动编译**

```bash
# Windows x64
GOOS=windows GOARCH=amd64 go build -o bin/SmartDNSSort-windows-x64.exe ./cmd/main.go

# Windows x86
GOOS=windows GOARCH=386 go build -o bin/SmartDNSSort-windows-x86.exe ./cmd/main.go

# Linux x64
GOOS=linux GOARCH=amd64 go build -o bin/SmartDNSSort-debian-x64 ./cmd/main.go

# Linux x86
GOOS=linux GOARCH=386 go build -o bin/SmartDNSSort-debian-x86 ./cmd/main.go

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o bin/SmartDNSSort-debian-arm64 ./cmd/main.go
```

### 第3步：验证编译产物
```bash
# 列出所有编译文件
ls -lh bin/

# 输出示例：
# SmartDNSSort-debian-arm64  (10.1 MB)
# SmartDNSSort-debian-x64    (10.8 MB)
# SmartDNSSort-debian-x86    (10.6 MB)
# SmartDNSSort-windows-x64.exe (11.0 MB)
# SmartDNSSort-windows-x86.exe (10.8 MB)
```

### 第4步：发布到 GitHub Releases

1. **在 GitHub 上创建新 Release**
   - 访问: https://github.com/lee-alone/SmartDNSSort/releases/new
   - 填写版本号（如 `v1.0.0`）
   - 填写发布名称和描述

2. **添加版本说明**

示例模板：
```markdown
## 🎉 SmartDNSSort v1.0.0 发布

### 📦 支持平台
- ✅ Windows x64 (64-bit)
- ✅ Windows x86 (32-bit)  
- ✅ Linux x64 (64-bit)
- ✅ Linux x86 (32-bit)
- ✅ Linux ARM64

### ✨ 新增功能
- [功能1 描述]
- [功能2 描述]
- [功能3 描述]

### 🐛 修复问题
- [问题1 修复]
- [问题2 修复]

### 📝 变更日志
详见: [CHANGELOG](docs/development/IMPLEMENTATION_CHANGELOG.md)

### 💾 文件说明
- `SmartDNSSort-windows-x64.exe` - Windows 64位版本
- `SmartDNSSort-windows-x86.exe` - Windows 32位版本
- `SmartDNSSort-debian-x64` - Linux 64位版本
- `SmartDNSSort-debian-x86` - Linux 32位版本
- `SmartDNSSort-debian-arm64` - Linux ARM64版本（树莓派等）

### 🚀 快速开始
详见: [README.md](README.md)

### 📖 文档
- [使用指南](docs/guides/USAGE_GUIDE.md)
- [Linux 安装](docs/linux/LINUX_INSTALL.md)
- [开发文档](docs/development/DEVELOP.md)
```

3. **上传编译产物**
   - 将 `bin/` 目录下的所有文件拖拽到 Release 页面
   - 或点击 "Attach binaries by dropping them here or selecting them" 进行上传

4. **发布**
   - 点击 "Publish release" 按钮完成发布

## 📂 项目结构更新

```
SmartDNSSort/
├── bin/                          # ✅ 编译产物输出目录（已添加到.gitignore）
├── README.md                     # ✅ 根目录使用说明（新建）
├── build.sh                      # ✅ Linux/macOS 构建脚本（新建）
├── build.bat                     # ✅ Windows CMD 构建脚本（新建）
├── build.ps1                     # ✅ Windows PowerShell 构建脚本（新建）
├── Makefile                      # ✅ 已优化的 Makefile
├── .gitignore                    # ✅ 已更新
├── config.yaml                   # 配置文件
├── cmd/                          # 应用入口
├── dnsserver/                    # DNS 服务器核心
├── cache/                        # 缓存模块
├── ping/                         # 延迟测试模块
├── upstream/                     # 上游服务器管理
├── web/                          # Web UI 文件
├── webapi/                       # Web API 接口
├── config/                       # 配置管理
├── stats/                        # 统计模块
├── sysinstall/                   # 系统安装模块
└── docs/                         # 文档（集中）
    ├── general/                  # 通用文档
    ├── guides/                   # 使用指南
    ├── linux/                    # Linux 相关
    ├── development/              # 开发文档
    └── completion/               # 完成报告
```

## 🔧 CI/CD 集成建议（可选）

### GitHub Actions 自动化编译

创建 `.github/workflows/build.yml`：

```yaml
name: Build and Release

on:
  push:
    tags:
      - 'v*'

jobs:
  build:
    runs-on: ubuntu-latest
    
    strategy:
      matrix:
        include:
          - goos: windows
            goarch: amd64
            output: SmartDNSSort-windows-x64.exe
          - goos: windows
            goarch: 386
            output: SmartDNSSort-windows-x86.exe
          - goos: linux
            goarch: amd64
            output: SmartDNSSort-debian-x64
          - goos: linux
            goarch: 386
            output: SmartDNSSort-debian-x86
          - goos: linux
            goarch: arm64
            output: SmartDNSSort-debian-arm64
    
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.25'
      
      - name: Build
        run: |
          mkdir -p bin
          GOOS=${{ matrix.goos }} GOARCH=${{ matrix.goarch }} go build -o bin/${{ matrix.output }} ./cmd/main.go
      
      - name: Upload Release Asset
        uses: softprops/action-gh-release@v1
        with:
          files: bin/${{ matrix.output }}
```

## 📊 性能指标

编译时间（参考）：
- Windows x64: ~3-5 秒
- Windows x86: ~3-5 秒
- Linux x64: ~3-5 秒
- Linux x86: ~3-5 秒
- Linux ARM64: ~3-5 秒

文件大小（参考）：
- Windows: ~11 MB
- Linux: ~10-11 MB

## 🎯 最佳实践

1. **版本号管理**
   - 使用语义化版本：v主.次.补
   - 例如：v1.0.0, v1.1.0, v2.0.0

2. **发布前检查清单**
   - [ ] 所有代码已提交
   - [ ] 更新了 `docs/development/IMPLEMENTATION_CHANGELOG.md`
   - [ ] 编译所有平台版本
   - [ ] 验证编译产物可正常运行
   - [ ] 清理了调试文件
   - [ ] 更新了 README.md

3. **发布命名规范**
   - Release 标签：`v1.0.0`
   - Release 名称：`SmartDNSSort v1.0.0`
   - 文件名：保持一致的命名规范

4. **更新文档**
   - 每次发布时更新 `docs/development/IMPLEMENTATION_CHANGELOG.md`
   - 在 GitHub Releases 中提供详细说明
   - 保持 README.md 最新

## 🆘 故障排除

### 编译失败

**问题**：`go: command not found`
**解决**：安装 Go 或确保 Go 在 PATH 中

**问题**：权限错误（Linux）
**解决**：
```bash
chmod +x build.sh
./build.sh
```

### 文件未出现在 Release

**问题**：上传的文件未显示
**解决**：
1. 检查网络连接
2. 尝试手动重新上传
3. 检查文件大小限制

### 执行权限（Linux）

**问题**：Linux 二进制文件无法执行
**解决**：
```bash
chmod +x SmartDNSSort-debian-x64
./SmartDNSSort-debian-x64 -h
```

## 📚 相关文档

- [项目 README](README.md)
- [使用指南](docs/guides/USAGE_GUIDE.md)
- [变更日志](docs/development/IMPLEMENTATION_CHANGELOG.md)
- [开发文档](docs/development/DEVELOP.md)
- [Linux 安装](docs/linux/LINUX_INSTALL.md)

## ✨ 总结

通过本次优化，SmartDNSSort 项目现已具备：

✅ 自动化跨平台编译
✅ 统一的产物输出目录
✅ 清晰的版本命名规范
✅ 完整的项目文档
✅ 便捷的发布流程
✅ 多种编译方式支持

项目已准备好进行规范的 GitHub 发布流程！

---

**最后更新**：2025-11-15
**维护者**：lee-alone
