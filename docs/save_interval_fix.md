# save_to_disk_interval_minutes 配置项问题修复

## 问题描述
`save_to_disk_interval_minutes` 配置项在 config.yaml 中被设置成了 0（或完全不存在）。

## 根本原因

### 1. 历史遗留问题
- 你的 `config.yaml` 文件是在添加 `save_to_disk_interval_minutes` 字段之前创建的
- 因此配置文件中完全没有这个字段

### 2. omitempty 标签的影响
当通过 Web API 保存配置时：
1. 调用 `config.LoadConfig()` 加载配置
2. 由于 YAML 文件中没有 `save_to_disk_interval_minutes` 字段，Go 将其设置为零值 `0`
3. 虽然 `LoadConfig()` 会设置默认值 60，但在某些情况下（如只修改部分配置）可能不会触发
4. 使用 `yaml.Marshal()` 序列化时，由于有 `omitempty` 标签，值为 0 的字段不会被写入
5. 下次加载时，字段又不存在，形成恶性循环

### 3. 代码中的使用
在 `dnsserver/server.go` 的 `saveCacheRoutine()` 函数中（第 1123 行）：
```go
interval := time.Duration(s.cfg.Cache.SaveToDiskIntervalMinutes) * time.Minute
if interval <= 0 {
    interval = 60 * time.Minute  // 兜底默认值
}
```
虽然有兜底逻辑，但配置文件中应该明确包含这个字段。

## 解决方案

### 1. 移除 omitempty 标签
在 `config/config.go` 中，将：
```go
SaveToDiskIntervalMinutes int `yaml:"save_to_disk_interval_minutes,omitempty" json:"save_to_disk_interval_minutes"`
```
修改为：
```go
SaveToDiskIntervalMinutes int `yaml:"save_to_disk_interval_minutes" json:"save_to_disk_interval_minutes"`
```

**原因**：这样可以确保这个字段总是被写入配置文件，即使值为 0。

### 2. 更新 config.yaml
在 `cache` 部分添加：
```yaml
save_to_disk_interval_minutes: 60
```

## 修复后的效果

✅ `save_to_disk_interval_minutes` 字段会始终出现在配置文件中
✅ 通过 Web API 保存配置时，这个字段不会丢失
✅ 缓存会按照配置的间隔（默认 60 分钟）定期保存到磁盘

## 为什么其他字段没有这个问题？

大多数配置字段都有非零的默认值，例如：
- `listen_port: 53`
- `timeout_ms: 5000`
- `concurrency: 3`

这些字段即使在配置文件中不存在，加载后也会被设置为非零值，因此在序列化时会被写入。

但 `save_to_disk_interval_minutes` 的情况特殊：
- 它是后来添加的字段
- 旧的配置文件中没有这个字段
- 如果有 `omitempty` 标签，零值不会被写入

## 建议

对于**重要的配置字段**，尤其是那些：
1. 控制关键功能的字段
2. 有明确默认值的字段
3. 用户可能需要显式设置为 0 的字段

**不应该使用 `omitempty` 标签**，以确保它们始终出现在配置文件中，提高配置的可见性和可维护性。
