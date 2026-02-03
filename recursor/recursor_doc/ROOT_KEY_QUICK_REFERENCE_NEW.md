# Root.Key 管理 - 快速参考

## 核心变更

| 方面 | 旧方案 | 新方案 |
|------|--------|--------|
| 更新方式 | 应用定期更新 | Unbound 自动管理 |
| 更新频率 | 每 30 天 | 由 unbound 决定 |
| 定时任务 | ✅ 需要 | ❌ 不需要 |
| 代码复杂度 | 高 | 低 |
| 资源占用 | 较高 | 无 |

## 启动流程

```
应用启动
  ↓
确保 root.key 存在
  ├─ 存在 → 使用现有文件
  └─ 不存在 → 从嵌入数据解压
  ↓
启动 unbound
  ↓
unbound 自动监控 root.key
  ↓
完成
```

## 配置

```unbound
# Unbound 会自动管理 root.key 的更新
auto-trust-anchor-file: "/etc/unbound/root.key"
```

## 代码变更

### 移除的方法

- `updateRootKeyInBackground()` - 定期更新任务
- `tryUpdateRootKey()` - 尝试更新

### 保留的方法

- `ensureRootKey()` - 启动时确保文件存在

## 验证

### 检查文件

```bash
# Linux
ls -la /etc/unbound/root.key

# Windows
dir unbound\root.key
```

### 检查 unbound 配置

```bash
grep "auto-trust-anchor-file" /etc/unbound/unbound.conf.d/smartdnssort.conf
```

### 查看日志

```bash
# 应该看到
[Recursor] Root key ready
```

## 常见操作

### 手动更新 root.key

```bash
# Linux
unbound-anchor -a /etc/unbound/root.key

# 或删除文件，重启应用
rm /etc/unbound/root.key
systemctl restart smartdnssort
```

### 验证 root.key 有效性

```bash
# 检查文件大小（应该 > 1KB）
ls -lh /etc/unbound/root.key

# 检查文件内容
head -5 /etc/unbound/root.key
```

## 优势

✅ 代码更简洁
✅ 资源占用更低
✅ 完全自动化
✅ 依赖成熟机制

## 故障排查

| 问题 | 解决方案 |
|------|---------|
| 文件不存在 | 重启应用，自动生成 |
| 文件损坏 | 删除文件，重启应用 |
| unbound 无法读取 | 检查权限和路径 |

## 总结

**新方案**：启动时确保文件存在，之后完全由 unbound 管理。

**优势**：简洁、高效、可靠、自动化。
