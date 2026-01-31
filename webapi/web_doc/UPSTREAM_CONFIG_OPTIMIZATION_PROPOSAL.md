# 上游服务器配置优化方案

## 当前问题分析

### 现状
1. **强制要求上游服务器** - 配置校验要求至少配置一个上游服务器
   ```go
   if len(cfg.Upstream.Servers) == 0 {
       return fmt.Errorf("at least one upstream server is required")
   }
   ```

2. **递归功能的引入** - 现在用户可以启用本地递归解析器（Unbound）
   - 递归解析器可以独立工作，不需要上游服务器
   - 但当前配置逻辑不允许这种场景

3. **用户体验问题**
   - 用户想只用递归解析器时，仍被迫填写上游服务器
   - 配置表单没有清晰的指导说明两者的关系

## 优化方案

### 方案 A：灵活的配置模式（推荐）

#### 1. 配置校验逻辑优化

**修改 `webapi/api_config.go` 中的校验规则**：

```go
// 新的校验逻辑
if len(cfg.Upstream.Servers) == 0 && !cfg.Upstream.EnableRecursor {
    return fmt.Errorf("at least one upstream server or recursor must be configured")
}

// 如果启用了递归，上游服务器可以为空
// 如果启用了上游服务器，递归可以禁用
// 两者都可以同时启用（混合模式）
```

#### 2. 配置表单优化

**改进 `config-upstream.html`**：

```html
<!-- 添加说明面板 -->
<div class="md:col-span-2 p-4 rounded-lg bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800">
    <h4 class="font-semibold mb-2" data-i18n="config.upstream.resolutionMode">Resolution Mode</h4>
    <p class="text-sm mb-3" data-i18n="config.upstream.resolutionModeHelp">
        Choose how DNS queries are resolved:
    </p>
    <ul class="text-sm space-y-2">
        <li>
            <strong data-i18n="config.upstream.modeUpstream">Upstream Only:</strong>
            <span data-i18n="config.upstream.modeUpstreamDesc">Forward queries to configured upstream servers</span>
        </li>
        <li>
            <strong data-i18n="config.upstream.modeRecursor">Recursor Only:</strong>
            <span data-i18n="config.upstream.modeRecursorDesc">Use embedded Unbound for recursive resolution</span>
        </li>
        <li>
            <strong data-i18n="config.upstream.modeHybrid">Hybrid:</strong>
            <span data-i18n="config.upstream.modeHybridDesc">Use recursor as primary, fallback to upstream servers</span>
        </li>
    </ul>
</div>

<!-- 上游服务器字段 - 添加条件提示 -->
<div class="form-group md:col-span-2">
    <label for="upstream.servers" class="block text-sm font-medium mb-2">
        <span data-i18n="config.upstream.servers">Upstream Servers</span>
        <span id="upstream-servers-required" class="text-red-500">*</span>
        <span id="upstream-servers-optional" class="text-gray-500">(Optional)</span>
    </label>
    <textarea id="upstream.servers" name="upstream.servers"
              class="w-full rounded-lg border border-border-light dark:border-border-dark bg-background-light dark:bg-black p-4 font-mono text-sm h-32"
              placeholder="e.g., 8.8.8.8:53&#10;1.1.1.1:53"></textarea>
    <small id="upstream-servers-help" data-i18n="config.upstream.serversHelp">
        One per line. Leave empty if using recursor only.
    </small>
</div>
```

#### 3. JavaScript 动态提示

**在 `config.js` 中添加逻辑**：

```javascript
// 监听递归启用状态变化
document.getElementById('upstream.enable_recursor').addEventListener('change', function() {
    const isRecursorEnabled = this.checked;
    const upstreamServersField = document.getElementById('upstream.servers');
    const requiredLabel = document.getElementById('upstream-servers-required');
    const optionalLabel = document.getElementById('upstream-servers-optional');
    const helpText = document.getElementById('upstream-servers-help');
    
    if (isRecursorEnabled) {
        // 递归启用 - 上游服务器变为可选
        upstreamServersField.classList.remove('border-red-500');
        requiredLabel.style.display = 'none';
        optionalLabel.style.display = 'inline';
        helpText.textContent = i18n.t('config.upstream.serversHelpOptional');
    } else {
        // 递归禁用 - 上游服务器变为必需
        upstreamServersField.classList.add('border-red-500');
        requiredLabel.style.display = 'inline';
        optionalLabel.style.display = 'none';
        helpText.textContent = i18n.t('config.upstream.serversHelpRequired');
    }
});

// 页面加载时初始化
document.addEventListener('DOMContentLoaded', function() {
    const isRecursorEnabled = document.getElementById('upstream.enable_recursor').checked;
    if (isRecursorEnabled) {
        document.getElementById('upstream-servers-required').style.display = 'none';
        document.getElementById('upstream-servers-optional').style.display = 'inline';
    }
});
```

### 方案 B：分离配置（备选）

如果想更清晰地分离两种模式，可以在配置表单中添加单选按钮：

```html
<!-- 分辨率模式选择 -->
<div class="form-group md:col-span-2">
    <label class="block text-sm font-medium mb-3" data-i18n="config.upstream.resolutionMode">
        Resolution Mode
    </label>
    <div class="space-y-2">
        <label class="flex items-center gap-3">
            <input type="radio" name="resolution_mode" value="upstream" 
                   class="h-4 w-4 text-primary">
            <span data-i18n="config.upstream.modeUpstream">Upstream Servers</span>
        </label>
        <label class="flex items-center gap-3">
            <input type="radio" name="resolution_mode" value="recursor" 
                   class="h-4 w-4 text-primary">
            <span data-i18n="config.upstream.modeRecursor">Recursor Only</span>
        </label>
        <label class="flex items-center gap-3">
            <input type="radio" name="resolution_mode" value="hybrid" 
                   class="h-4 w-4 text-primary">
            <span data-i18n="config.upstream.modeHybrid">Hybrid (Recursor + Upstream)</span>
        </label>
    </div>
</div>
```

## 实现步骤

### 第一步：修改后端校验逻辑

**文件：`webapi/api_config.go`**

```go
// 修改第 132-135 行
if len(cfg.Upstream.Servers) == 0 && !cfg.Upstream.EnableRecursor {
    return fmt.Errorf("at least one upstream server or recursor must be configured")
}
```

### 第二步：更新前端表单

**文件：`webapi/web/components/config-upstream.html`**

- 添加说明面板
- 修改上游服务器字段的必需性提示
- 添加占位符文本

### 第三步：添加 JavaScript 逻辑

**文件：`webapi/web/js/modules/config.js`**

- 监听递归启用状态
- 动态更新上游服务器字段的必需性提示
- 实时验证表单

### 第四步：更新国际化文本

**文件：`webapi/web/js/i18n/resources-*.js`**

添加新的翻译键：
```javascript
"config.upstream.resolutionMode": "Resolution Mode",
"config.upstream.resolutionModeHelp": "Choose how DNS queries are resolved",
"config.upstream.modeUpstream": "Upstream Only",
"config.upstream.modeUpstreamDesc": "Forward queries to configured upstream servers",
"config.upstream.modeRecursor": "Recursor Only",
"config.upstream.modeRecursorDesc": "Use embedded Unbound for recursive resolution",
"config.upstream.modeHybrid": "Hybrid",
"config.upstream.modeHybridDesc": "Use recursor as primary, fallback to upstream servers",
"config.upstream.serversHelpOptional": "One per line. Leave empty if using recursor only.",
"config.upstream.serversHelpRequired": "One per line. At least one server is required.",
```

## 优势对比

| 方面 | 当前方案 | 方案 A（推荐） | 方案 B |
|------|--------|-------------|--------|
| 灵活性 | ❌ 低 | ✅ 高 | ✅ 高 |
| 用户体验 | ❌ 差 | ✅ 好 | ✅ 很好 |
| 实现复杂度 | ✅ 简单 | ✅ 简单 | ⚠️ 中等 |
| 向后兼容 | ✅ 是 | ✅ 是 | ⚠️ 需要迁移 |
| 代码改动 | - | 最小 | 中等 |

## 推荐方案

**采用方案 A**，理由：
1. 改动最小，风险最低
2. 用户体验好，有清晰的说明
3. 支持三种使用模式：纯上游、纯递归、混合
4. 前端动态提示，用户友好
5. 向后兼容，现有配置不受影响

## 实现时间估计

- 后端校验修改：5 分钟
- 前端表单更新：15 分钟
- JavaScript 逻辑：10 分钟
- 国际化文本：10 分钟
- 测试验证：15 分钟

**总计：约 1 小时**

## 测试场景

1. ✅ 只配置上游服务器，禁用递归 → 应该保存成功
2. ✅ 只启用递归，不配置上游服务器 → 应该保存成功
3. ✅ 同时启用递归和上游服务器 → 应该保存成功
4. ✅ 既不配置上游也不启用递归 → 应该保存失败，显示错误提示
5. ✅ 前端提示动态变化 → 启用递归时，上游服务器字段提示应变为"可选"

## 后续优化

1. 添加"推荐配置"预设
2. 在仪表板显示当前使用的解析模式
3. 添加性能对比建议
4. 支持故障转移策略配置
