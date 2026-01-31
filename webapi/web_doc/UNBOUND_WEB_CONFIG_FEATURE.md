# Unbound Web 配置编辑功能

## 功能概述

在 Web 界面的「自定义设置」标签页中添加了 Unbound 配置文件编辑器，允许用户直接编辑 Unbound 递归解析器的配置文件，并在保存时自动重启 Unbound 进程。

## 实现内容

### 1. 后端 API (`webapi/api_unbound.go`)

新增 API 端点：`/api/unbound/config`

**GET 请求**：读取 Unbound 配置文件
```bash
curl http://localhost:8080/api/unbound/config
```

**响应**：
```json
{
  "content": "# Unbound configuration\nserver:\n    interface: 127.0.0.1@5353\n    ..."
}
```

**POST 请求**：保存配置文件并重启 Unbound
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

### 2. 前端 HTML (`webapi/web/components/custom-rules.html`)

添加了一个新的配置编辑器部分：

```html
<!-- Unbound 配置编辑器 -->
<section id="unbound-config-section" class="...">
    <div class="px-6 py-5 border-b ...">
        <h3 class="text-lg font-bold" data-i18n="custom.unboundConfig">
            Unbound Configuration
        </h3>
    </div>
    <div class="flex-1 p-6 flex flex-col gap-4">
        <!-- 帮助文本 -->
        <div class="rounded-lg bg-blue-50 dark:bg-blue-900/20 ...">
            <p class="text-xs text-blue-800 dark:text-blue-200" data-i18n="custom.unboundConfigHelp">
                Edit the Unbound recursive resolver configuration...
            </p>
        </div>
        <!-- 配置编辑框 -->
        <textarea id="unbound-config-content" class="..."></textarea>
        <!-- 行数和字符数统计 -->
        <div class="text-xs text-text-sub-light dark:text-text-sub-dark flex gap-4">
            <span id="unbound-line-count">0 lines</span>
            <span id="unbound-char-count">0 characters</span>
        </div>
    </div>
    <div class="px-6 py-4 border-t ... flex justify-end items-center gap-3">
        <!-- 重新加载按钮 -->
        <button onclick="reloadUnboundConfig(this)" class="...">
            <span data-i18n="custom.reload">Reload</span>
        </button>
        <!-- 保存并重启按钮 -->
        <button onclick="saveUnboundConfig(this)" class="...">
            <span data-i18n="custom.saveUnbound">Save & Restart</span>
        </button>
    </div>
</section>
```

### 3. 前端 JavaScript (`webapi/web/js/modules/custom-settings.js`)

新增函数：

**`loadUnboundConfig()`** - 加载 Unbound 配置文件
```javascript
function loadUnboundConfig() {
    fetch('/api/unbound/config')
        .then(response => response.json())
        .then(data => {
            const section = document.getElementById('unbound-config-section');
            const el = document.getElementById('unbound-config-content');
            
            if (data.content !== undefined) {
                // 递归已启用，显示编辑器
                if (section) section.classList.remove('hidden');
                if (el) {
                    el.value = data.content;
                    updateCounter('unbound-config-content', 'unbound-line-count', 'unbound-char-count');
                }
            } else if (data.error) {
                // 递归未启用，隐藏编辑器
                if (section) section.classList.add('hidden');
            }
        });
}
```

**`reloadUnboundConfig(button)`** - 重新加载配置文件
```javascript
function reloadUnboundConfig(button) {
    button.disabled = true;
    loadUnboundConfig();
    setTimeout(() => {
        button.disabled = false;
    }, 500);
}
```

**`saveUnboundConfig(button)`** - 保存配置文件并重启
```javascript
function saveUnboundConfig(button) {
    const el = document.getElementById('unbound-config-content');
    const content = el.value;
    
    fetch('/api/unbound/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: content })
    })
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                addButtonFeedback(button, true);
                alert(i18n.t('messages.unboundConfigSaved'));
            } else {
                addButtonFeedback(button, false);
                alert(i18n.t('messages.unboundConfigSaveError', { error: data.message }));
            }
        });
}
```

### 4. API 路由注册 (`webapi/api.go`)

```go
// Recursor API 路由
mux.HandleFunc("/api/recursor/status", s.handleRecursorStatus)
mux.HandleFunc("/api/unbound/config", s.handleUnboundConfig)
```

## 用户体验流程

### 1. 页面加载
- 用户打开「自定义设置」标签页
- 系统检查递归是否启用
- 如果启用，显示 Unbound 配置编辑器
- 如果禁用，隐藏编辑器

### 2. 编辑配置
- 用户在编辑框中修改配置
- 实时显示行数和字符数
- 支持所有标准的文本编辑操作

### 3. 保存配置
- 用户点击「Save & Restart」按钮
- 系统保存配置文件到 `unbound/unbound.conf`
- 系统停止当前 Unbound 进程
- 系统启动新的 Unbound 进程
- 显示成功提示

### 4. 重新加载
- 用户点击「Reload」按钮
- 系统从文件重新加载配置
- 丢弃用户未保存的修改

## 技术实现细节

### 配置文件路径
```
<主程序目录>/unbound/unbound.conf
```

### 重启流程
1. 调用 `recursorMgr.Stop()` - 停止当前进程
2. 调用 `recursorMgr.Start()` - 启动新进程
3. 新进程会读取更新后的配置文件

### 错误处理
- 文件不存在：返回空内容
- 写入失败：返回错误信息
- 重启失败：返回错误信息，但配置文件已保存

### 权限检查
- 检查递归是否启用
- 检查 Recursor Manager 是否初始化
- 检查目录是否可写

## 配置示例

### 默认配置
```
# SmartDNSSort Embedded Unbound Configuration
# Auto-generated, do not edit manually
# Generated for 6 CPU cores

server:
    # 监听配置
    interface: 127.0.0.1@5353
    do-ip4: yes
    do-ip6: no
    do-udp: yes
    do-tcp: yes
    
    # 访问控制 - 仅本地访问
    access-control: 127.0.0.1 allow
    access-control: ::1 allow
    access-control: 0.0.0.0/0 deny
    access-control: ::/0 deny
    
    # 性能优化 - 根据 CPU 核数动态调整
    num-threads: 6
    msg-cache-size: 200m
    rrset-cache-size: 400m
    ...
```

### 用户修改示例
用户可以修改以下参数：
- `num-threads` - 线程数
- `msg-cache-size` - 消息缓存大小
- `rrset-cache-size` - RRset 缓存大小
- `access-control` - 访问控制规则
- `interface` - 监听地址和端口
- 等等

## 安全考虑

### 1. 权限检查
- 只有在递归启用时才允许编辑
- 检查 Recursor Manager 是否初始化

### 2. 文件权限
- 配置文件权限：`0644`
- 目录权限：`0755`

### 3. 错误恢复
- 如果重启失败，配置文件已保存
- 用户可以手动修复或重新加载

### 4. 日志记录
- 所有操作都有日志记录
- 便于调试和审计

## 国际化支持

需要添加以下翻译键：

**中文**：
```javascript
"custom": {
    "unboundConfig": "Unbound 配置",
    "unboundConfigHelp": "编辑 Unbound 递归解析器配置。保存后更改将立即应用。",
    "reload": "重新加载",
    "saveUnbound": "保存并重启"
}

"messages": {
    "unboundConfigSaved": "Unbound 配置已保存并重启成功",
    "unboundConfigSaveError": "保存 Unbound 配置失败: {error}"
}
```

**英文**：
```javascript
"custom": {
    "unboundConfig": "Unbound Configuration",
    "unboundConfigHelp": "Edit the Unbound recursive resolver configuration. Changes will be applied immediately after saving.",
    "reload": "Reload",
    "saveUnbound": "Save & Restart"
}

"messages": {
    "unboundConfigSaved": "Unbound configuration saved and restarted successfully",
    "unboundConfigSaveError": "Failed to save Unbound configuration: {error}"
}
```

## 测试场景

- [ ] 递归禁用时，编辑器隐藏
- [ ] 递归启用时，编辑器显示
- [ ] 加载配置文件内容
- [ ] 编辑配置文件
- [ ] 保存配置文件
- [ ] 重启 Unbound 进程
- [ ] 重新加载配置文件
- [ ] 错误处理（文件不存在、写入失败等）
- [ ] 深色模式显示正确
- [ ] 多语言支持

## 相关文件修改

1. `webapi/api_unbound.go` - 新增 API 处理器
2. `webapi/api.go` - 注册路由
3. `webapi/web/components/custom-rules.html` - 添加 HTML 组件
4. `webapi/web/js/modules/custom-settings.js` - 添加 JavaScript 逻辑
5. `webapi/web/js/i18n/resources-zh-cn.js` - 中文翻译
6. `webapi/web/js/i18n/resources-en.js` - 英文翻译

## 后续优化建议

1. **配置验证** - 在保存前验证配置文件语法
2. **配置备份** - 保存前备份当前配置
3. **配置预设** - 提供常用配置模板
4. **配置对比** - 显示修改前后的差异
5. **配置历史** - 保存配置修改历史
6. **实时预览** - 显示配置的实时效果
