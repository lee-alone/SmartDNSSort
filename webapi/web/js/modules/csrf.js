/**
 * CSRF Protection Module
 * Handles CSRF token management for secure API requests
 */

const CSRFManager = (function() {
    // 私有变量
    let csrfToken = null;
    let tokenExpiry = null;
    let refreshTimer = null;
    
    // 配置
    const CONFIG = {
        tokenEndpoint: '/api/csrf-token',
        refreshBeforeExpiryMs: 5 * 60 * 1000, // 在过期前5分钟刷新
        maxRetries: 3,
        retryDelayMs: 1000,
    };

    /**
     * 从服务器获取新的 CSRF 令牌
     * @param {number} retryCount - 重试次数
     * @returns {Promise<string>} CSRF 令牌
     */
    async function fetchToken(retryCount = 0) {
        try {
            const response = await fetch(CONFIG.tokenEndpoint, {
                method: 'GET',
                credentials: 'same-origin',
            });

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }

            const data = await response.json();
            
            if (!data.success || !data.data || !data.data.csrf_token) {
                throw new Error('Invalid CSRF token response');
            }

            csrfToken = data.data.csrf_token;
            
            // 解析过期时间
            if (data.data.expires_in) {
                const expiresInSeconds = parseExpiryTime(data.data.expires_in);
                tokenExpiry = Date.now() + expiresInSeconds * 1000;
                
                // 设置自动刷新定时器
                scheduleTokenRefresh(expiresInSeconds);
            }

            console.log('[CSRF] Token obtained successfully');
            return csrfToken;
        } catch (error) {
            console.error('[CSRF] Failed to fetch token:', error);
            
            if (retryCount < CONFIG.maxRetries) {
                await new Promise(resolve => setTimeout(resolve, CONFIG.retryDelayMs));
                return fetchToken(retryCount + 1);
            }
            
            throw error;
        }
    }

    /**
     * 解析过期时间字符串
     * @param {string} expiresStr - 过期时间字符串（如 "2h0m0s"）
     * @returns {number} 过期秒数
     */
    function parseExpiryTime(expiresStr) {
        // 解析格式如 "2h0m0s"
        const hours = expiresStr.match(/(\d+)h/);
        const minutes = expiresStr.match(/(\d+)m/);
        const seconds = expiresStr.match(/(\d+)s/);

        let totalSeconds = 0;
        if (hours) totalSeconds += parseInt(hours[1]) * 3600;
        if (minutes) totalSeconds += parseInt(minutes[1]) * 60;
        if (seconds) totalSeconds += parseInt(seconds[1]);

        return totalSeconds || 7200; // 默认2小时
    }

    /**
     * 安排令牌刷新
     * @param {number} expiresInSeconds - 过期秒数
     */
    function scheduleTokenRefresh(expiresInSeconds) {
        // 清除之前的定时器
        if (refreshTimer) {
            clearTimeout(refreshTimer);
        }

        // 在过期前刷新令牌
        const refreshTime = (expiresInSeconds * 1000) - CONFIG.refreshBeforeExpiryMs;
        
        if (refreshTime > 0) {
            refreshTimer = setTimeout(async () => {
                try {
                    await fetchToken();
                } catch (error) {
                    console.error('[CSRF] Auto-refresh failed:', error);
                }
            }, refreshTime);
        }
    }

    /**
     * 获取当前 CSRF 令牌
     * 如果令牌不存在或即将过期，自动获取新令牌
     * @returns {Promise<string>} CSRF 令牌
     */
    async function getToken() {
        // 如果令牌不存在或即将过期，获取新令牌
        if (!csrfToken || !tokenExpiry || Date.now() > tokenExpiry - CONFIG.refreshBeforeExpiryMs) {
            return await fetchToken();
        }
        return csrfToken;
    }

    /**
     * 为请求添加 CSRF 令牌头
     * @param {Object} headers - 请求头对象
     * @returns {Promise<Object>} 添加了 CSRF 令牌的请求头
     */
    async function addCsrfHeader(headers = {}) {
        const token = await getToken();
        return {
            ...headers,
            'X-CSRF-Token': token,
        };
    }

    /**
     * 创建带 CSRF 保护的 fetch 请求
     * @param {string} url - 请求 URL
     * @param {Object} options - fetch 选项
     * @returns {Promise<Response>} fetch 响应
     */
    async function secureFetch(url, options = {}) {
        // GET 请求不需要 CSRF 保护
        const method = (options.method || 'GET').toUpperCase();
        if (method === 'GET' || method === 'HEAD' || method === 'OPTIONS') {
            return fetch(url, options);
        }

        // 添加 CSRF 令牌
        const headers = await addCsrfHeader(options.headers || {});
        
        const response = await fetch(url, {
            ...options,
            headers,
            credentials: 'same-origin',
        });

        // POST/PUT/DELETE 请求成功后主动刷新令牌（增强安全性）
        if (response.ok && (method === 'POST' || method === 'PUT' || method === 'DELETE')) {
            // 异步刷新令牌，不阻塞响应
            fetchToken().catch(error => {
                console.warn('[CSRF] Failed to refresh token after request:', error);
            });
        }

        return response;
    }

    /**
     * 处理 CSRF 错误响应
     * 如果是 CSRF 错误，尝试刷新令牌并重试
     * @param {Response} response - fetch 响应
     * @param {string} url - 请求 URL
     * @param {Object} options - fetch 选项
     * @returns {Promise<Response>} 重试后的响应或原始响应
     */
    async function handleCsrfError(response, url, options) {
        if (response.status === 403) {
            // 尝试解析错误信息
            try {
                const data = await response.clone().json();
                if (data.message && data.message.includes('CSRF')) {
                    console.log('[CSRF] Token invalid, refreshing and retrying...');
                    
                    // 强制刷新令牌
                    csrfToken = null;
                    tokenExpiry = null;
                    
                    // 重试请求
                    return secureFetch(url, options);
                }
            } catch (e) {
                // 忽略解析错误
            }
        }
        return response;
    }

    /**
     * 清理 CSRF 管理器
     */
    function cleanup() {
        if (refreshTimer) {
            clearTimeout(refreshTimer);
            refreshTimer = null;
        }
        csrfToken = null;
        tokenExpiry = null;
    }

    // 公开 API
    return {
        getToken,
        addCsrfHeader,
        secureFetch,
        handleCsrfError,
        cleanup,
        fetchToken,
    };
})();

// 导出模块
if (typeof module !== 'undefined' && module.exports) {
    module.exports = CSRFManager;
}
