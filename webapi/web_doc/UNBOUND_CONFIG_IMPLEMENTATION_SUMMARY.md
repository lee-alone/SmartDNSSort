# Unbound Web 配置编辑功能 - 实现总结

## 功能概述

在 Web 界面的「自定义设置」标签页中添加了 Unbound 配置文件编辑器，允许用户直接编辑 Unbound 递归解析器的配置文件，并在保存时自动重启 Unbound 进程。

## 修改的文件

### 1. 后端 API

**新增文件**：`webapi/api_unbound.go`

功能：
- 处理 GET 请求读取 Unbound 配置文件
- 处理 POST 请求保存配置文件并重启 Unbound 进程
- 检查递归是否启用
- 错误处理和日志记录

关键函数：
- `handleUnboundConfig()` - 主处理函数
- `handleUnboundConfigGet()` - 读取配置
- `handleUnboundConfigPost()` - 保存配置并重启
- `getUnboundConfigPath()` - 获取配置文件路径

### 2. API 路由注册

**修改文件**：`webapi/api.go`

修改内容：
```go
// 第 101 行添加
mux.HandleFunc("/api/unbound/config", s.handleUnboundConfig)
```

### 3. 前端 HTML

**修改文件**：`webapi/web/components/custom-rules.html`

添加内容：
- Unbound 配置编辑器部分（第 80-120 行）
- 编辑框、统计信息、按钮等

### 4. 前端 JavaScript

**修改文件**：`webapi/web/js/modules/custom-settings.js`

新增函数：
- `loadUnboundConfig()` - 加载配置文件
- `reloadUnboundConfig(button)` - 重新加载
- `saveUnboundConfig(button)` - 保存并重启

修改内容：
- 在 `initializeCounters()` 中添加 Unbound 编辑框的计数器
- 在 `loadCustomSettings()` 中调用 `loadUnboundConfig()`

### 5. 应用初始化

**修改文件**：`webapi/web/js/app.js`

修改内容：
```javascript
// 第 23 行添加
loadCustomSettings();
```

## 工作流程

### 页面加载流程

```
1. 用户打开「自定义设置」标签页
   ↓
2. 系统调用 loadCustomSettings()
   ↓
3. loadCustomSettings() 调用 loadUnboundConfig()
   ↓
4. loadUnboundConfig() 发送 GET /api/unbound/config
   ↓
5. 后端检查递归是否启用
   ├─ 启用 → 返回配置内容，前端显示编辑器
   └─ 禁用 → 返回 enabled: false，前端隐藏编辑器
```

### 保存流程

```
1. 用户点击「Save & Restart」按钮
   ↓
2. 前端发送 POST /api/unbound/config
   ↓
3. 后端保存配置文件到 unbound/unbound.conf
   ↓
4. 后端停止当前 Unbound 进程
   ↓
5. 后端启动新的 Unbound 进程
   ↓
6. 前端显示成功提示
```

## API 端点

### GET /api/unbound/config

**请求**：
```bash
curl http://localhost:8080/api/unbound/config
```

**响应（递归启用）**：
```json
{
  "content": "# Unbound configuration\nserver:\n    interface: 127.0.0.1@5353\n    ...",
  "enabled": true
}
```

**响应（递归禁用）**：
```json
{
  "content": "",
  "enabled": false
}
```

### POST /api/unbound/config

**请求**：
```bash
curl -X POST http://localhost:8080/api/unbound/config \
  -H "Content-Type: application/json" \
  -d '{"content": "# New configuration..."}'
```

**响应**：
```json
{
  "success": true,
  "message": "Unbound config saved and process restarted"
}
```

## 配置文件位置

```
<主程序目录>/unbound/unbound.conf
```

## 用户界面

### 位置
- 标签页：「自定义设置」
- 位置：在「拦截域名」和「自定义回复规则」下方

### 组件
- **编辑框** - 编辑 Unbound 配置文件
- **统计信息** - 显示行数和字符数
- **重新加载按钮** - 从文件重新加载配置
- **保存并重启按钮** - 保存配置并重启 Unbound 进程

## 关键特性

✅ **条件显示** - 只在递归启用时显示编辑器
✅ **实时统计** - 显示行数和字符数
✅ **自动重启** - 保存时自动重启 Unbound 进程
✅ **错误处理** - 完善的错误提示
✅ **权限检查** - 检查递归是否启用
✅ **日志记录** - 所有操作都有日志记录
✅ **深色模式** - 完全支持深色模式

## 修复的问题

### 问题 1：编辑器不显示

**原因**：`loadCustomSettings()` 没有在应用初始化时调用

**修复**：在 `webapi/web/js/app.js` 中添加 `loadCustomSettings()` 调用

### 问题 2：递归禁用时出现错误

**原因**：API 返回错误而不是空响应

**修复**：修改 `handleUnboundConfig()` 返回 `enabled: false` 而不是错误

### 问题 3：前端无法正确处理响应

**原因**：前端检查逻辑不正确

**修复**：修改 `loadUnboundConfig()` 检查 `data.enabled` 字段

## 测试清单

- [ ] 递归禁用时，编辑器隐藏
- [ ] 递归启用时，编辑器显示
- [ ] 加载配置文件内容
- [ ] 编辑配置文件
- [ ] 保存配置文件
- [ ] 重启 Unbound 进程
- [ ] 重新加载配置文件
- [ ] 错误处理正确
- [ ] 深色模式显示正确
- [ ] 多语言支持

## 编译和运行

### 编译
```bash
go build -o smartdnssort.exe ./cmd
```

### 运行
```bash
./smartdnssort.exe
```

### 访问 Web 界面
```
http://localhost:8080
```

## 故障排查

如果编辑器不显示，请参考 `UNBOUND_CONFIG_TROUBLESHOOTING.md`

常见问题：
1. 递归功能未启用
2. 页面未正确加载
3. API 端点未正确注册
4. 编译问题

## 相关文档

- `UNBOUND_WEB_CONFIG_FEATURE.md` - 完整功能文档
- `UNBOUND_CONFIG_QUICK_REFERENCE.md` - 快速参考
- `UNBOUND_CONFIG_TROUBLESHOOTING.md` - 故障排查指南

## 总结

通过以上修改，用户现在可以在 Web 界面中直接编辑 Unbound 配置文件，而不需要通过命令行或文件编辑器。编辑器会在递归功能启用时显示，并在保存时自动重启 Unbound 进程。
