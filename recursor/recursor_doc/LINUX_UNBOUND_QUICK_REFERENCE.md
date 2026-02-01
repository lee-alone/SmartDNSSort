# Linux Unbound 管理 - 快速参考

## 你的需求

✅ **第一次启动递归** → 检查是否已有 unbound
✅ **没有就安装** → 安装 unbound
✅ **取消自启动** → `systemctl disable unbound`
✅ **禁止当前进程** → `systemctl stop unbound`

## 当前实现

### 已安装 unbound 的情况

```
1. 获取版本和路径
2. 停止 unbound 服务
3. 禁用自启动
4. 备份配置
5. 启动我们的 unbound 进程
```

### 未安装 unbound 的情况

```
1. 执行安装命令（apt/yum/pacman/apk）
2. 禁用自启动
3. 停止服务
4. 验证安装
5. 启动我们的 unbound 进程
```

## 改进点

### 1. 逻辑更清晰

- 分离了 `executeInstall()` 方法
- 分离了 `StopService()` 方法
- 分离了 `handleExistingUnbound()` 方法

### 2. 错误处理更完善

- `StopService()` 支持备选方法（killall）
- `DisableAutoStart()` 支持备选方法（chkconfig）
- 所有错误都被正确记录

### 3. 代码复用更好

- 停止服务的逻辑统一在 `StopService()` 中
- 禁用自启的逻辑统一在 `DisableAutoStart()` 中

## 关键方法

### InstallUnbound()

```go
// 流程：
// 1. 检查是否已安装
// 2. 如果已安装，处理现有的 unbound
// 3. 如果未安装，执行安装
// 4. 禁用自启动
// 5. 停止当前进程
```

### executeInstall()

```go
// 根据包管理器执行安装
// 支持：apt, yum, pacman, apk
```

### StopService()

```go
// 停止 unbound 服务
// 尝试 systemctl，备选 killall
```

### DisableAutoStart()

```go
// 禁用自启动
// 尝试 systemctl，备选 chkconfig
```

### handleExistingUnbound()

```go
// 处理已存在的 unbound
// 1. 停止服务
// 2. 禁用自启
// 3. 备份配置
```

## 权限要求

需要 root 权限的操作：
- 安装 unbound
- 禁用自启
- 停止服务
- 备份配置

## 使用示例

### 启用递归功能

```bash
# 程序会自动：
# 1. 检查 unbound 是否已安装
# 2. 如果未安装，安装 unbound
# 3. 禁用自启动
# 4. 停止当前进程
# 5. 启动我们的 unbound 进程
```

### 禁用递归功能

```bash
# 程序会自动：
# 1. 停止我们的 unbound 进程
# 2. 清理配置文件
```

## 详细文档

- [LINUX_UNBOUND_MANAGEMENT_DESIGN.md](LINUX_UNBOUND_MANAGEMENT_DESIGN.md) - 完整设计文档
