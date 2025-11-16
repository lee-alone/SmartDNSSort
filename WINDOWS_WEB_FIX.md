# 🔧 Windows Web 文件路径修复

## 问题描述

Windows 下运行程序时，无法找到 `web` 目录，导致 Web UI 无法访问。

## 根本原因

原来的路径查找顺序不适合 Windows 开发环境：
```
1. /var/lib/SmartDNSSort/web     ← Linux 专用
2. /usr/share/smartdnssort/web   ← Linux 专用
3. /etc/SmartDNSSort/web         ← Linux 专用
4. ./web                          ← 需要正确的工作目录
5. web                            ← 需要正确的工作目录
6. <可执行文件目录>/web          ← 太靠后
```

**问题：** 在 Windows 上，Linux 路径永远无法找到，而相对路径 `./web` 和 `web` 只在特定工作目录下有效。

## 解决方案

重新优化路径查找顺序，**优先 Windows 开发环境路径**：

```
1. <可执行文件目录>/web          ← Windows 首选（最先查找）
2. <可执行文件目录>/../web       ← 上级目录（bin 目录结构）
3. ./web                          ← 当前工作目录相对路径
4. web                            ← 当前工作目录相对路径
5. /var/lib/SmartDNSSort/web     ← Linux 服务部署
6. /usr/share/smartdnssort/web   ← FHS 标准
7. /etc/SmartDNSSort/web         ← Linux 备选
```

## 修改的代码

**文件：** `webapi/api.go`  
**函数：** `findWebDirectory()`

### 修改前
```go
possiblePaths := []string{
    "/var/lib/SmartDNSSort/web",   // Linux 优先（不适合 Windows）
    "/usr/share/smartdnssort/web",
    "/etc/SmartDNSSort/web",
    "./web",
    "web",
}

// 可执行文件目录加到最后
if exePath, err := os.Executable(); err == nil {
    execDir := filepath.Dir(exePath)
    possiblePaths = append([]string{
        filepath.Join(execDir, "web"),
    }, possiblePaths...)
}
```

### 修改后
```go
possiblePaths := []string{}

// 首先：在可执行文件目录查找 web 目录（对 Windows 最有效）
if exePath, err := os.Executable(); err == nil {
    execDir := filepath.Dir(exePath)
    possiblePaths = append(possiblePaths,
        filepath.Join(execDir, "web"),
        filepath.Join(execDir, "..", "web"), // 上级目录的 web
    )
}

// 当前工作目录相对路径（开发环境）
possiblePaths = append(possiblePaths,
    "./web",
    "web",
)

// Linux 系统路径（Linux 服务部署）
possiblePaths = append(possiblePaths,
    "/var/lib/SmartDNSSort/web",
    "/usr/share/smartdnssort/web",
    "/etc/SmartDNSSort/web",
)
```

## 优点

✅ **Windows 开发环境更优先** - `bin\SmartDNSSort.exe` 附近的 `web\` 目录会被首先找到  
✅ **灵活的目录结构支持** - 支持 `bin/../web` 这样的构造  
✅ **完全向后兼容** - Linux 路径仍然支持，但优先级降低  
✅ **开发和生产都支持** - 同一个二进制在两个平台都能工作  

## 使用场景

### Windows 开发环境
```
project/
├── bin/
│   └── SmartDNSSort.exe         ← 程序从这里找 web
├── web/                          ← 在 ../web 找到
│   └── index.html
└── config.yaml
```

程序运行时：
1. 检查 `bin/web/` → 找不到
2. 检查 `bin/../web/` → ✓ 找到！

### Windows 编译输出目录
```
SmartDNSSort/bin/
├── SmartDNSSort.exe
├── web/                         ← 或直接放在同级
│   └── index.html
```

程序运行时：
1. 检查 `bin/web/` → ✓ 找到！

### Linux 生产环境（保持不变）
```
/var/lib/SmartDNSSort/
├── web/                         ← 系统安装时复制
│   └── index.html
```

程序运行时：
1. 检查可执行文件目录 → 找不到
2. 检查相对路径 → 找不到
3. 检查 `/var/lib/SmartDNSSort/web/` → ✓ 找到！

## 编译信息

```
Windows 版本：SmartDNSSort.exe (9.87 MB)
Linux x64 版本：SmartDNSSort-linux-x64 (10.3 MB)
```

两个版本都已重新编译，修复已生效。

## 测试方法

### Windows 测试

```bash
# 方式 1：在项目根目录运行
cd d:\gb\SmartDNSSort
.\bin\SmartDNSSort.exe -c config.yaml

# 应该看到：
# Web UI server started on http://localhost:8080
# Using web directory: web

# 或
# Using web directory: D:\gb\SmartDNSSort\web  (如果 bin 同级有 web)
```

### Linux 测试

```bash
# 部署到 /var/lib/SmartDNSSort/
sudo ./SmartDNSSort-linux-x64 -s install

# 应该看到：
# Using web directory: /var/lib/SmartDNSSort/web
```

## 故障排除

### 问题：仍然找不到 web 目录

**解决方案 1：检查 web 目录位置**
```bash
# Windows 中，web 应该在以下位置之一：
# 1. SmartDNSSort.exe 同级目录
# 2. SmartDNSSort.exe 上级目录  
# 3. 当前工作目录

# 查看程序搜索的路径
# 在代码中添加 log.Printf("Looking for web in: %s\n", path)
```

**解决方案 2：使用 Web UI 禁用选项**
```bash
# 如果没有 web 文件，可以禁用 Web UI
./SmartDNSSort.exe -c config.yaml

# 在 config.yaml 中修改：
# webui:
#   enabled: false
```

### 问题：Web UI 显示但页面空白

**检查步骤：**
```bash
# 1. 查看 index.html 是否存在
dir web\

# 2. 查看 Web 服务是否正常启动
# 查看日志消息，应该有 "Using web directory: ..." 的输出

# 3. 检查防火墙
# 确保 8080 端口未被防火墙阻止
```

## 相关文件

- `webapi/api.go` - Web API 和文件服务实现
- `bin/SmartDNSSort.exe` - Windows 编译版本（已更新）
- `bin/SmartDNSSort-linux-x64` - Linux 编译版本（已更新）

## 修复日期

**2025 年 11 月 15 日**

---

## 总结

✅ 修改了路径查找优先级  
✅ Windows 和 Linux 都支持  
✅ 完全向后兼容  
✅ 两个平台的二进制都已重新编译  
✅ 可以直接使用新的二进制文件
