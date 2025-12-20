# sysinstall 包

系统安装器包，用于 SmartDNSSort 的 Linux 系统安装和管理。

## 文件结构

- **installer.go** - 核心结构和主要流程
  - `InstallerConfig` - 安装配置结构
  - `SystemInstaller` - 系统安装器主类
  - `Install()` - 执行安装流程
  - `Uninstall()` - 执行卸载流程
  - `Status()` - 显示服务状态

- **installer_setup.go** - 安装前的准备工作
  - `CheckSystemd()` - 检查 systemd 支持
  - `CreateDirectories()` - 创建必要的目录
  - `GenerateDefaultConfig()` - 生成默认配置文件

- **installer_files.go** - 文件操作相关
  - `CopyBinary()` - 复制二进制文件
  - `CopyWebFiles()` - 复制 Web 静态文件
  - `RemoveDirectories()` - 删除相关目录
  - `copyDirRecursive()` - 递归复制目录

- **installer_service.go** - systemd 服务管理
  - `GenerateServiceFile()` - 生成服务文件内容
  - `WriteServiceFile()` - 写入服务文件
  - `ReloadSystemd()` - 重新加载 systemd
  - `EnableService()` - 启用服务
  - `StartService()` - 启动服务
  - `StopService()` - 停止服务
  - `DisableService()` - 禁用服务
  - `GetServiceStatus()` - 获取服务状态
  - `GetServiceDetails()` - 获取服务详细信息
  - `GetRecentLogs()` - 获取最近日志
  - `RemoveServiceFile()` - 删除服务文件

## 使用示例

```go
// 创建安装器
cfg := InstallerConfig{
    ConfigPath: "/etc/SmartDNSSort/config.yaml",
    WorkDir:    "/var/lib/SmartDNSSort",
    RunUser:    "root",
    DryRun:     false,
    Verbose:    true,
}

installer := NewSystemInstaller(cfg)

// 执行安装
if err := installer.Install(); err != nil {
    log.Fatal(err)
}

// 查看状态
if err := installer.Status(); err != nil {
    log.Fatal(err)
}

// 卸载
if err := installer.Uninstall(); err != nil {
    log.Fatal(err)
}
```
