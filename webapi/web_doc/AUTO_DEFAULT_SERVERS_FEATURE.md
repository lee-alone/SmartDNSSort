# 自动填充默认服务器功能

## 功能概述

当用户在以下情况下取消递归功能时，系统会自动填充 Google 和 Cloudflare 的 DoH 服务器，防止因配置错误导致 DNS 无法工作。

**触发条件**：
1. 用户启用了递归解析器
2. 上游服务器配置为空
3. 用户取消勾选递归功能

## 实现细节

### 1. 核心逻辑

**文件：`webapi/web/js/modules/config.js`**

修改了 `updateUpstreamRecursorAlert()` 函数，添加了以下逻辑：

```javascript
function updateUpstreamRecursorAlert() {
    const recursorCheckbox = document.getElementById('upstream.enable_recursor');
    const alertBox = document.getElementById('recursor-status-alert');
    const upstreamServersField = document.getElementById('upstream.servers');
    
    if (!recursorCheckbox || !alertBox) return;
    
    if (recursorCheckbox.checked) {
        // 显示提示
        alertBox.classList.remove('hidden');
    } else {
        // 隐藏提示
        alertBox.classList.add('hidden');
        
        // 当取消递归时，检查是否需要填充默认服务器
        if (upstreamServersField) {
            const currentServers = upstreamServersField.value.trim();
            
            // 如果上游服务器为空，自动填充默认的 DoH 服务器
            if (!currentServers) {
                const defaultServers = [
                    'https://dns.google/dns-query',
                    'https://cloudflare-dns.com/dns-query'
                ].join('\n');
                
                upstreamServersField.value = defaultServers;
                
                // 显示提示信息
                showDefaultServersNotification();
            }
        }
    }
}
```

### 2. 用户通知

新增 `showDefaultServersNotification()` 函数，显示一个绿色的成功通知：

```javascript
function showDefaultServersNotification() {
    // 创建临时通知
    const notification = document.createElement('div');
    notification.className = 'fixed bottom-4 right-4 p-4 rounded-lg bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 shadow-lg z-50 max-w-sm';
    notification.innerHTML = `
        <div class="flex items-start gap-3">
            <svg class="w-5 h-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5">
                <!-- 成功图标 -->
            </svg>
            <div>
                <h4 class="font-semibold text-green-900 dark:text-green-100 mb-1">
                    Default Servers Added
                </h4>
                <p class="text-sm text-green-800 dark:text-green-200">
                    Google and Cloudflare DoH servers have been added to prevent DNS resolution failure.
                </p>
            </div>
        </div>
    `;
    
    document.body.appendChild(notification);
    
    // 3 秒后自动移除
    setTimeout(() => {
        notification.remove();
    }, 3000);
}
```

### 3. 默认服务器

系统自动填充的服务器：

| 服务商 | 地址 | 类型 |
|------|------|------|
| Google | `https://dns.google/dns-query` | DoH |
| Cloudflare | `https://cloudflare-dns.com/dns-query` | DoH |

**选择理由**：
- 都是全球知名的公共 DNS 服务
- 支持 DoH（DNS over HTTPS）
- 高可用性和稳定性
- 无日志政策（Cloudflare）或隐私友好（Google）

## 用户体验流程

### 场景：用户误操作

1. **初始状态**
   - 用户启用递归解析器
   - 上游服务器为空（因为用户想用纯递归）

2. **用户取消递归**
   - 用户勾选「启用嵌入式 Unbound 递归解析器」
   - 然后取消勾选（误操作或改变主意）

3. **系统自动处理**
   - 检测到上游服务器为空
   - 自动填充 Google 和 Cloudflare 的 DoH 服务器
   - 显示绿色通知：「已添加默认服务器」

4. **用户看到**
   ```
   上游服务器：
   https://dns.google/dns-query
   https://cloudflare-dns.com/dns-query
   
   [绿色通知] ✓ 已添加默认服务器
              Google 和 Cloudflare 的 DoH 服务器已自动添加，
              以防止 DNS 解析失败。
   ```

5. **用户可以**
   - 保存配置，DNS 正常工作
   - 修改服务器列表
   - 重新启用递归

## 设计特点

### 1. 智能检测
- 只在上游服务器为空时填充
- 不会覆盖用户已配置的服务器
- 只在取消递归时触发

### 2. 用户友好
- 自动处理，无需用户干预
- 显示清晰的通知
- 用户可以随时修改

### 3. 防错机制
- 防止"既没有上游也没有递归"的错误配置
- 确保 DNS 始终可用
- 提供合理的默认值

### 4. 视觉反馈
- 绿色成功通知
- 3 秒后自动消失
- 支持深色模式

## 国际化支持

添加了两个新的翻译键：

**中文**：
```javascript
"defaultServersAdded": "已添加默认服务器",
"defaultServersAddedDesc": "已自动添加 Google 和 Cloudflare 的 DoH 服务器，以防止 DNS 解析失败。"
```

**英文**：
```javascript
"defaultServersAdded": "Default Servers Added",
"defaultServersAddedDesc": "Google and Cloudflare DoH servers have been added to prevent DNS resolution failure."
```

## 配置示例

### 触发自动填充的场景

**初始配置**：
```yaml
upstream:
  servers: []
  enable_recursor: true
  recursor_port: 5353
```

**用户取消递归后**：
```yaml
upstream:
  servers:
    - https://dns.google/dns-query
    - https://cloudflare-dns.com/dns-query
  enable_recursor: false
  recursor_port: 5353
```

### 不触发自动填充的场景

**已有上游服务器**：
```yaml
upstream:
  servers:
    - 8.8.8.8:53
  enable_recursor: true
```
→ 取消递归时，不填充默认服务器（因为已有配置）

**直接禁用递归**：
```yaml
upstream:
  servers: []
  enable_recursor: false
```
→ 页面加载时不填充（只在用户操作时填充）

## 技术实现细节

### 触发时机
- 事件：`change` 事件（递归复选框）
- 条件：`recursorCheckbox.checked === false`
- 检查：`upstreamServersField.value.trim() === ''`

### 通知样式
- 位置：右下角固定位置
- 颜色：绿色（成功）
- 持续时间：3 秒
- 支持深色模式

### 代码流程
```
用户取消递归
    ↓
触发 change 事件
    ↓
updateUpstreamRecursorAlert() 执行
    ↓
检查递归是否被禁用
    ↓
检查上游服务器是否为空
    ↓
自动填充默认服务器
    ↓
显示成功通知
    ↓
3 秒后通知消失
```

## 测试场景

- [ ] 启用递归，上游为空，然后取消递归 → 自动填充
- [ ] 启用递归，上游有配置，然后取消递归 → 不填充
- [ ] 直接禁用递归（页面加载时） → 不填充
- [ ] 填充后，用户修改服务器 → 保存用户修改
- [ ] 切换语言 → 通知文本正确更新
- [ ] 深色模式 → 通知样式正确
- [ ] 多次切换递归 → 逻辑正确

## 后续优化建议

1. **可配置的默认服务器**
   - 允许管理员自定义默认服务器列表
   - 支持多个预设方案

2. **更智能的检测**
   - 检测用户是否真的想用纯递归
   - 显示确认对话框

3. **性能优化**
   - 测试默认服务器的可用性
   - 选择最快的服务器

4. **用户教育**
   - 添加帮助文本说明为什么需要上游服务器
   - 提供配置建议

## 相关文件修改

1. `webapi/web/js/modules/config.js` - 核心逻辑
2. `webapi/web/js/i18n/resources-zh-cn.js` - 中文翻译
3. `webapi/web/js/i18n/resources-en.js` - 英文翻译

## 常见问题

**Q: 为什么选择 DoH 而不是普通 DNS？**
A: DoH 更安全，支持加密传输，且这两个服务商都提供了 DoH 端点。

**Q: 用户可以修改自动填充的服务器吗？**
A: 可以，这只是初始值，用户可以随时修改或删除。

**Q: 如果用户想要纯递归怎么办？**
A: 用户可以在取消递归后立即清空上游服务器字段，或者重新启用递归。

**Q: 通知可以关闭吗？**
A: 通知会在 3 秒后自动消失，用户无需手动关闭。

**Q: 这个功能会影响性能吗？**
A: 不会，只是在用户操作时执行简单的字符串操作。
