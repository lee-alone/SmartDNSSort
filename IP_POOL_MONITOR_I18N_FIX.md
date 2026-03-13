# IP Pool Monitor 国际化修复总结

## 问题描述
前端 dashboard.ipPoolMonitor 卡片在中文环境下仍显示英文，存在两个核心问题：
1. 缺失的翻译 Key
2. 动态组件翻译未触发

## 修复方案

### 1. 补充缺失的翻译 Key

#### 中文资源文件 (webapi/web/js/i18n/resources-zh-cn.js)
在 `dashboard` 对象下添加以下条目：
```javascript
"ipPoolMonitor": "IP 池监控",
"totalIPs": "总 IP 数",
"totalRefreshes": "总巡检次数",
"totalIpsRefreshed": "累计巡检 IP 数",
"t0PoolSize": "核心池 (T0)",
"t1PoolSize": "活跃池 (T1)",
"t2PoolSize": "淘汰池 (T2)",
"ip": "IP 地址",
"repDomain": "代表域名",
"refCount": "引用计数",
"accessHeat": "访问热度",
"rtt": "延迟 (ms)",
"lastAccess": "最后访问",
"noIpData": "暂无 IP 池统计数据"
```

#### 英文资源文件 (webapi/web/js/i18n/resources-en.js)
在 `dashboard` 对象下添加以下条目：
```javascript
"ipPoolMonitor": "IP Pool Monitor",
"totalIPs": "Total IPs",
"totalRefreshes": "Total Refreshes",
"totalIpsRefreshed": "Total IPs Refreshed",
"t0PoolSize": "T0 Pool (Core)",
"t1PoolSize": "T1 Pool (Active)",
"t2PoolSize": "T2 Pool (Evicted)",
"ip": "IP Address",
"repDomain": "Rep Domain",
"refCount": "Ref Count",
"accessHeat": "Access Heat",
"rtt": "RTT (ms)",
"lastAccess": "Last Access",
"noIpData": "No IP pool data available"
```

### 2. 解决动态组件翻译未触发问题

#### 方案 A: 在 dashboard.js 中触发翻译
修改 `initializeDashboardButtons()` 函数末尾，添加：
```javascript
// 触发动态加载组件的翻译
if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
    window.i18n.applyTranslations();
}
```

修改 `languageChanged` 事件监听器，在语言切换时也触发翻译：
```javascript
window.addEventListener('languageChanged', () => {
    // 应用翻译到所有 DOM 元素
    if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
        window.i18n.applyTranslations();
    }
    updateDashboard();
    if (!window.dashboardInterval) {
        window.dashboardInterval = setInterval(updateDashboard, 5000);
    }
});
```

#### 方案 B: 创建独立的 IP Pool 模块
创建 `webapi/web/js/modules/ip-pool.js`，将 IP 池的数据更新逻辑从 HTML 内联脚本移出：
- 提取 `loadIPPoolData()` 函数
- 创建 `initializeIPPoolMonitor()` 函数
- 在组件加载时触发翻译
- 在语言切换时重新应用翻译

#### 方案 C: 清理 HTML 组件
修改 `webapi/web/components/ip-pool-monitor.html`：
- 移除内联 `<script>` 标签
- 保留所有 `data-i18n` 属性用于翻译

#### 方案 D: 加载新模块
在 `webapi/web/index.html` 中添加脚本加载：
```html
<script src="js/modules/ip-pool.js"></script>
```

## 修改的文件

1. **webapi/web/js/i18n/resources-zh-cn.js** - 添加中文翻译 Key
2. **webapi/web/js/i18n/resources-en.js** - 添加英文翻译 Key
3. **webapi/web/js/modules/dashboard.js** - 添加翻译触发逻辑
4. **webapi/web/js/modules/ip-pool.js** - 新建 IP 池监控模块
5. **webapi/web/components/ip-pool-monitor.html** - 移除内联脚本
6. **webapi/web/index.html** - 添加 ip-pool.js 脚本加载

## 工作原理

1. **初始加载**：当页面加载时，`ip-pool.js` 监听 `componentsLoaded` 事件
2. **翻译应用**：组件加载后，调用 `i18n.applyTranslations()` 将翻译应用到所有 `data-i18n` 属性
3. **数据更新**：`loadIPPoolData()` 获取 API 数据并更新 DOM
4. **语言切换**：当用户切换语言时，`languageChanged` 事件触发，重新应用翻译
5. **自动刷新**：每 30 秒自动刷新 IP 池数据

## 测试步骤

1. 打开浏览器开发者工具
2. 切换到中文语言
3. 验证 IP Pool Monitor 卡片的标题和表头是否显示中文
4. 刷新页面，确认翻译持久化
5. 切换回英文，验证翻译切换正常

## 注意事项

- 确保 i18n 系统已正确初始化
- 所有新增的翻译 Key 必须在两个资源文件中都定义
- `data-i18n` 属性的值必须与资源文件中的 Key 路径完全匹配
- 动态生成的 HTML 内容需要在生成后手动调用 `i18n.applyTranslations()`
