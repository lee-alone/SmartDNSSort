# 上游服务器统计 - 快速调试清单

## 🔍 快速诊断 (5 分钟)

### 1. 检查浏览器控制台 (F12 → Console)

```
✓ 看到 [DEBUG] 日志
✗ 没有看到任何日志 → API 未被调用
```

### 2. 检查 Network 标签

```
✓ /api/upstream-stats 返回 200
✓ Response 中有 data.servers 数组
✗ 返回 500 或其他错误 → 后端问题
✗ 没有这个请求 → 前端未调用
```

### 3. 检查表格显示

```
✓ 表格有 8 列
✓ 数据正确对齐
✗ 数据错位 → 字段缺失
✗ 表格为空 → 数据加载失败
```

---

## 🛠️ 常见问题速查

| 症状 | 可能原因 | 快速修复 |
|------|--------|--------|
| 表格为空 | 没有配置上游服务器 | 检查配置文件 |
| 数据错位 | 字段缺失 | 查看 API 响应 |
| 间歇性出现 | 竞态条件 | 增加刷新间隔 |
| 某服务器无数据 | 地址格式不匹配 | 查看后端日志 |
| 显示错误信息 | i18n 未初始化 | 等待页面加载完成 |

---

## 📊 数据验证

在浏览器控制台执行：

```javascript
// 1. 检查 API 响应
fetch('/api/upstream-stats').then(r => r.json()).then(d => {
    console.log('Servers:', d.data.servers.length);
    d.data.servers.forEach(s => {
        console.log(`${s.address}: ${s.success}/${s.failure} (${s.success_rate.toFixed(1)}%)`);
    });
});

// 2. 检查表格 DOM
const tbody = document.getElementById('upstream_stats').getElementsByTagName('tbody')[0];
console.log('Rows:', tbody.rows.length);
```

---

## 🔧 快速修复

### 修复 1: 清除缓存并刷新

```
Ctrl+Shift+Delete (清除浏览器缓存)
Ctrl+F5 (硬刷新)
```

### 修复 2: 检查服务器日志

```bash
# 查看最近的日志
tail -f /var/log/smartdnssort.log | grep "upstream\|DEBUG"
```

### 修复 3: 重启服务

```bash
systemctl restart smartdnssort
```

---

## 📝 收集诊断信息

如果问题仍未解决，收集以下信息：

```javascript
// 在浏览器控制台执行
console.log('=== 诊断信息 ===');
console.log('URL:', window.location.href);
console.log('i18n 状态:', window.i18n ? '已初始化' : '未初始化');

fetch('/api/upstream-stats')
    .then(r => r.json())
    .then(d => {
        console.log('API 响应:', JSON.stringify(d, null, 2));
    })
    .catch(e => console.error('API 错误:', e));
```

复制所有输出并提交问题报告。

---

## ✅ 验证修复

修复后，检查以下项目：

- [ ] 表格显示 8 列
- [ ] 所有数据正确对齐
- [ ] 成功率显示百分比
- [ ] 健康状态显示图标
- [ ] 延迟显示毫秒
- [ ] 刷新时数据更新
- [ ] 没有控制台错误

---

**如果以上都检查过仍有问题，请参考完整的诊断指南: UPSTREAM_STATS_TROUBLESHOOTING.md**
