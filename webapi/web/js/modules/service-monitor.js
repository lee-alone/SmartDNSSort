/**
 * Service Monitor Module
 * Provides real-time feedback for service restart operations
 */

const ServiceMonitor = (function() {
    // 配置
    const CONFIG = {
        healthEndpoint: '/health',
        pollInterval: 1000,      // 轮询间隔（毫秒）
        maxPollAttempts: 60,     // 最大轮询次数（60秒）
        reconnectDelay: 2000,    // 重连延迟
        maxReconnectAttempts: 5, // 最大重连尝试次数
    };

    // 状态
    let isMonitoring = false;
    let pollTimer = null;
    let pollAttempts = 0;
    let reconnectAttempts = 0;

    // UI 元素
    let overlay = null;
    let progressBar = null;
    let statusText = null;
    let countdownText = null;

    /**
     * 创建监控 UI
     */
    function createMonitorUI() {
        // 移除已存在的 UI
        removeMonitorUI();

        // 创建遮罩层
        overlay = document.createElement('div');
        overlay.id = 'service-monitor-overlay';
        overlay.style.cssText = `
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: rgba(0, 0, 0, 0.7);
            display: flex;
            flex-direction: column;
            justify-content: center;
            align-items: center;
            z-index: 10000;
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
        `;

        // 创建内容容器
        const container = document.createElement('div');
        container.style.cssText = `
            background: white;
            border-radius: 12px;
            padding: 32px 48px;
            text-align: center;
            max-width: 400px;
            box-shadow: 0 4px 24px rgba(0, 0, 0, 0.2);
        `;

        // 创建图标
        const icon = document.createElement('div');
        icon.id = 'monitor-icon';
        icon.style.cssText = `
            font-size: 48px;
            margin-bottom: 16px;
        `;
        icon.innerHTML = '🔄';

        // 创建状态文本
        statusText = document.createElement('div');
        statusText.id = 'monitor-status';
        statusText.style.cssText = `
            font-size: 18px;
            font-weight: 600;
            color: #333;
            margin-bottom: 12px;
        `;
        statusText.textContent = i18n ? i18n.t('serviceMonitor.restarting') : '正在重启服务...';

        // 创建进度条容器
        const progressContainer = document.createElement('div');
        progressContainer.style.cssText = `
            width: 100%;
            height: 8px;
            background: #e5e7eb;
            border-radius: 4px;
            overflow: hidden;
            margin-bottom: 12px;
        `;

        // 创建进度条
        progressBar = document.createElement('div');
        progressBar.id = 'monitor-progress';
        progressBar.style.cssText = `
            width: 0%;
            height: 100%;
            background: linear-gradient(90deg, #3b82f6, #8b5cf6);
            border-radius: 4px;
            transition: width 0.3s ease;
        `;
        progressContainer.appendChild(progressBar);

        // 创建倒计时文本
        countdownText = document.createElement('div');
        countdownText.id = 'monitor-countdown';
        countdownText.style.cssText = `
            font-size: 14px;
            color: #666;
        `;
        countdownText.textContent = i18n ? i18n.t('serviceMonitor.waiting') : '等待服务恢复...';

        // 组装 UI
        container.appendChild(icon);
        container.appendChild(statusText);
        container.appendChild(progressContainer);
        container.appendChild(countdownText);
        overlay.appendChild(container);
        document.body.appendChild(overlay);
    }

    /**
     * 移除监控 UI
     */
    function removeMonitorUI() {
        if (overlay && overlay.parentNode) {
            overlay.parentNode.removeChild(overlay);
        }
        overlay = null;
        progressBar = null;
        statusText = null;
        countdownText = null;
    }

    /**
     * 更新进度
     * @param {number} percent - 进度百分比
     * @param {string} status - 状态文本
     */
    function updateProgress(percent, status) {
        if (progressBar) {
            progressBar.style.width = `${Math.min(100, percent)}%`;
        }
        if (statusText && status) {
            statusText.textContent = status;
        }
    }

    /**
     * 更新倒计时
     * @param {number} remaining - 剩余秒数
     */
    function updateCountdown(remaining) {
        if (countdownText) {
            const message = i18n 
                ? i18n.t('serviceMonitor.countdown', { seconds: remaining })
                : `预计剩余时间: ${remaining} 秒`;
            countdownText.textContent = message;
        }
    }

    /**
     * 检查服务健康状态
     * @returns {Promise<boolean>} 服务是否健康
     */
    async function checkHealth() {
        try {
            const response = await fetch(CONFIG.healthEndpoint, {
                method: 'GET',
                cache: 'no-cache',
            });
            
            if (response.ok) {
                const data = await response.json();
                return data.success === true || data.status === 'healthy';
            }
            return false;
        } catch (error) {
            return false;
        }
    }

    /**
     * 开始监控服务状态
     * @param {Object} options - 配置选项
     * @param {Function} [options.onProgress] - 进度回调
     * @param {Function} [options.onSuccess] - 成功回调
     * @param {Function} [options.onFailure] - 失败回调
     * @param {Function} [options.onStatusChange] - 状态变化回调
     */
    async function startMonitoring(options = {}) {
        if (isMonitoring) {
            return;
        }

        isMonitoring = true;
        pollAttempts = 0;
        reconnectAttempts = 0;

        // 创建 UI
        createMonitorUI();

        // 更新初始状态
        updateProgress(10, i18n ? i18n.t('serviceMonitor.restarting') : '正在重启服务...');

        if (options.onStatusChange) {
            options.onStatusChange('restarting');
        }

        // 开始轮询
        return new Promise((resolve, reject) => {
            const poll = async () => {
                pollAttempts++;

                // 更新进度
                const progress = 10 + (pollAttempts / CONFIG.maxPollAttempts) * 80;
                updateProgress(progress);
                updateCountdown(Math.max(0, CONFIG.maxPollAttempts - pollAttempts));

                if (options.onProgress) {
                    options.onProgress(progress, pollAttempts);
                }

                // 检查是否超时
                if (pollAttempts >= CONFIG.maxPollAttempts) {
                    isMonitoring = false;
                    updateProgress(100, i18n ? i18n.t('serviceMonitor.timeout') : '重启超时');
                    
                    if (options.onStatusChange) {
                        options.onStatusChange('timeout');
                    }
                    
                    setTimeout(() => {
                        removeMonitorUI();
                        if (options.onFailure) {
                            options.onFailure(new Error('Restart timeout'));
                        }
                        reject(new Error('Restart timeout'));
                    }, 2000);
                    return;
                }

                // 检查健康状态
                const isHealthy = await checkHealth();

                if (isHealthy) {
                    // 服务已恢复
                    isMonitoring = false;
                    updateProgress(100, i18n ? i18n.t('serviceMonitor.success') : '服务已恢复');
                    
                    if (countdownText) {
                        countdownText.textContent = i18n ? i18n.t('serviceMonitor.refreshing') : '正在刷新页面...';
                    }

                    if (options.onStatusChange) {
                        options.onStatusChange('healthy');
                    }

                    setTimeout(() => {
                        removeMonitorUI();
                        if (options.onSuccess) {
                            options.onSuccess();
                        }
                        resolve();
                    }, 1000);
                    return;
                }

                // 继续轮询
                pollTimer = setTimeout(poll, CONFIG.pollInterval);
            };

            // 延迟开始轮询，给服务一些时间关闭
            setTimeout(poll, 2000);
        });
    }

    /**
     * 停止监控
     */
    function stopMonitoring() {
        isMonitoring = false;
        if (pollTimer) {
            clearTimeout(pollTimer);
            pollTimer = null;
        }
        removeMonitorUI();
    }

    /**
     * 执行重启并监控
     * @param {Object} options - 配置选项
     * @param {string} [options.restartEndpoint] - 重启端点
     * @param {Function} [options.onBeforeRestart] - 重启前回调
     * @param {Function} [options.onProgress] - 进度回调
     * @param {Function} [options.onSuccess] - 成功回调
     * @param {Function} [options.onFailure] - 失败回调
     * @returns {Promise<void>}
     */
    async function restartWithMonitor(options = {}) {
        const restartEndpoint = options.restartEndpoint || '/api/restart';

        try {
            // 执行重启前回调
            if (options.onBeforeRestart) {
                await options.onBeforeRestart();
            }

            // 发送重启请求
            const response = await CSRFManager.secureFetch(restartEndpoint, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                },
            });

            if (!response.ok) {
                const errorData = await response.json().catch(() => ({}));
                throw new Error(errorData.message || `Restart failed: ${response.status}`);
            }

            // 开始监控
            await startMonitoring(options);

        } catch (error) {
            if (options.onFailure) {
                options.onFailure(error);
            }
            
            throw error;
        }
    }

    // 公开 API
    return {
        startMonitoring,
        stopMonitoring,
        restartWithMonitor,
        checkHealth,
        isMonitoring: () => isMonitoring,
    };
})();

// 导出模块
if (typeof module !== 'undefined' && module.exports) {
    module.exports = ServiceMonitor;
}
