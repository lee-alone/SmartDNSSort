# 自动填充默认服务器 - 快速参考

## 功能说明

当用户在以下情况下取消递归功能时，系统会自动填充 Google 和 Cloudflare 的 DoH 服务器：
- 用户启用了递归解析器
- 上游服务器配置为空
- 用户取消勾选递归功能

## 用户看到的效果

### 操作步骤
1. 启用递归解析器 ✓
2. 上游服务器留空（用纯递归）
3. 取消勾选递归功能 ✗

### 系统自动处理
```
上游服务器字段自动填充：
https://dns.google/dns-query
https://cloudflare-dns.com/dns-query

[绿色通知] ✓ 已添加默认服务器
           Google 和 Cloudflare 的 DoH 服务器已自动添加，
           以防止 DNS 解析失败。
```

## 核心代码

### JavaScript 逻辑
```javascript
// 在 updateUpstreamRecursorAlert() 中
if (!recursorCheckbox.checked) {
    // 隐藏提示
    alertBox.classList.add('hidden');
    
    // 检查是否需要填充默认服务器
    if (upstreamServersField) {
        const currentServers = upstreamServersField.value.trim();
        
        if (!currentServers) {
            // 自动填充
            const defaultServers = [
                'https://dns.google/dns-query',
                'https://cloudflare-dns.com/dns-query'
            ].join('\n');
            
            upstreamServersField.value = defaultServers;
            showDefaultServersNotification();
        }
    }
}
```

## 默认服务器

| 服务商 | 地址 | 类型 |
|------|------|------|
| Google | `https://dns.google/dns-query` | DoH |
| Cloudflare | `https://cloudflare-dns.com/dns-query` | DoH |

## 触发条件

| 条件 | 是否填充 | 说明 |
|------|--------|------|
| 递归启用 → 禁用，上游为空 | ✅ 是 | 自动填充默认服务器 |
| 递归启用 → 禁用，上游有值 | ❌ 否 | 保留用户配置 |
| 页面加载时递归禁用 | ❌ 否 | 只在用户操作时填充 |
| 用户手动清空上游 | ❌ 否 | 不自动填充 |

## 用户通知

### 通知样式
- **颜色**：绿色（成功）
- **位置**：右下角固定
- **持续时间**：3 秒自动消失
- **支持**：深色模式

### 通知内容
```
✓ 已添加默认服务器
  Google 和 Cloudflare 的 DoH 服务器已自动添加，
  以防止 DNS 解析失败。
```

## 实现的文件修改

### 1. JavaScript (`config.js`)
- 修改 `updateUpstreamRecursorAlert()` 函数
- 新增 `showDefaultServersNotification()` 函数
- 添加自动填充逻辑

### 2. 国际化
- `resources-zh-cn.js` - 中文翻译
- `resources-en.js` - 英文翻译
- 2 个新的翻译键

## 配置示例

### 自动填充前
```yaml
upstream:
  servers: []
  enable_recursor: true
```

### 自动填充后
```yaml
upstream:
  servers:
    - https://dns.google/dns-query
    - https://cloudflare-dns.com/dns-query
  enable_recursor: false
```

## 防错机制

✅ **防止无法访问** - 确保 DNS 始终可用
✅ **智能检测** - 只在必要时填充
✅ **用户友好** - 自动处理，无需干预
✅ **可修改** - 用户可以随时修改服务器
✅ **视觉反馈** - 清晰的成功通知

## 测试清单

- [ ] 启用递归 → 禁用递归 → 自动填充
- [ ] 启用递归 → 添加上游 → 禁用递归 → 不填充
- [ ] 页面加载时递归禁用 → 不填充
- [ ] 填充后修改服务器 → 保存修改
- [ ] 切换语言 → 通知文本正确
- [ ] 深色模式 → 通知样式正确
- [ ] 多次切换 → 逻辑正确

## 常见问题

**Q: 为什么是 DoH？**
A: DoH 更安全，支持加密传输。

**Q: 用户可以修改吗？**
A: 可以，这只是初始值。

**Q: 如果用户想要纯递归？**
A: 用户可以重新启用递归，或取消递归后立即清空服务器。

**Q: 通知可以关闭吗？**
A: 3 秒后自动消失。

## 相关文件

- 完整文档：`AUTO_DEFAULT_SERVERS_FEATURE.md`
- 配置文件：`webapi/web/js/modules/config.js`
- 翻译文件：`resources-zh-cn.js`, `resources-en.js`
