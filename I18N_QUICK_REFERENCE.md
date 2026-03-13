# 国际化 (i18n) 快速参考指南

## 问题症状
- 动态加载的组件（如 IP Pool Monitor）在中文环境下仍显示英文
- 页面刷新后翻译恢复正常
- 语言切换时翻译不更新

## 根本原因
通过 `innerHTML` 插入的 HTML 内容不会自动执行翻译逻辑，需要手动触发。

## 解决方案总结

### 1. 确保翻译 Key 存在
```javascript
// 在 resources-zh-cn.js 和 resources-en.js 中定义
"dashboard": {
    "ipPoolMonitor": "IP 池监控",
    // ... 其他 Key
}
```

### 2. 在组件加载后触发翻译
```javascript
// 方式 1: 直接调用
if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
    window.i18n.applyTranslations();
}

// 方式 2: 在事件监听器中
document.addEventListener('componentsLoaded', () => {
    window.i18n.applyTranslations();
});
```

### 3. 在语言切换时重新应用翻译
```javascript
window.addEventListener('languageChanged', () => {
    window.i18n.applyTranslations();
    // 其他更新逻辑
});
```

## 最佳实践

### ✅ 推荐做法
1. **分离关注点**：将动态组件的逻辑放在独立的 JS 模块中
2. **显式触发**：在组件加载和语言切换时显式调用翻译
3. **使用 data-i18n 属性**：在 HTML 中使用标准属性
4. **检查 i18n 可用性**：始终检查 `window.i18n` 是否存在

### ❌ 避免做法
1. **内联脚本**：避免在 HTML 中使用 `<script>` 标签
2. **硬编码文本**：不要在 JS 中硬编码翻译文本
3. **忘记触发翻译**：不要假设 innerHTML 会自动翻译
4. **不处理语言切换**：必须在 `languageChanged` 事件中更新翻译

## 调试技巧

### 检查翻译是否加载
```javascript
console.log(window.i18n.translations);
console.log(window.i18n.t('dashboard.ipPoolMonitor'));
```

### 手动触发翻译
```javascript
window.i18n.applyTranslations();
```

### 检查 data-i18n 属性
```javascript
document.querySelectorAll('[data-i18n]').forEach(el => {
    console.log(el.getAttribute('data-i18n'), el.textContent);
});
```

## 常见问题

### Q: 为什么翻译在页面刷新后才显示？
A: 因为 innerHTML 插入的内容不会自动触发翻译。需要在插入后手动调用 `i18n.applyTranslations()`。

### Q: 如何在动态生成的 HTML 中使用翻译？
A: 使用 `data-i18n` 属性，然后在生成后调用 `i18n.applyTranslations()`。

### Q: 语言切换时翻译不更新怎么办？
A: 在 `languageChanged` 事件监听器中调用 `i18n.applyTranslations()`。

### Q: 如何添加新的翻译 Key？
A: 在 `resources-zh-cn.js` 和 `resources-en.js` 中的相应对象下添加新的 Key-Value 对。

## 相关文件

- `webapi/web/js/i18n/core.js` - i18n 核心引擎
- `webapi/web/js/i18n/resources-zh-cn.js` - 中文翻译
- `webapi/web/js/i18n/resources-en.js` - 英文翻译
- `webapi/web/js/modules/dashboard.js` - 仪表板模块
- `webapi/web/js/modules/ip-pool.js` - IP 池监控模块
