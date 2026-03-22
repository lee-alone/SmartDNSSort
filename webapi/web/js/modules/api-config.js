/**
 * API Configuration Module
 * Centralized API URL management
 */

const API_CONFIG = {
    // 基础 URL
    baseURL: '/api',

    // API 端点
    endpoints: {
        // 安全相关
        csrfToken: '/api/csrf-token',

        // 统计相关
        stats: '/api/stats',
        upstreamStats: '/api/upstream-stats',
        recentQueries: '/api/recent-queries',
        recentBlocked: '/api/recent-blocked',
        
        // 配置相关
        config: '/api/config',
        configExport: '/api/config/export',
        
        // AdBlock 相关
        adblockToggle: '/api/adblock/toggle',
        adblockSources: '/api/adblock/sources',
        adblockUpdate: '/api/adblock/update',
        adblockTest: '/api/adblock/test',
        adblockBlockMode: '/api/adblock/block-mode',
        
        // IP 池相关
        ipPool: '/api/ip-pool',
        
        // 自定义规则相关
        customRules: '/api/custom-rules',
        
        // Recursor 相关
        recursorConfig: '/api/recursor/config',
        recursorRootzone: '/api/recursor/rootzone',
        
        // Unbound 相关
        unboundConfig: '/api/unbound/config',
        unboundStatus: '/api/unbound/status',
        unboundRestart: '/api/unbound/restart',
    },
    
    /**
     * 获取完整的 API URL
     * @param {string} endpointName - 端点名称
     * @param {object} params - URL 参数
     * @returns {string} 完整的 URL
     */
    getUrl(endpointName, params = {}) {
        const endpoint = this.endpoints[endpointName];
        if (!endpoint) {
            console.warn(`API endpoint "${endpointName}" not found`);
            return '';
        }
        
        let url = endpoint;
        const queryString = Object.entries(params)
            .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
            .join('&');
        
        if (queryString) {
            url += `?${queryString}`;
        }
        
        return url;
    },
    
    /**
     * 获取带时间范围参数的统计 URL
     * @param {string} endpointName - 端点名称
     * @param {number} days - 天数
     * @returns {string} 完整的 URL
     */
    getStatsUrl(endpointName, days = 7) {
        return this.getUrl(endpointName, { days });
    }
};

// 导出（如果支持模块）
if (typeof module !== 'undefined' && module.exports) {
    module.exports = { API_CONFIG };
}
