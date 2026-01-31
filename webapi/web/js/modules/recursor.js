// Recursor 管理模块

const RECURSOR_API_URL = '/api/recursor';

/**
 * 获取 Recursor 状态
 */
async function getRecursorStatus() {
    try {
        const response = await fetch(`${RECURSOR_API_URL}/status`);
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}`);
        }
        return await response.json();
    } catch (error) {
        console.error('[Recursor] Failed to get status:', error);
        return null;
    }
}

/**
 * 更新 UI 中的 Recursor 状态显示
 */
async function updateRecursorStatus() {
    const statusIndicator = document.getElementById('recursor-status-indicator');
    const statusText = document.getElementById('recursor-status-text');
    
    if (!statusIndicator || !statusText) {
        return;
    }

    const status = await getRecursorStatus();
    
    if (!status) {
        statusIndicator.className = 'w-3 h-3 rounded-full bg-gray-400';
        statusText.textContent = i18n.t('config.recursor.statusUnknown');
        return;
    }

    // 根据状态更新指示器颜色
    if (status.enabled) {
        statusIndicator.className = 'w-3 h-3 rounded-full bg-green-500';
        const uptime = formatUptime(status.uptime);
        statusText.textContent = i18n.t('config.recursor.statusRunning', {
            port: status.port,
            uptime: uptime
        });
    } else {
        statusIndicator.className = 'w-3 h-3 rounded-full bg-red-500';
        statusText.textContent = i18n.t('config.recursor.statusStopped');
    }
}

/**
 * 格式化运行时间
 */
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

/**
 * 定期更新 Recursor 状态（每 5 秒）
 */
function startRecursorStatusPolling() {
    setInterval(updateRecursorStatus, 5000);
}

// 页面加载时启动轮询
document.addEventListener('DOMContentLoaded', () => {
    startRecursorStatusPolling();
});

// 语言变更时更新状态显示
window.addEventListener('languageChanged', updateRecursorStatus);
