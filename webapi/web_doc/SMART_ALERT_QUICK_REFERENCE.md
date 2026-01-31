# 智能提示功能 - 快速参考

## 功能说明

当用户启用递归解析器时，上游配置表单中会显示一个蓝色提示面板，说明当前配置模式和可选操作。

## 用户看到的效果

### 递归禁用时（默认）
```
[上游配置表单]
- 上游服务器输入框
- 其他配置选项
（没有提示面板）
```

### 递归启用时
```
[蓝色提示面板]
ℹ️ 递归解析器已启用
   本地递归解析器已启用。您可以：
   • 将上游服务器留空，使用纯递归解析
   • 添加上游服务器作为备用，用于递归解析器无法解决的查询

[上游配置表单]
- 上游服务器输入框
- 其他配置选项
```

## 实现的文件修改

### 1. HTML 表单（`config-upstream.html`）
- 添加了 `id="recursor-status-alert"` 的提示面板
- 默认隐藏（`hidden` 类）
- 包含信息图标和清晰的说明文本

### 2. JavaScript 逻辑（`config.js`）
- 在 `populateForm` 中添加事件监听
- 新增 `updateUpstreamRecursorAlert()` 函数
- 监听递归复选框的 `change` 事件

### 3. 国际化文本
- 中文：`resources-zh-cn.js`
- 英文：`resources-en.js`
- 添加了 4 个新的翻译键

## 核心代码

### HTML
```html
<div id="recursor-status-alert" class="form-group md:col-span-2 hidden p-4 rounded-lg bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800">
    <!-- 提示内容 -->
</div>
```

### JavaScript
```javascript
// 在 populateForm 中
const recursorCheckbox = document.getElementById('upstream.enable_recursor');
if (recursorCheckbox) {
    recursorCheckbox.addEventListener('change', updateUpstreamRecursorAlert);
    updateUpstreamRecursorAlert();
}

// 新增函数
function updateUpstreamRecursorAlert() {
    const recursorCheckbox = document.getElementById('upstream.enable_recursor');
    const alertBox = document.getElementById('recursor-status-alert');
    
    if (!recursorCheckbox || !alertBox) return;
    
    if (recursorCheckbox.checked) {
        alertBox.classList.remove('hidden');
    } else {
        alertBox.classList.add('hidden');
    }
}
```

## 用户场景

| 场景 | 递归状态 | 上游服务器 | 提示显示 | 说明 |
|------|--------|---------|--------|------|
| 纯上游 | ❌ 禁用 | ✅ 已配置 | ❌ 隐藏 | 使用上游服务器解析 |
| 纯递归 | ✅ 启用 | ❌ 空 | ✅ 显示 | 使用本地递归解析 |
| 混合 | ✅ 启用 | ✅ 已配置 | ✅ 显示 | 递归为主，上游为备 |
| 错误配置 | ❌ 禁用 | ❌ 空 | ❌ 隐藏 | 需要用户修正 |

## 设计特点

✅ **实时反应** - 用户勾选/取消时立即更新
✅ **专业外观** - 蓝色信息提示风格
✅ **多语言** - 支持中英文
✅ **深色模式** - 完全支持
✅ **响应式** - 适配各种屏幕尺寸
✅ **无刷新** - 流畅的用户体验

## 测试清单

- [ ] 页面加载时，递归禁用，提示隐藏
- [ ] 勾选递归，提示立即显示
- [ ] 取消勾选递归，提示立即隐藏
- [ ] 切换语言，提示文本正确更新
- [ ] 深色模式下，提示样式正确
- [ ] 保存配置后，提示状态保持正确
- [ ] 刷新页面后，提示状态正确

## 相关配置

### 纯递归配置
```yaml
upstream:
  servers: []
  enable_recursor: true
  recursor_port: 5353
```

### 混合配置
```yaml
upstream:
  servers:
    - 8.8.8.8:53
  enable_recursor: true
  recursor_port: 5353
```

### 纯上游配置
```yaml
upstream:
  servers:
    - 8.8.8.8:53
    - 1.1.1.1:53
  enable_recursor: false
```

## 常见问题

**Q: 提示面板什么时候显示？**
A: 当用户勾选「启用嵌入式 Unbound 递归解析器」时显示。

**Q: 提示面板可以关闭吗？**
A: 不能关闭，但当取消勾选递归时会自动隐藏。

**Q: 提示面板会影响配置保存吗？**
A: 不会，它只是一个信息提示，不影响配置逻辑。

**Q: 如何修改提示文本？**
A: 修改 `resources-zh-cn.js` 或 `resources-en.js` 中的翻译键。

## 后续优化

1. 添加错误提示（上游和递归都未配置）
2. 添加配置预设按钮
3. 显示当前配置模式的性能建议
4. 支持更多语言
