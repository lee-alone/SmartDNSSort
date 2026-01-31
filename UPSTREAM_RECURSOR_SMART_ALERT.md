# 上游配置智能提示功能

## 功能概述

当用户启用递归解析器时，在上游配置表单中显示一个专业的提示面板，说明当前的配置模式和可选操作。

## 实现细节

### 1. HTML 提示面板

**文件：`webapi/web/components/config-upstream.html`**

添加了一个蓝色的信息提示面板，包含：
- 标题：「递归解析器已启用」
- 描述：说明用户可以进行的操作
- 两个选项：
  1. 将上游服务器留空，使用纯递归解析
  2. 添加上游服务器作为备用

```html
<div id="recursor-status-alert" class="form-group md:col-span-2 hidden p-4 rounded-lg bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800">
    <div class="flex items-start gap-3">
        <svg class="w-5 h-5 text-blue-600 dark:text-blue-400 flex-shrink-0 mt-0.5">
            <!-- 信息图标 -->
        </svg>
        <div>
            <h4 class="font-semibold text-blue-900 dark:text-blue-100 mb-1">
                Recursive Resolver Enabled
            </h4>
            <p class="text-sm text-blue-800 dark:text-blue-200 mb-2">
                Local recursive resolver is active. You can:
            </p>
            <ul class="text-sm text-blue-800 dark:text-blue-200 space-y-1 ml-4">
                <li>• Leave upstream servers empty for pure recursive resolution</li>
                <li>• Add upstream servers as fallback for queries the recursor cannot resolve</li>
            </ul>
        </div>
    </div>
</div>
```

### 2. JavaScript 动态控制

**文件：`webapi/web/js/modules/config.js`**

#### 在 `populateForm` 中添加监听：

```javascript
// 添加递归状态变化监听
const recursorCheckbox = document.getElementById('upstream.enable_recursor');
if (recursorCheckbox) {
    recursorCheckbox.addEventListener('change', updateUpstreamRecursorAlert);
    // 初始化提示
    updateUpstreamRecursorAlert();
}
```

#### 新增 `updateUpstreamRecursorAlert` 函数：

```javascript
/**
 * 更新上游配置中的递归状态提示
 */
function updateUpstreamRecursorAlert() {
    const recursorCheckbox = document.getElementById('upstream.enable_recursor');
    const alertBox = document.getElementById('recursor-status-alert');
    
    if (!recursorCheckbox || !alertBox) return;
    
    if (recursorCheckbox.checked) {
        // 显示提示
        alertBox.classList.remove('hidden');
    } else {
        // 隐藏提示
        alertBox.classList.add('hidden');
    }
}
```

### 3. 国际化支持

**文件：`webapi/web/js/i18n/resources-zh-cn.js` 和 `resources-en.js`**

添加了新的翻译键：

```javascript
"upstream": {
    "recursorEnabled": "递归解析器已启用",
    "recursorEnabledDesc": "本地递归解析器已启用。您可以：",
    "recursorOption1": "将上游服务器留空，使用纯递归解析",
    "recursorOption2": "添加上游服务器作为备用，用于递归解析器无法解决的查询",
    // ... 其他配置
}
```

## 用户体验流程

### 场景 1：禁用递归（默认）
1. 用户打开配置页面
2. 递归解析器复选框未勾选
3. 上游配置中的提示面板隐藏
4. 用户看到正常的上游服务器配置表单

### 场景 2：启用递归
1. 用户勾选「启用嵌入式 Unbound 递归解析器」
2. 上游配置中立即显示蓝色提示面板
3. 提示说明：
   - 递归解析器已启用
   - 可以选择纯递归模式（留空上游服务器）
   - 或者添加上游服务器作为备用
4. 用户可以根据需要配置上游服务器

### 场景 3：禁用递归
1. 用户取消勾选递归解析器
2. 提示面板立即隐藏
3. 表单恢复到正常状态

## 设计特点

### 1. 专业的视觉设计
- 使用蓝色主题（信息提示色）
- 包含信息图标
- 清晰的层级结构
- 支持深色模式

### 2. 实时反应
- 用户勾选/取消递归时立即更新
- 无需刷新页面
- 流畅的用户体验

### 3. 清晰的指导
- 说明当前状态
- 列出可选操作
- 帮助用户做出正确决策

### 4. 多语言支持
- 中文和英文翻译
- 易于扩展其他语言

## 配置示例

### 纯递归模式
```yaml
upstream:
  servers: []  # 空列表
  enable_recursor: true
  recursor_port: 5353
```
**提示显示**：「递归解析器已启用。将上游服务器留空，使用纯递归解析」

### 混合模式（递归 + 备用上游）
```yaml
upstream:
  servers:
    - 8.8.8.8:53
  enable_recursor: true
  recursor_port: 5353
```
**提示显示**：「递归解析器已启用。添加上游服务器作为备用，用于递归解析器无法解决的查询」

### 纯上游模式
```yaml
upstream:
  servers:
    - 8.8.8.8:53
    - 1.1.1.1:53
  enable_recursor: false
```
**提示隐藏**：不显示递归相关提示

## 技术实现细节

### HTML 结构
- 使用 `hidden` 类控制显示/隐藏
- 响应式设计（`md:col-span-2`）
- 支持深色模式（`dark:` 前缀）

### JavaScript 逻辑
- 事件监听：`change` 事件
- DOM 操作：`classList.add/remove`
- 初始化：在 `populateForm` 中调用

### 国际化
- 使用 `data-i18n` 属性
- 支持动态翻译
- 易于维护和扩展

## 测试场景

- [ ] 页面加载时，递归禁用，提示隐藏
- [ ] 勾选递归，提示立即显示
- [ ] 取消勾选递归，提示立即隐藏
- [ ] 切换语言，提示文本正确更新
- [ ] 深色模式下，提示样式正确
- [ ] 保存配置后，提示状态保持正确

## 相关文件修改

1. `webapi/web/components/config-upstream.html` - 添加提示面板
2. `webapi/web/js/modules/config.js` - 添加事件监听和控制逻辑
3. `webapi/web/js/i18n/resources-zh-cn.js` - 中文翻译
4. `webapi/web/js/i18n/resources-en.js` - 英文翻译

## 后续优化建议

1. **添加更多提示**
   - 当上游服务器为空且递归禁用时，显示错误提示
   - 当同时启用递归和上游时，显示混合模式说明

2. **增强用户指导**
   - 添加"推荐配置"按钮
   - 显示当前配置的性能影响

3. **配置预设**
   - 「纯递归」预设
   - 「纯上游」预设
   - 「混合」预设

4. **实时验证**
   - 保存前检查配置有效性
   - 给出清晰的错误提示
