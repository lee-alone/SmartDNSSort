# Recursor 二进制路径迁移总结

## 问题

打包后在 Windows 下启用递归时出现错误：
```
[ERROR] Failed to start new recursor: failed to extract unbound binary: 
unbound binary not found for windows/amd64: open binaries\windows\unbound.exe: file does not exist
```

原因：
1. 嵌入的二进制文件在打包时丢失
2. `embed.FS` 总是使用正斜杠 `/`，但 `filepath.Join()` 在 Windows 上使用反斜杠 `\`

## 解决方案

### 1. 修复 `//go:embed` 指令

使用通配符模式确保所有文件都被嵌入：
```go
//go:embed binaries/* data/*
var unboundBinaries embed.FS
```

### 2. 修复路径问题

在 `embed.FS` 中读取文件时，必须使用正斜杠 `/`：
```go
// ❌ 错误 - Windows 上会变成 binaries\windows\unbound.exe
binPath := filepath.Join("binaries", platform, binName)

// ✅ 正确 - 总是使用正斜杠
binPath := "binaries/" + platform + "/" + binName
```

### 3. 解压到主程序目录

将 Unbound 二进制文件和配置文件解压到主程序所在目录下的 `unbound/` 子目录。

## 修改内容

### `recursor/embedded.go`

**关键变更**：
- 修改 `//go:embed` 指令为 `binaries/* data/*`
- 修复 `ExtractUnboundBinary()` 中的路径构建，使用正斜杠
- 所有文件解压到主程序目录下的 `unbound/` 子目录

**关键函数**：
```go
// 解压二进制到主程序目录
ExtractUnboundBinary() (string, error)

// 获取配置目录
GetUnboundConfigDir() (string, error)

// 清理 unbound 目录
CleanupUnboundFiles() error

// 自定义目录（可选）
SetUnboundDir(dir string)
```

### `recursor/manager.go`

**变更**：
- 更新 `Start()` 方法注释，说明新的路径策略
- 添加日志输出，便于调试
- 更新 `Stop()` 方法注释，说明清理策略

**新增日志**：
```
[Recursor] Extracted unbound binary to: <path>
[Recursor] Generated config file: <path>
[Recursor] Unbound process started (PID: <pid>)
[Recursor] Unbound process stopped
```

## 目录结构

```
<主程序目录>/
├── smartdnssort.exe
├── config.yaml
└── unbound/
    ├── unbound.exe
    ├── unbound.conf
    └── root.key
```

## 优势

1. ✅ **便于调试** - 所有文件在主程序目录下，易于查看
2. ✅ **权限简化** - 不需要临时目录权限
3. ✅ **打包友好** - 便于容器化和系统集成
4. ✅ **文件持久化** - 配置文件可保留用于调试
5. ✅ **跨平台兼容** - 正确处理 Windows 和 Linux 路径差异

## 测试步骤

1. 重新编译项目：
   ```bash
   go build -o smartdnssort.exe ./cmd
   ```

2. 启用递归配置：
   ```yaml
   upstream:
     recursor_port: 5353
   ```

3. 启动程序，检查 `unbound/` 目录是否创建并包含文件

4. 查看日志确认启动成功：
   ```
   [Recursor] Extracted unbound binary to: unbound\unbound.exe
   [Recursor] Generated config file: unbound\unbound.conf
   [Recursor] Unbound process started (PID: xxxx)
   ```

## 向后兼容性

- 旧的临时目录路径 (`/tmp/smartdnssort-unbound`) 不再使用
- 如需自定义路径，使用 `recursor.SetUnboundDir()` 函数

## 相关文件

- `recursor/embedded.go` - 嵌入和解压逻辑
- `recursor/manager.go` - 进程管理
- `recursor/UNBOUND_PATH_GUIDE.md` - 详细指南
