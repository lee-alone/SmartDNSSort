# Unbound Web 配置编辑 - 快速参考

## 功能说明

在 Web 界面的「自定义设置」标签页中添加了 Unbound 配置文件编辑器，允许用户直接编辑和保存 Unbound 配置，并自动重启进程。

## 用户界面

### 编辑器位置
- 标签页：「自定义设置」
- 位置：在「拦截域名」和「自定义回复规则」下方
- 标题：「Unbound Configuration」

### 编辑器功能
- 📝 **编辑框** - 编辑 Unbound 配置文件
- 📊 **统计** - 显示行数和字符数
- 🔄 **重新加载** - 从文件重新加载配置
- 💾 **保存并重启** - 保存配置并重启 Unbound 进程

## API 端点

### GET /api/unbound/config
读取 Unbound 配置文件

**请求**：
```bash
curl http://localhost:8080/api/unbound/config
```

**响应**：
```json
{
  "content": "# Unbound configuration\nserver:\n    interface: 127.0.0.1@5353\n    ..."
}
```

### POST /api/unbound/config
保存配置文件并重启 Unbound

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

## 工作流程

### 1. 页面加载
```
用户打开「自定义设置」
    ↓
系统检查递归是否启用
    ↓
启用 → 显示编辑器，加载配置
禁用 → 隐藏编辑器
```

### 2. 编辑配置
```
用户在编辑框中修改配置
    ↓
实时显示行数和字符数
    ↓
用户点击「Save & Restart」
```

### 3. 保存并重启
```
发送 POST 请求到 /api/unbound/config
    ↓
系统保存配置文件
    ↓
系统停止当前 Unbound 进程
    ↓
系统启动新的 Unbound 进程
    ↓
显示成功提示
```

## 常见操作

### 修改监听端口
```
找到 interface 行：
interface: 127.0.0.1@5353

修改为：
interface: 127.0.0.1@5354
```

### 增加缓存大小
```
找到 msg-cache-size 和 rrset-cache-size：
msg-cache-size: 200m
rrset-cache-size: 400m

修改为：
msg-cache-size: 500m
rrset-cache-size: 1000m
```

### 增加线程数
```
找到 num-threads：
num-threads: 6

修改为：
num-threads: 8
```

## 实现的文件

| 文件 | 说明 |
|------|------|
| `webapi/api_unbound.go` | 后端 API 处理器 |
| `webapi/api.go` | 路由注册 |
| `webapi/web/components/custom-rules.html` | HTML 组件 |
| `webapi/web/js/modules/custom-settings.js` | JavaScript 逻辑 |

## 核心代码

### 后端 API
```go
// 处理 Unbound 配置文件的读写
func (s *Server) handleUnboundConfig(w http.ResponseWriter, r *http.Request) {
    // 检查递归是否启用
    if !s.cfg.Upstream.EnableRecursor {
        s.writeJSONError(w, "Recursor is not enabled", http.StatusBadRequest)
        return
    }
    
    switch r.Method {
    case http.MethodGet:
        s.handleUnboundConfigGet(w)
    case http.MethodPost:
        s.handleUnboundConfigPost(w, r, mgr)
    }
}
```

### 前端 JavaScript
```javascript
// 加载配置文件
function loadUnboundConfig() {
    fetch('/api/unbound/config')
        .then(response => response.json())
        .then(data => {
            const el = document.getElementById('unbound-config-content');
            if (el) el.value = data.content;
        });
}

// 保存配置并重启
function saveUnboundConfig(button) {
    const content = document.getElementById('unbound-config-content').value;
    
    fetch('/api/unbound/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: content })
    })
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                alert('Unbound config saved and restarted');
            }
        });
}
```

## 错误处理

| 错误 | 原因 | 解决 |
|------|------|------|
| 编辑器隐藏 | 递归未启用 | 启用递归功能 |
| 保存失败 | 文件权限问题 | 检查目录权限 |
| 重启失败 | 配置文件错误 | 检查配置语法 |
| 加载失败 | 文件不存在 | 重新启用递归 |

## 权限检查

✅ 递归必须启用
✅ Recursor Manager 必须初始化
✅ 目录必须可写

## 安全特性

✅ 权限检查
✅ 错误处理
✅ 日志记录
✅ 文件备份（可选）

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

## 相关文档

- 完整文档：`UNBOUND_WEB_CONFIG_FEATURE.md`
- 后端代码：`webapi/api_unbound.go`
- 前端代码：`webapi/web/js/modules/custom-settings.js`
