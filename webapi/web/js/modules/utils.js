/**
 * Utility Functions Module
 * Common helper functions for the application
 */

// ==================== 常量定义 ====================
const CONSTANTS = {
    // 重试相关
    MAX_RETRIES: 3,
    INITIAL_RETRY_DELAY_MS: 1000,
    RETRY_BACKOFF_MULTIPLIER: 2,
    
    // 超时相关
    COMPONENT_LOAD_TIMEOUT_MS: 10000,
    FETCH_TIMEOUT_MS: 30000,
    
    // UI 相关
    RELOAD_DELAY_MS: 5000,
    NOTIFICATION_AUTO_CLOSE_MS: 3000,
    
    // 验证相关
    MIN_PORT: 1,
    MAX_PORT: 65535,
    MIN_TIMEOUT_MS: 100,
    MAX_TIMEOUT_MS: 30000,
    MIN_CACHE_SIZE: 0,
    MAX_CACHE_SIZE: 10000000,
};

// ==================== 安全解析函数 ====================
/**
 * 安全解析整数，避免 NaN
 * @param {string|number} value - 要解析的值
 * @param {number} defaultValue - 默认值
 * @returns {number} 解析后的整数或默认值
 */
function safeParseInt(value, defaultValue = 0) {
    const parsed = parseInt(value, 10);
    return isNaN(parsed) ? defaultValue : parsed;
}

/**
 * 安全解析浮点数，避免 NaN
 * @param {string|number} value - 要解析的值
 * @param {number} defaultValue - 默认值
 * @returns {number} 解析后的浮点数或默认值
 */
function safeParseFloat(value, defaultValue = 0) {
    const parsed = parseFloat(value);
    return isNaN(parsed) ? defaultValue : parsed;
}

// ==================== 输入验证函数 ====================
const InputValidator = {
    /**
     * 验证端口号
     * @param {number} port - 端口号
     * @returns {boolean} 是否有效
     */
    validatePort(port) {
        const p = safeParseInt(port, -1);
        return p >= CONSTANTS.MIN_PORT && p <= CONSTANTS.MAX_PORT;
    },

    /**
     * 验证超时时间
     * @param {number} timeout - 超时时间（毫秒）
     * @returns {boolean} 是否有效
     */
    validateTimeout(timeout) {
        const t = safeParseInt(timeout, -1);
        return t >= CONSTANTS.MIN_TIMEOUT_MS && t <= CONSTANTS.MAX_TIMEOUT_MS;
    },

    /**
     * 验证缓存大小
     * @param {number} size - 缓存大小
     * @returns {boolean} 是否有效
     */
    validateCacheSize(size) {
        const s = safeParseInt(size, -1);
        return s >= CONSTANTS.MIN_CACHE_SIZE && s <= CONSTANTS.MAX_CACHE_SIZE;
    },

    /**
     * 验证配置数据
     * @param {object} data - 配置数据
     * @returns {object} { valid: boolean, errors: string[] }
     */
    validateConfig(data) {
        const errors = [];

        if (data.dns) {
            if (!this.validatePort(data.dns.listen_port)) {
                errors.push(`DNS 端口必须在 ${CONSTANTS.MIN_PORT}-${CONSTANTS.MAX_PORT} 之间`);
            }
        }

        if (data.upstream) {
            if (!this.validateTimeout(data.upstream.timeout_ms)) {
                errors.push(`超时时间必须在 ${CONSTANTS.MIN_TIMEOUT_MS}-${CONSTANTS.MAX_TIMEOUT_MS}ms 之间`);
            }
        }

        if (data.cache) {
            if (!this.validateCacheSize(data.cache.size)) {
                errors.push(`缓存大小必须在 ${CONSTANTS.MIN_CACHE_SIZE}-${CONSTANTS.MAX_CACHE_SIZE} 之间`);
            }
        }

        return {
            valid: errors.length === 0,
            errors
        };
    }
};

// ==================== 防抖和节流函数 ====================
/**
 * 防抖函数
 * @param {Function} func - 要执行的函数
 * @param {number} wait - 等待时间（毫秒）
 * @returns {Function} 防抖后的函数
 */
function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
        const later = () => {
            clearTimeout(timeout);
            func(...args);
        };
        clearTimeout(timeout);
        timeout = setTimeout(later, wait);
    };
}

/**
 * 节流函数
 * @param {Function} func - 要执行的函数
 * @param {number} limit - 时间限制（毫秒）
 * @returns {Function} 节流后的函数
 */
function throttle(func, limit) {
    let inThrottle;
    return function executedFunction(...args) {
        if (!inThrottle) {
            func(...args);
            inThrottle = true;
            setTimeout(() => inThrottle = false, limit);
        }
    };
}

// ==================== DOM 安全操作函数 ====================
/**
 * 安全地设置元素文本内容（防止 XSS）
 * @param {HTMLElement} element - 目标元素
 * @param {string} text - 要设置的文本
 */
function safeSetText(element, text) {
    if (element) {
        element.textContent = text;
    }
}

/**
 * 安全地创建表格行（防止 XSS）
 * @param {Array} cellData - 单元格数据数组
 * @param {Array} cellClasses - 单元格类名数组（可选）
 * @returns {HTMLTableRowElement} 表格行元素
 */
function createTableRow(cellData, cellClasses = []) {
    const row = document.createElement('tr');
    cellData.forEach((data, index) => {
        const cell = document.createElement('td');
        if (cellClasses[index]) {
            cell.className = cellClasses[index];
        }
        cell.textContent = data;
        row.appendChild(cell);
    });
    return row;
}

/**
 * 安全地创建带状态的表格单元格
 * @param {string} text - 文本内容
 * @param {string} className - 类名
 * @returns {HTMLTableCellElement} 表格单元格元素
 */
function createTableCell(text, className = '') {
    const cell = document.createElement('td');
    if (className) {
        cell.className = className;
    }
    cell.textContent = text;
    return cell;
}

/**
 * 安全地创建带 HTML 内容的元素（仅用于受信任的静态 HTML）
 * @param {string} tag - 标签名
 * @param {string} className - 类名
 * @param {string} htmlContent - HTML 内容（必须是静态安全的）
 * @returns {HTMLElement} 创建的元素
 */
function createSafeElement(tag, className = '', htmlContent = '') {
    const element = document.createElement(tag);
    if (className) {
        element.className = className;
    }
    if (htmlContent) {
        // 仅用于静态、安全的 HTML 内容
        element.innerHTML = htmlContent;
    }
    return element;
}

/**
 * 安全地创建带图标和文本的单元格
 * @param {string} iconHtml - 图标 HTML（必须是静态安全的）
 * @param {string} text - 文本内容
 * @returns {HTMLTableCellElement} 表格单元格元素
 */
function createIconTextCell(iconHtml, text) {
    const cell = document.createElement('td');
    // 图标 HTML 是静态的，可以安全使用
    cell.innerHTML = iconHtml + ' ';
    const textSpan = document.createElement('span');
    textSpan.textContent = text;
    cell.appendChild(textSpan);
    return cell;
}

// ==================== 带超时的 fetch 封装 ====================
/**
 * 带超时的 fetch
 * @param {string} url - 请求 URL
 * @param {object} options - fetch 选项
 * @param {number} timeout - 超时时间（毫秒）
 * @returns {Promise} fetch Promise
 */
async function fetchWithTimeout(url, options = {}, timeout = CONSTANTS.FETCH_TIMEOUT_MS) {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), timeout);

    try {
        const response = await fetch(url, {
            ...options,
            signal: controller.signal
        });
        return response;
    } finally {
        clearTimeout(timeoutId);
    }
}

// ==================== 导出 ====================
// 如果在模块环境中，使用 export
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        CONSTANTS,
        safeParseInt,
        safeParseFloat,
        InputValidator,
        debounce,
        throttle,
        safeSetText,
        createTableRow,
        createTableCell,
        createSafeElement,
        createIconTextCell,
        fetchWithTimeout
    };
}
