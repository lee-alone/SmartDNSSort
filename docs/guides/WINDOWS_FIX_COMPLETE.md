# ✅ Windows Web 文件路径修复 - 完成

## 问题

Windows 下运行 SmartDNSSort 时，无法找到 `web` 目录，Web UI 无法使用。

## 原因

原来的路径查找顺序不适合 Windows 开发环境：
- Linux 路径（`/var/lib/` 等）在 Windows 上永远找不到
- 相对路径 `./web` 和 `web` 依赖于当前工作目录
- 可执行文件目录的查找优先级太低

## 解决方案

修改 `webapi/api.go` 中的 `findWebDirectory()` 函数，**重新优化路径查找顺序**：

### 新的查找顺序

```
1. <可执行文件目录>/web          ← Windows 首选（最快找到）
2. <可执行文件目录>/../web       ← 支持 bin 目录结构
3. ./web                         ← 当前工作目录
4. web                           ← 当前工作目录
5. /var/lib/SmartDNSSort/web    ← Linux 服务部署
6. /usr/share/smartdnssort/web  ← Linux FHS 标准
7. /etc/SmartDNSSort/web        ← Linux 备选路径
```

## 改进点

✅ **Windows 开发环境优先** - 在可执行文件所在目录优先查找  
✅ **灵活目录结构** - 支持 `bin/../web` 这样的构造  
✅ **完全向后兼容** - Linux 系统路径仍然支持  
✅ **开发和生产通用** - 同一个代码在 Windows 和 Linux 都能工作  

## 使用说明

### Windows 开发环境

**方式 1：Web 目录在可执行文件同级**
```
SmartDNSSort/
├── bin/
│   └── SmartDNSSort.exe   ← 运行这个
├── web/                   ← 程序会找到这里
│   └── index.html
└── config.yaml
```

运行时：
```bash
.\bin\SmartDNSSort.exe -c config.yaml
# → Using web directory: <项目路径>/web ✓
```

**方式 2：Web 目录在 bin 同级**
```
SmartDNSSort/bin/
├── SmartDNSSort.exe   ← 运行这个
└── web/               ← 在上级目录的 web
    └── index.html
```

运行时（从 bin 目录）：
```bash
.\SmartDNSSort.exe
# → Using web directory: <项目路径>/web ✓
```

### Linux 部署（保持不变）

系统安装时自动复制到 `/var/lib/SmartDNSSort/web/`，程序会自动找到。

## 编译信息

所有二进制已重新编译：

- ✅ `SmartDNSSort.exe` (9.87 MB) - Windows 用
- ✅ `SmartDNSSort-linux-x64` (10.3 MB) - Linux 用

## 验证

### Windows 测试

```bash
# 在项目根目录运行
cd d:\gb\SmartDNSSort
.\bin\SmartDNSSort.exe -c config.yaml
```

**成功标志：**
```
Web API server started on http://localhost:8080
Using web directory: D:\gb\SmartDNSSort\web
```

然后在浏览器访问 `http://127.0.0.1:8080/` 应该能看到 Web UI。

### Linux 验证

```bash
sudo systemctl status SmartDNSSort
# → active (running)

curl http://127.0.0.1:8080/
# → 返回 HTML 内容，不是 404
```

## 修改的代码

**文件：** `webapi/api.go`  
**行数：** 约 85-115 行

关键改进：
1. 首先检查可执行文件目录
2. 然后检查相对路径（开发环境）
3. 最后检查 Linux 系统路径（生产环境）

## 文档

- `WINDOWS_WEB_FIX.md` - 详细的修复说明和测试方法

---

**修复日期：** 2025 年 11 月 15 日  
**修复状态：** ✅ 完成  
**影响范围：** Windows 和 Linux 开发/部署都兼容
