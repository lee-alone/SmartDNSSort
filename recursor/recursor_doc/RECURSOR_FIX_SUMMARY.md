# Recursor 二进制解压问题修复总结

## 问题症状

```
[ERROR] Failed to start new recursor: failed to extract unbound binary: 
unbound binary not found for windows/amd64: open binaries\windows\unbound.exe: file does not exist
```

## 根本原因

1. **`//go:embed` 指令不完整** - 没有正确包含所有文件
2. **路径分隔符不匹配** - `filepath.Join()` 在 Windows 上使用 `\`，但 `embed.FS` 总是使用 `/`

## 修复方案

### 修改 1：`recursor/embedded.go` - 第 22 行

**之前**：
```go
//go:embed binaries/linux/unbound binaries/windows/unbound.exe data/root.key
```

**之后**：
```go
//go:embed binaries/* data/*
```

### 修改 2：`recursor/embedded.go` - `ExtractUnboundBinary()` 函数

**之前**：
```go
binPath := filepath.Join("binaries", platform, binName)
```

**之后**：
```go
binPath := "binaries/" + platform + "/" + binName
```

## 验证修复

编译后，检查 `unbound/` 目录是否包含：
- `unbound.exe` (Windows) 或 `unbound` (Linux)
- `root.key`
- `unbound.conf` (启动时生成)

## 日志输出示例

修复后的正常启动日志：
```
[INFO] Initializing new recursor on port 5353...
[Recursor] Extracted unbound binary to: unbound\unbound.exe
[Recursor] Generated config file: unbound\unbound.conf
[Recursor] Unbound process started (PID: 12345)
```

## 关键要点

| 项目 | 说明 |
|------|------|
| 解压位置 | `<主程序目录>/unbound/` |
| 二进制文件 | `unbound.exe` (Windows) 或 `unbound` (Linux) |
| 配置文件 | `unbound.conf` (动态生成) |
| 信任锚 | `root.key` (DNSSEC) |
| 路径分隔符 | embed.FS 总是使用 `/` |

## 下一步

1. 重新编译：`go build -o smartdnssort.exe ./cmd`
2. 启用递归配置
3. 启动程序并验证 `unbound/` 目录
4. 查看日志确认成功
