# Unbound 二进制文件路径指南

## 概述

从 v2.0 开始，Unbound 二进制文件和配置文件解压到**主程序所在目录**下的 `unbound/` 子目录，而不是系统临时目录。

## 目录结构

```
<主程序目录>/
├── smartdnssort.exe (或 smartdnssort)
├── config.yaml
└── unbound/                    # Unbound 相关文件目录
    ├── unbound.exe             # Windows 二进制
    ├── unbound                 # Linux 二进制
    ├── unbound.conf            # 动态生成的配置文件
    └── root.key                # DNSSEC 信任锚文件
```

## 优势

1. **便于调试** - 所有文件在主程序目录下，易于查看和管理
2. **权限简化** - 不需要临时目录的特殊权限
3. **打包友好** - 便于容器化和系统集成
4. **文件持久化** - 配置文件可以保留用于调试

## 文件说明

| 文件 | 来源 | 说明 |
|------|------|------|
| `unbound.exe` / `unbound` | 嵌入式 | 递归解析器二进制程序 |
| `unbound.conf` | 动态生成 | 根据 CPU 核数和配置动态生成 |
| `root.key` | 嵌入式 | DNSSEC 信任锚文件 |

## 自定义路径

如果需要使用不同的目录，可以在启动前调用：

```go
import "smartdnssort/recursor"

// 设置自定义目录
recursor.SetUnboundDir("/custom/path/to/unbound")

// 然后启动 Manager
manager.Start()
```

## 清理

停止 Recursor 时会自动清理 `unbound/` 目录下的文件：

```go
manager.Stop()  // 自动清理 unbound/ 目录
```

## 故障排查

### 错误：`unbound binary not found`

**原因**：嵌入的二进制文件在打包时丢失

**解决**：
1. 确保 `recursor/binaries/` 目录在源代码中
2. 使用 `go build` 而不是其他打包工具
3. 检查 `embedded.go` 中的 `//go:embed` 指令

### 错误：`permission denied`

**原因**：主程序目录没有写权限

**解决**：
1. 确保主程序目录可写
2. 或使用 `SetUnboundDir()` 指定可写目录

### 错误：`failed to start unbound process`

**原因**：
1. 二进制文件损坏
2. 配置文件生成失败
3. 端口被占用

**解决**：
1. 检查 `unbound/` 目录下的文件
2. 查看日志输出
3. 确保端口 5353 未被占用
