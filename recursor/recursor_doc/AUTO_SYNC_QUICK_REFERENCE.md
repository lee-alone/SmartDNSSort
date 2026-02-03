# Root.Zone 自动同步 - 快速参考

## 核心改变

| 方面 | 旧方案 | 新方案 |
|------|--------|--------|
| 更新方式 | 应用定期下载 | Unbound 自动同步 |
| 更新频率 | 每 7 天 | 由 unbound 决定 |
| 定时任务 | ✅ 需要 | ❌ 不需要 |
| 网络故障 | ❌ 无法更新 | ✅ 自动回退 |
| 资源占用 | 较高 | 较低 |

## 配置要点

```unbound
auth-zone:
    name: "."                           # 根域
    zonefile: "unbound/root.zone"       # 本地文件
    primary: 192.0.32.132               # 根服务器 (IPv4)
    primary: 192.0.47.132               # 根服务器 (IPv4)
    primary: 2001:500:12::d0d           # 根服务器 (IPv6)
    primary: 2001:500:1::53             # 根服务器 (IPv6)
    fallback-enabled: yes               # 网络故障回退
    for-upstream: yes                   # 递归查询使用
    for-downstream: no                  # 不向外部暴露
```

## 代码变更位置

### 1. manager_rootzone.go
- `GetRootZoneConfig()` - 生成 auth-zone 配置
- `UpdateRootZonePeriodically()` - 已弃用

### 2. manager.go
- `Start()` - 移除定期更新任务启动
- `Stop()` - 保留兼容性代码

## 验证方法

### 1. 检查日志
```
[Recursor] Ensuring root.zone file...
[Recursor] Using existing root.zone file: unbound/root.zone
[Recursor] Unbound will automatically sync root.zone from root servers
```

### 2. 查看配置
```bash
grep -A 15 "auth-zone" unbound/unbound.conf
```

### 3. 测试查询
```bash
dig @127.0.0.1 -p 5353 example.com
```

## 常见问题

**Q: 如何手动更新 root.zone？**
A: 删除文件，重启应用会重新创建。或让 unbound 自动同步。

**Q: 网络断开会怎样？**
A: `fallback-enabled: yes` 会自动回退到普通递归查询。

**Q: 如何监控同步状态？**
A: 查看 unbound 日志或检查文件修改时间。

**Q: 是否需要定时任务？**
A: 不需要，unbound 完全自动管理。

## 性能提升

- 启动时间：更快（无需下载）
- 内存占用：更低（无定时任务）
- CPU 占用：更低（无定时检查）
- 网络效率：更高（本地缓存）

## 故障排查

| 问题 | 原因 | 解决方案 |
|------|------|---------|
| root.zone 不存在 | 初始化失败 | 检查权限，重启应用 |
| unbound 无法同步 | 网络问题 | 检查网络，查看日志 |
| 配置错误 | 版本不支持 | 检查 unbound 版本 |

## 总结

✅ 完全自动化 - 无需手动干预
✅ 高可靠性 - 网络故障自动回退
✅ 低资源占用 - 无定时任务开销
✅ 简洁优雅 - 充分利用 unbound 能力
