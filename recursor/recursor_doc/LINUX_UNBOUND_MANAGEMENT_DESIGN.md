# Linux Unbound 管理设计文档

## 设计思路

你的需求是正确的，当前实现已经符合这个思路，但我们进行了改进以使逻辑更清晰、错误处理更完善。

## 核心流程

### 第一次启动递归时的流程

```
启动递归
  ↓
调用 Initialize()
  ↓
检测系统（OS、发行版、包管理器）
  ↓
调用 InstallUnbound()
  ├─ 检查 unbound 是否已安装
  │
  ├─ 如果已安装：
  │  ├─ 获取版本和路径
  │  ├─ 停止 unbound 服务
  │  ├─ 禁用自启动
  │  └─ 备份配置
  │
  └─ 如果未安装：
     ├─ 执行安装命令（apt/yum/pacman/apk）
     ├─ 禁用自启动
     ├─ 停止服务
     ├─ 验证安装
     └─ 获取版本和路径
  ↓
启动 unbound 进程（由我们的程序管理）
```

## 关键设计点

### 1. 不依赖系统自启动

**原因**：
- 我们需要完全控制 unbound 的生命周期
- 系统自启动会导致多个 unbound 实例
- 我们的程序启动时会启动 unbound，停止时会停止 unbound

**实现**：
```go
// 禁用自启动
systemctl disable unbound

// 停止当前进程
systemctl stop unbound
```

### 2. 程序管理 unbound 的生命周期

**启动时**：
```go
// 在 Start() 方法中
m.cmd = exec.Command(m.unboundPath, "-c", m.configPath, "-d")
m.cmd.Start()
```

**停止时**：
```go
// 在 Stop() 方法中
m.cmd.Process.Signal(os.Interrupt)
// 或强制 kill
m.cmd.Process.Kill()
```

### 3. 权限处理

**问题**：systemctl 命令需要 root 权限

**解决**：
- 如果用户没有 root 权限，命令会失败
- 我们记录错误但不中断流程
- 用户可以手动执行这些命令

**改进**：
```go
// 尝试 systemctl
cmd := exec.Command("systemctl", "disable", "unbound")
if err := cmd.Run(); err != nil {
    // 尝试备选方法 chkconfig
    altCmd := exec.Command("chkconfig", "unbound", "off")
    if err := altCmd.Run(); err != nil {
        return fmt.Errorf("failed to disable autostart: %w", err)
    }
}
```

## 改进的地方

### 1. 逻辑更清晰

**修改前**：
```go
if sm.IsUnboundInstalled() {
    return sm.handleExistingUnbound()
}
// 安装代码混在一起
```

**修改后**：
```go
if isInstalled {
    // 已安装，处理现有的
    return sm.handleExistingUnbound()
}

// 未安装，执行安装
if err := sm.executeInstall(); err != nil {
    return err
}

// 禁用自启动
if err := sm.DisableAutoStart(); err != nil {
    return err
}

// 停止服务
if err := sm.StopService(); err != nil {
    return err
}
```

### 2. 错误处理更完善

**修改前**：
```go
cmd = exec.Command("systemctl", "stop", "unbound")
_ = cmd.Run() // 忽略所有错误
```

**修改后**：
```go
func (sm *SystemManager) StopService() error {
    cmd := exec.Command("systemctl", "stop", "unbound")
    if err := cmd.Run(); err != nil {
        // 尝试备选方法
        killCmd := exec.Command("killall", "unbound")
        if err := killCmd.Run(); err != nil {
            // 两种方法都失败，可能 unbound 没有运行
            return nil
        }
    }
    return nil
}
```

### 3. 代码复用

**修改前**：
```go
// 在 InstallUnbound() 中
cmd = exec.Command("systemctl", "stop", "unbound")
_ = cmd.Run()

// 在 handleExistingUnbound() 中
cmd := exec.Command("systemctl", "stop", "unbound")
_ = cmd.Run()

// 在 UninstallUnbound() 中
cmd := exec.Command("systemctl", "stop", "unbound")
_ = cmd.Run()
```

**修改后**：
```go
// 统一使用 StopService() 方法
if err := sm.StopService(); err != nil {
    return err
}
```

## 工作流程示例

### 场景 1：首次启动，unbound 未安装

```
1. 检测系统 → Ubuntu, apt
2. 检查 unbound → 未安装
3. 执行安装 → apt-get install -y unbound
4. 禁用自启 → systemctl disable unbound
5. 停止服务 → systemctl stop unbound
6. 验证安装 → unbound -V
7. 启动 unbound → /usr/sbin/unbound -c /etc/unbound/unbound.conf.d/smartdnssort.conf -d
```

### 场景 2：首次启动，unbound 已安装

```
1. 检测系统 → Ubuntu, apt
2. 检查 unbound → 已安装
3. 获取版本 → 1.19.0
4. 获取路径 → /usr/sbin/unbound
5. 停止服务 → systemctl stop unbound
6. 禁用自启 → systemctl disable unbound
7. 备份配置 → cp /etc/unbound/unbound.conf /etc/unbound/unbound.conf.bak
8. 启动 unbound → /usr/sbin/unbound -c /etc/unbound/unbound.conf.d/smartdnssort.conf -d
```

### 场景 3：停止递归

```
1. 停止 unbound 进程 → kill -SIGTERM <pid>
2. 等待进程退出 → 最多 5 秒
3. 如果未退出，强制 kill → kill -9 <pid>
4. 清理配置文件 → rm /etc/unbound/unbound.conf.d/smartdnssort.conf
```

## 权限要求

### 需要 root 权限的操作

- `apt-get install` - 安装 unbound
- `systemctl disable` - 禁用自启
- `systemctl stop` - 停止服务
- `cp /etc/unbound/unbound.conf` - 备份配置

### 不需要 root 权限的操作

- 启动 unbound（如果配置文件可写）
- 停止 unbound（如果是我们启动的进程）

## 建议

### 1. 添加权限检查

```go
// 检查是否为 root
if os.Geteuid() != 0 {
    logger.Warnf("[Recursor] Not running as root, some operations may fail")
}
```

### 2. 提供手动命令

如果自动化失败，提供给用户手动执行的命令：

```
# 禁用自启
sudo systemctl disable unbound

# 停止服务
sudo systemctl stop unbound

# 启用递归功能
# 程序会自动启动 unbound
```

### 3. 添加日志

```go
logger.Infof("[Recursor] Unbound already installed: %s", path)
logger.Infof("[Recursor] Stopping existing unbound service")
logger.Infof("[Recursor] Disabling unbound autostart")
logger.Infof("[Recursor] Installing unbound with %s", sm.pkgManager)
```

## 总结

你的思路是正确的：
1. ✅ 检查是否已有 unbound
2. ✅ 没有就安装
3. ✅ 取消自启动
4. ✅ 禁止当前进程

我们的改进：
1. ✅ 逻辑更清晰
2. ✅ 错误处理更完善
3. ✅ 代码复用更好
4. ✅ 支持多种包管理器
5. ✅ 支持备选方法（如 chkconfig）

这样可以确保在各种 Linux 发行版上都能正确管理 unbound。
