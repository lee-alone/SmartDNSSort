# SmartDNSSort 快速参考卡

## 🚀 快速编译

### Windows (PowerShell)
```powershell
# 编译当前平台
go build -o bin/SmartDNSSort.exe ./cmd/main.go

# 编译所有平台
.\build.ps1 all

# 或使用 CMD
build.bat all
```

### Linux/macOS (Bash)
```bash
# 编译当前平台
go build -o bin/SmartDNSSort ./cmd/main.go

# 编译所有平台
chmod +x build.sh
./build.sh all

# 或使用 Makefile
make build-all
```

---

## 📦 编译产物位置

所有编译产物输出到 `./bin/` 目录：

| 文件名 | 平台 | 架构 |
|--------|------|------|
| `SmartDNSSort-windows-x64.exe` | Windows | 64-bit |
| `SmartDNSSort-windows-x86.exe` | Windows | 32-bit |
| `SmartDNSSort-debian-x64` | Linux | 64-bit |
| `SmartDNSSort-debian-x86` | Linux | 32-bit |
| `SmartDNSSort-debian-arm64` | Linux | ARM64 |

---

## 📋 Makefile 常用命令

```bash
make build              # 编译当前平台
make build-windows      # 编译 Windows (x86+x64)
make build-linux        # 编译 Linux (x86+x64+arm64)
make build-all          # 编译所有平台

make test               # 运行测试
make clean              # 清理编译文件
make release            # 打包发布版本
make help               # 显示帮助
```

---

## 🔨 构建脚本使用

### build.ps1 (Windows PowerShell - 推荐)
```powershell
.\build.ps1              # 编译 Windows 版本
.\build.ps1 windows      # 编译 Windows 版本
.\build.ps1 linux        # 编译 Linux 版本
.\build.ps1 all          # 编译所有平台
.\build.ps1 help         # 显示帮助
```

### build.bat (Windows CMD)
```batch
build.bat                # 编译 Windows 版本
build.bat windows        # 编译 Windows 版本
build.bat linux          # 编译 Linux 版本
build.bat all            # 编译所有平台
build.bat help           # 显示帮助
```

### build.sh (Linux/macOS)
```bash
./build.sh               # 编译 Linux 版本
./build.sh linux         # 编译 Linux 版本
./build.sh windows       # 编译 Windows 版本
./build.sh all           # 编译所有平台
./build.sh clean         # 清理文件
./build.sh help          # 显示帮助
```

---

## 🖥️ 运行应用

### Windows
```powershell
# 直接运行
.\bin\SmartDNSSort-windows-x64.exe

# 使用配置文件
.\bin\SmartDNSSort-windows-x64.exe -c config.yaml

# 显示帮助
.\bin\SmartDNSSort-windows-x64.exe -h
```

### Linux
```bash
# 直接运行
./bin/SmartDNSSort-debian-x64

# 使用配置文件
./bin/SmartDNSSort-debian-x64 -c config.yaml

# 显示帮助
./bin/SmartDNSSort-debian-x64 -h
```

### Linux 系统服务
```bash
# 安装服务
sudo ./bin/SmartDNSSort-debian-x64 -s install

# 查看状态
./bin/SmartDNSSort-debian-x64 -s status

# 卸载服务
sudo ./bin/SmartDNSSort-debian-x64 -s uninstall
```

---

## 📤 发布到 GitHub

### 1️⃣ 编译所有版本
```powershell
.\build.ps1 all     # Windows
```

```bash
./build.sh all      # Linux/macOS
```

### 2️⃣ 验证编译产物
```bash
ls -lh bin/         # Linux/macOS
dir bin\            # Windows
```

### 3️⃣ 在 GitHub 创建 Release
- 访问: https://github.com/lee-alone/SmartDNSSort/releases/new
- 填写版本号: `v1.0.0`
- 上传 `bin/` 中的所有文件
- 发布

### 4️⃣ 发布说明示例
```markdown
## SmartDNSSort v1.0.0 发布

### 支持平台
- Windows x64 / x86
- Linux x64 / x86 / ARM64

### 下载
选择适合您平台的版本下载：
- [SmartDNSSort-windows-x64.exe](../) - Windows 64位
- [SmartDNSSort-debian-x64](../) - Linux 64位

### 快速开始
详见: [README.md](../README.md)
```

---

## 📝 命令行参数

```bash
# 基本用法
SmartDNSSort [选项]

# 选项
-s <命令>       系统服务管理（仅Linux）
                install, uninstall, status
-c <路径>      配置文件路径（默认：config.yaml）
-w <路径>      工作目录（默认：当前目录）
-user <用户>   运行用户（仅limit install）
-dry-run       干运行模式
-v             详细输出
-h             显示帮助
```

---

## 🧪 测试

### 运行单元测试
```bash
go test -v ./...
```

### 运行并检查竞态
```bash
go test -v -race ./...
```

### 运行特定包的测试
```bash
go test -v ./cache
go test -v ./ping
go test -v ./dnsserver
```

---

## 🔍 代码质量

### 代码格式化
```bash
go fmt ./...
```

### 代码检查
```bash
go vet ./...
```

### Lint (需要安装 golangci-lint)
```bash
golangci-lint run ./...
```

---

## 📁 项目结构

```
SmartDNSSort/
├── bin/                    # ⭐ 编译产物输出目录
├── cmd/                    # 应用入口
├── cache/                  # DNS 缓存
├── ping/                   # 延迟测试
├── dnsserver/              # DNS 服务器
├── upstream/               # 上游管理
├── web/                    # Web UI
├── webapi/                 # Web API
├── config/                 # 配置
├── stats/                  # 统计
├── config.yaml             # 配置文件
├── README.md               # ⭐ 项目说明
├── RELEASE_GUIDE.md        # ⭐ 发布指南
├── build.sh                # ⭐ Linux 构建脚本
├── build.bat               # ⭐ Windows 构建脚本
├── build.ps1               # ⭐ PowerShell 构建脚本
├── Makefile                # ⭐ 优化后的 Makefile
├── .gitignore              # ⭐ 已优化
└── docs/                   # 文档集合
```

---

## 🛠️ 常见任务速查

| 任务 | 命令 |
|------|------|
| **编译 Windows** | `.\build.ps1 windows` |
| **编译 Linux** | `./build.sh linux` |
| **编译全部** | `.\build.ps1 all` / `./build.sh all` |
| **清理编译文件** | `./build.sh clean` / `make clean` |
| **运行测试** | `go test -v ./...` |
| **显示帮助** | `.\build.ps1 help` / `./build.sh help` |
| **查看编译产物** | `ls bin/` / `dir bin\` |

---

## ⚙️ 环境变量

```bash
# 交叉编译指定平台
export GOOS=linux
export GOARCH=amd64

go build -o bin/SmartDNSSort ./cmd/main.go

# Windows 下
$env:GOOS="windows"
$env:GOARCH="amd64"
go build -o bin/SmartDNSSort.exe ./cmd/main.go
```

---

## 📞 需要帮助？

- 📖 完整文档: 见 `README.md` 和 `docs/` 目录
- 🚀 发布指南: 见 `RELEASE_GUIDE.md`
- 💬 问题报告: GitHub Issues
- 📧 联系维护者: lee-alone

---

**最后更新**: 2025-11-15
**适用版本**: SmartDNSSort v1.0+
