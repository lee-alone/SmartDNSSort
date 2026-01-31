# 上线时长显示格式修复

## 问题描述

Web 界面中的系统状态显示上线时长时出现格式错误：
```
系统状态上线时长7m 17.58501899999999sCPU 使用率0.0%...
```

问题表现：
1. 秒数显示为浮点数（17.58501899999999s）而不是整数
2. 当天数和小时数为 0 时，仍然显示 `0d 0h`，导致显示冗长

## 根本原因

两个 JavaScript 文件中的 `formatUptime` 函数实现有问题：

### 1. `webapi/web/js/modules/utils.js`
```javascript
// ❌ 错误 - 总是显示所有单位，秒数未取整
return `${d}d ${h}h ${m}m ${s}s`;
```

### 2. `webapi/web/js/modules/recursor.js`
```javascript
// ❌ 错误 - 秒数未取整，导致浮点数显示
const secs = seconds % 60;  // 这会得到浮点数
```

## 修复方案

### 修改 1：`webapi/web/js/modules/utils.js`

**之前**：
```javascript
function formatUptime(seconds) {
    const d = Math.floor(seconds / (3600 * 24));
    const h = Math.floor((seconds % (3600 * 24)) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = Math.floor(seconds % 60);
    return `${d}d ${h}h ${m}m ${s}s`;
}
```

**之后**：
```javascript
function formatUptime(seconds) {
    if (!seconds || seconds < 0) return '0s';
    
    const d = Math.floor(seconds / (3600 * 24));
    const h = Math.floor((seconds % (3600 * 24)) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = Math.floor(seconds % 60);
    
    const parts = [];
    if (d > 0) parts.push(`${d}d`);
    if (h > 0) parts.push(`${h}h`);
    if (m > 0) parts.push(`${m}m`);
    if (s > 0 || parts.length === 0) parts.push(`${s}s`);
    
    return parts.join(' ');
}
```

### 修改 2：`webapi/web/js/modules/recursor.js`

**之前**：
```javascript
function formatUptime(seconds) {
    if (!seconds) return '0s';
    
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = seconds % 60;  // ❌ 浮点数
    
    if (hours > 0) {
        return `${hours}h ${minutes}m`;
    } else if (minutes > 0) {
        return `${minutes}m ${secs}s`;
    } else {
        return `${secs}s`;
    }
}
```

**之后**：
```javascript
function formatUptime(seconds) {
    if (!seconds || seconds < 0) return '0s';
    
    const d = Math.floor(seconds / (3600 * 24));
    const h = Math.floor((seconds % (3600 * 24)) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = Math.floor(seconds % 60);
    
    const parts = [];
    if (d > 0) parts.push(`${d}d`);
    if (h > 0) parts.push(`${h}h`);
    if (m > 0) parts.push(`${m}m`);
    if (s > 0 || parts.length === 0) parts.push(`${s}s`);
    
    return parts.join(' ');
}
```

## 修复效果

### 修复前
```
7m 17.58501899999999s
0d 0h 7m 17s
```

### 修复后
```
7m 17s
7m 17s
```

## 显示示例

| 秒数 | 修复前 | 修复后 |
|------|--------|--------|
| 17 | 0d 0h 0m 17s | 17s |
| 437 | 0d 0h 7m 17.58501899999999s | 7m 17s |
| 3661 | 0d 1h 1m 1s | 1h 1m 1s |
| 90061 | 1d 1h 1m 1s | 1d 1h 1m 1s |

## 关键改进

1. ✅ **秒数取整** - 使用 `Math.floor()` 确保整数显示
2. ✅ **智能显示** - 只显示非零的时间单位
3. ✅ **一致性** - 两个文件使用相同的格式化逻辑
4. ✅ **边界处理** - 处理 0 秒和负数的情况

## 测试

刷新 Web 界面，检查系统状态中的上线时长显示是否正确。
