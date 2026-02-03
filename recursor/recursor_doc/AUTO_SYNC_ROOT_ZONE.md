# Unbound 自动同步 Root.Zone 实现

## 概述

通过配置 unbound 的 `auth-zone` 让它自动从根服务器同步 root.zone，完全消除了手动定期更新的需要。

## 问题背景

之前的实现方式：
- 应用启动时下载 root.zone 文件
- 每 7 天定期检查并更新一次
- 需要网络连接和额外的定时任务

新的实现方式：
- 让 unbound 自己管理 root.zone 的更新
- unbound 会自动从根服务器同步最新数据
- 完全自动化，无需我们干预

## 技术方案

### Auth-Zone 配置

```unbound
auth-zone:
    name: "."
    zonefile: "unbound/root.zone"
    
    # 配置根服务器作为权威数据源
    primary: 192.0.32.132      # b.root-servers.net (IPv4)
    primary: 192.0.47.132      # x.root-servers.net (IPv4)
    primary: 2001:500:12::d0d  # b.root-servers.net (IPv6)
    primary: 2001:500:1::53    # x.root-servers.net (IPv6)
    
    # 如果网络断开，回退到普通递归查询
    fallback-enabled: yes
    
    # 让递归模块使用本地根数据，加速递归查询
    for-upstream: yes
    
    # 不向外部暴露根区数据，保护隐私
    for-downstream: no
```

### 工作原理

1. **初始化**：应用启动时确保 root.zone 文件存在
2. **自动同步**：unbound 定期从配置的根服务器检查更新
3. **本地缓存**：更新的数据保存到 zonefile
4. **递归加速**：递归查询时使用本地根数据，避免网络查询
5. **容错机制**：网络断开时自动回退到普通递归

### 关键参数说明

| 参数 | 值 | 说明 |
|------|-----|------|
| `name` | "." | 根域 |
| `zonefile` | "unbound/root.zone" | 本地存储路径 |
| `primary` | 根服务器地址 | 权威数据源，支持多个 |
| `fallback-enabled` | yes | 网络故障时回退 |
| `for-upstream` | yes | 递归查询使用本地数据 |
| `for-downstream` | no | 不向外部暴露 |

## 代码变更

### 1. GetRootZoneConfig() 方法

**文件**：`recursor/manager_rootzone.go`

**变更**：
- 移除了文件存在性检查
- 添加了根服务器配置
- 启用了自动同步参数

**优势**：
- 配置更简洁
- 功能更强大
- 完全自动化

### 2. Manager.Start() 方法

**文件**：`recursor/manager.go`

**变更**：
- 移除了 `UpdateRootZonePeriodically()` 调用
- 保留了初始化逻辑确保文件存在

**优势**：
- 减少了 goroutine 数量
- 降低了 CPU 占用
- 简化了生命周期管理

### 3. UpdateRootZonePeriodically() 方法

**文件**：`recursor/manager_rootzone.go`

**状态**：已弃用，保留以保持向后兼容性

## 性能对比

### 旧方案
- 启动时：下载 root.zone（~2-3MB）
- 每 7 天：检查更新，可能重新下载
- 定时任务：持续运行，占用资源
- 网络依赖：需要网络连接才能更新

### 新方案
- 启动时：确保文件存在（快速）
- 自动同步：unbound 后台处理，无额外开销
- 无定时任务：完全由 unbound 管理
- 网络容错：断网时自动回退

## 测试验证

### 验证步骤

1. **启动应用**
   ```bash
   # 观察日志
   [Recursor] Ensuring root.zone file...
   [Recursor] Using existing root.zone file: unbound/root.zone
   [Recursor] Unbound will automatically sync root.zone from root servers
   ```

2. **检查配置**
   ```bash
   # 查看生成的 unbound.conf
   cat unbound/unbound.conf | grep -A 15 "auth-zone"
   ```

3. **监控同步**
   ```bash
   # 观察 unbound 日志
   # unbound 会定期输出同步状态
   ```

4. **验证功能**
   ```bash
   # 测试 DNS 查询
   dig @127.0.0.1 -p 5353 example.com
   ```

## 故障排查

### 问题：root.zone 文件不存在

**原因**：初始化失败或文件被删除

**解决**：
1. 检查 unbound 目录权限
2. 手动删除 root.zone，重启应用会重新创建
3. 检查网络连接

### 问题：unbound 无法同步

**原因**：网络问题或根服务器不可达

**解决**：
1. 检查网络连接
2. 检查防火墙规则
3. 查看 unbound 日志
4. fallback-enabled 会自动回退到递归查询

### 问题：配置文件错误

**原因**：unbound 版本不支持某些参数

**解决**：
1. 检查 unbound 版本
2. 查看 unbound 错误日志
3. 简化配置，移除不支持的参数

## 未来优化

1. **监控面板**：显示 root.zone 同步状态
2. **统计信息**：记录同步次数和时间
3. **告警机制**：同步失败时发送通知
4. **手动触发**：提供 API 手动触发同步

## 总结

通过让 unbound 自动管理 root.zone 的更新，我们：
- ✅ 消除了定时更新任务
- ✅ 降低了应用复杂度
- ✅ 提高了系统可靠性
- ✅ 改善了性能表现
- ✅ 实现了完全自动化

这是一个优雅的解决方案，充分利用了 unbound 的能力。
