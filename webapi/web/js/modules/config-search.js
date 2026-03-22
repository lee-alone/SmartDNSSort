/**
 * Configuration Search Module
 * Provides quick search functionality for configuration items
 */

const ConfigSearch = (function() {
    // 配置
    const CONFIG = {
        searchInputId: 'config-search-input',
        searchContainerId: 'config-search-container',
        resultsContainerId: 'config-search-results',
        minSearchLength: 1,
        debounceDelay: 200,
        highlightClass: 'search-highlight',
        noResultsClass: 'no-results',
    };

    // 状态
    let searchInput = null;
    let resultsContainer = null;
    let debounceTimer = null;
    let currentIndex = -1;
    let matchedElements = [];

    // 搜索索引（配置项元数据）
    let searchIndex = [];

    /**
     * 初始化搜索功能
     */
    function init() {
        // 构建搜索索引
        buildSearchIndex();
        
        // 创建搜索 UI
        createSearchUI();
        
        // 绑定事件
        bindEvents();
        
        console.log('[ConfigSearch] Initialized with', searchIndex.length, 'items');
    }

    /**
     * 构建搜索索引
     */
    function buildSearchIndex() {
        searchIndex = [];

        // 配置项定义
        const configItems = [
            // DNS 基础配置
            { id: 'dns-listen-port', section: 'dns', keywords: ['dns', 'port', 'listen', '端口', '监听'], title: 'DNS Listen Port', titleZh: 'DNS 监听端口' },
            { id: 'dns-protocol', section: 'dns', keywords: ['dns', 'protocol', 'tcp', 'udp', '协议'], title: 'DNS Protocol', titleZh: 'DNS 协议' },
            
            // 上游配置
            { id: 'upstream-servers', section: 'upstream', keywords: ['upstream', 'server', 'dns', 'server', '上游', '服务器'], title: 'Upstream Servers', titleZh: '上游服务器' },
            { id: 'upstream-strategy', section: 'upstream', keywords: ['upstream', 'strategy', 'random', 'parallel', 'sequential', 'racing', '策略', '负载均衡'], title: 'Upstream Strategy', titleZh: '上游策略' },
            { id: 'upstream-timeout', section: 'upstream', keywords: ['upstream', 'timeout', '超时', '时间'], title: 'Upstream Timeout', titleZh: '上游超时' },
            { id: 'upstream-concurrency', section: 'upstream', keywords: ['upstream', 'concurrency', '并发', '数量'], title: 'Upstream Concurrency', titleZh: '上游并发数' },
            { id: 'bootstrap-dns', section: 'upstream', keywords: ['bootstrap', 'dns', '引导', '启动'], title: 'Bootstrap DNS', titleZh: '引导 DNS' },
            
            // 缓存配置
            { id: 'cache-max-memory', section: 'cache', keywords: ['cache', 'memory', '缓存', '内存', '大小'], title: 'Cache Max Memory', titleZh: '缓存最大内存' },
            { id: 'cache-ttl', section: 'cache', keywords: ['cache', 'ttl', 'expire', '缓存', '过期', '时间'], title: 'Cache TTL', titleZh: '缓存 TTL' },
            { id: 'cache-min-ttl', section: 'cache', keywords: ['cache', 'min', 'ttl', '缓存', '最小', '过期'], title: 'Cache Min TTL', titleZh: '最小缓存 TTL' },
            { id: 'cache-max-ttl', section: 'cache', keywords: ['cache', 'max', 'ttl', '缓存', '最大', '过期'], title: 'Cache Max TTL', titleZh: '最大缓存 TTL' },
            { id: 'cache-negative-ttl', section: 'cache', keywords: ['cache', 'negative', 'ttl', '缓存', '否定', '失败'], title: 'Cache Negative TTL', titleZh: '否定缓存 TTL' },
            
            // Ping 配置
            { id: 'ping-count', section: 'ping', keywords: ['ping', 'count', '探测', '次数', '数量'], title: 'Ping Count', titleZh: 'Ping 次数' },
            { id: 'ping-timeout', section: 'ping', keywords: ['ping', 'timeout', '探测', '超时'], title: 'Ping Timeout', titleZh: 'Ping 超时' },
            { id: 'ping-concurrency', section: 'ping', keywords: ['ping', 'concurrency', '探测', '并发'], title: 'Ping Concurrency', titleZh: 'Ping 并发数' },
            { id: 'ping-strategy', section: 'ping', keywords: ['ping', 'strategy', 'min', 'avg', 'auto', '探测', '策略'], title: 'Ping Strategy', titleZh: 'Ping 策略' },
            
            // AdBlock 配置
            { id: 'adblock-enable', section: 'adblock', keywords: ['adblock', 'enable', '广告', '拦截', '启用'], title: 'AdBlock Enable', titleZh: '启用广告拦截' },
            { id: 'adblock-block-mode', section: 'adblock', keywords: ['adblock', 'block', 'mode', '广告', '拦截', '模式'], title: 'AdBlock Block Mode', titleZh: '拦截模式' },
            { id: 'adblock-sources', section: 'adblock', keywords: ['adblock', 'source', '规则', '来源', '广告'], title: 'AdBlock Sources', titleZh: '规则来源' },
            
            // WebUI 配置
            { id: 'webui-port', section: 'webui', keywords: ['webui', 'port', 'web', '界面', '端口'], title: 'WebUI Port', titleZh: 'WebUI 端口' },
            { id: 'webui-enabled', section: 'webui', keywords: ['webui', 'enable', 'web', '界面', '启用'], title: 'WebUI Enabled', titleZh: '启用 WebUI' },
            
            // 系统配置
            { id: 'system-cpu-cores', section: 'system', keywords: ['system', 'cpu', 'core', '系统', '核心'], title: 'Max CPU Cores', titleZh: '最大 CPU 核心数' },
            { id: 'system-workers', section: 'system', keywords: ['system', 'worker', 'thread', '系统', '工作', '线程'], title: 'Worker Count', titleZh: '工作线程数' },
            
            // 统计配置
            { id: 'stats-hot-domains', section: 'stats', keywords: ['stats', 'hot', 'domain', '统计', '热门', '域名'], title: 'Hot Domains Window', titleZh: '热门域名统计窗口' },
            { id: 'stats-blocked-domains', section: 'stats', keywords: ['stats', 'blocked', 'domain', '统计', '拦截', '域名'], title: 'Blocked Domains Window', titleZh: '拦截域名统计窗口' },
        ];

        searchIndex = configItems;
    }

    /**
     * 创建搜索 UI
     */
    function createSearchUI() {
        // 检查是否已存在
        if (document.getElementById(CONFIG.searchContainerId)) {
            return;
        }

        // 查找侧边栏
        const sidebar = document.querySelector('.sidebar, #sidebar, [role="navigation"]');
        if (!sidebar) {
            console.warn('[ConfigSearch] Sidebar not found');
            return;
        }

        // 创建搜索容器
        const container = document.createElement('div');
        container.id = CONFIG.searchContainerId;
        container.style.cssText = `
            padding: 12px;
            border-bottom: 1px solid #e5e7eb;
        `;

        // 创建搜索输入框
        const inputWrapper = document.createElement('div');
        inputWrapper.style.cssText = `
            position: relative;
        `;

        searchInput = document.createElement('input');
        searchInput.type = 'text';
        searchInput.id = CONFIG.searchInputId;
        searchInput.placeholder = document.documentElement.lang?.startsWith('zh') 
            ? '搜索配置项...' 
            : 'Search config...';
        searchInput.style.cssText = `
            width: 100%;
            padding: 8px 12px;
            padding-left: 32px;
            border: 1px solid #d1d5db;
            border-radius: 6px;
            font-size: 14px;
            outline: none;
            transition: border-color 0.2s, box-shadow 0.2s;
        `;
        searchInput.setAttribute('aria-label', document.documentElement.lang?.startsWith('zh') 
            ? '搜索配置项' 
            : 'Search configuration items');

        // 搜索图标
        const searchIcon = document.createElement('span');
        searchIcon.innerHTML = `
            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <circle cx="11" cy="11" r="8"></circle>
                <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
            </svg>
        `;
        searchIcon.style.cssText = `
            position: absolute;
            left: 10px;
            top: 50%;
            transform: translateY(-50%);
            color: #9ca3af;
            pointer-events: none;
        `;

        // 创建结果容器
        resultsContainer = document.createElement('div');
        resultsContainer.id = CONFIG.resultsContainerId;
        resultsContainer.style.cssText = `
            display: none;
            position: absolute;
            top: 100%;
            left: 0;
            right: 0;
            background: white;
            border: 1px solid #e5e7eb;
            border-radius: 6px;
            margin-top: 4px;
            max-height: 300px;
            overflow-y: auto;
            z-index: 1000;
            box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1);
        `;

        // 组装 UI
        inputWrapper.appendChild(searchIcon);
        inputWrapper.appendChild(searchInput);
        inputWrapper.appendChild(resultsContainer);
        container.appendChild(inputWrapper);

        // 插入到侧边栏顶部
        sidebar.insertBefore(container, sidebar.firstChild);
    }

    /**
     * 绑定事件
     */
    function bindEvents() {
        if (!searchInput) return;

        // 输入事件（带防抖）
        searchInput.addEventListener('input', (e) => {
            clearTimeout(debounceTimer);
            debounceTimer = setTimeout(() => {
                handleSearch(e.target.value);
            }, CONFIG.debounceDelay);
        });

        // 键盘导航
        searchInput.addEventListener('keydown', (e) => {
            switch (e.key) {
                case 'ArrowDown':
                    e.preventDefault();
                    navigateResults(1);
                    break;
                case 'ArrowUp':
                    e.preventDefault();
                    navigateResults(-1);
                    break;
                case 'Enter':
                    e.preventDefault();
                    selectCurrentResult();
                    break;
                case 'Escape':
                    e.preventDefault();
                    clearSearch();
                    break;
            }
        });

        // 失焦时隐藏结果
        searchInput.addEventListener('blur', () => {
            setTimeout(() => {
                if (resultsContainer) {
                    resultsContainer.style.display = 'none';
                }
            }, 200);
        });

        // 聚焦时显示结果
        searchInput.addEventListener('focus', () => {
            if (searchInput.value.length >= CONFIG.minSearchLength) {
                resultsContainer.style.display = 'block';
            }
        });
    }

    /**
     * 处理搜索
     * @param {string} query - 搜索关键词
     */
    function handleSearch(query) {
        const trimmedQuery = query.trim().toLowerCase();
        
        if (trimmedQuery.length < CONFIG.minSearchLength) {
            resultsContainer.style.display = 'none';
            return;
        }

        // 搜索匹配项
        matchedElements = searchIndex.filter(item => {
            const titleMatch = item.title.toLowerCase().includes(trimmedQuery) ||
                              item.titleZh.includes(trimmedQuery);
            const keywordMatch = item.keywords.some(kw => kw.includes(trimmedQuery));
            return titleMatch || keywordMatch;
        });

        // 渲染结果
        renderResults(matchedElements, trimmedQuery);
    }

    /**
     * 渲染搜索结果
     * @param {Array} results - 搜索结果
     * @param {string} query - 搜索关键词
     */
    function renderResults(results, query) {
        if (!resultsContainer) return;

        resultsContainer.innerHTML = '';

        if (results.length === 0) {
            const noResults = document.createElement('div');
            noResults.className = CONFIG.noResultsClass;
            noResults.style.cssText = `
                padding: 12px;
                text-align: center;
                color: #6b7280;
                font-size: 14px;
            `;
            noResults.textContent = document.documentElement.lang?.startsWith('zh') 
                ? '未找到匹配的配置项' 
                : 'No matching configuration items found';
            resultsContainer.appendChild(noResults);
        } else {
            results.forEach((item, index) => {
                const resultItem = document.createElement('div');
                resultItem.className = 'search-result-item';
                resultItem.style.cssText = `
                    padding: 10px 12px;
                    cursor: pointer;
                    border-bottom: 1px solid #f3f4f6;
                    transition: background-color 0.15s;
                `;
                resultItem.setAttribute('data-index', index);
                resultItem.setAttribute('data-section', item.section);
                resultItem.setAttribute('data-id', item.id);

                // 标题
                const title = document.createElement('div');
                title.style.cssText = `
                    font-weight: 500;
                    color: #111827;
                    font-size: 14px;
                `;
                const displayTitle = document.documentElement.lang?.startsWith('zh') ? item.titleZh : item.title;
                title.textContent = displayTitle;

                // 分区标签
                const section = document.createElement('div');
                section.style.cssText = `
                    font-size: 12px;
                    color: #6b7280;
                    margin-top: 2px;
                `;
                section.textContent = document.documentElement.lang?.startsWith('zh') 
                    ? getSectionNameZh(item.section)
                    : getSectionName(item.section);

                resultItem.appendChild(title);
                resultItem.appendChild(section);

                // 悬停效果
                resultItem.addEventListener('mouseenter', () => {
                    resultItem.style.backgroundColor = '#f3f4f6';
                });
                resultItem.addEventListener('mouseleave', () => {
                    resultItem.style.backgroundColor = 'transparent';
                });

                // 点击事件
                resultItem.addEventListener('click', () => {
                    navigateToConfigItem(item);
                });

                resultsContainer.appendChild(resultItem);
            });
        }

        resultsContainer.style.display = 'block';
        currentIndex = -1;
    }

    /**
     * 获取分区名称（英文）
     */
    function getSectionName(section) {
        const names = {
            'dns': 'DNS',
            'upstream': 'Upstream',
            'cache': 'Cache',
            'ping': 'Ping',
            'adblock': 'AdBlock',
            'webui': 'WebUI',
            'system': 'System',
            'stats': 'Statistics',
        };
        return names[section] || section;
    }

    /**
     * 获取分区名称（中文）
     */
    function getSectionNameZh(section) {
        const names = {
            'dns': 'DNS 配置',
            'upstream': '上游配置',
            'cache': '缓存配置',
            'ping': 'Ping 配置',
            'adblock': '广告拦截',
            'webui': 'WebUI 配置',
            'system': '系统配置',
            'stats': '统计配置',
        };
        return names[section] || section;
    }

    /**
     * 键盘导航结果
     * @param {number} direction - 方向 (1: 下, -1: 上)
     */
    function navigateResults(direction) {
        const items = resultsContainer.querySelectorAll('.search-result-item');
        if (items.length === 0) return;

        // 移除当前选中
        if (currentIndex >= 0 && items[currentIndex]) {
            items[currentIndex].style.backgroundColor = 'transparent';
        }

        // 更新索引
        currentIndex += direction;
        if (currentIndex < 0) currentIndex = items.length - 1;
        if (currentIndex >= items.length) currentIndex = 0;

        // 高亮新选中项
        if (items[currentIndex]) {
            items[currentIndex].style.backgroundColor = '#f3f4f6';
            items[currentIndex].scrollIntoView({ block: 'nearest' });
        }
    }

    /**
     * 选择当前结果
     */
    function selectCurrentResult() {
        const items = resultsContainer.querySelectorAll('.search-result-item');
        if (currentIndex >= 0 && items[currentIndex]) {
            items[currentIndex].click();
        }
    }

    /**
     * 导航到配置项
     * @param {Object} item - 配置项
     */
    function navigateToConfigItem(item) {
        // 清除搜索
        clearSearch();

        // 切换到配置视图
        const configView = document.getElementById('view-config');
        if (configView) {
            // 隐藏所有视图
            document.querySelectorAll('.view-content').forEach(view => {
                view.style.display = 'none';
            });
            // 显示配置视图
            configView.style.display = 'block';
        }

        // 展开对应的配置分区
        const section = document.querySelector(`[data-section="${item.section}"], #config-section-${item.section}`);
        if (section) {
            // 如果是折叠状态，展开它
            const collapse = section.closest('.collapse') || section.querySelector('.collapse');
            if (collapse && !collapse.classList.contains('show')) {
                collapse.classList.add('show');
            }
        }

        // 滚动到配置项
        setTimeout(() => {
            const element = document.getElementById(item.id);
            if (element) {
                element.scrollIntoView({ behavior: 'smooth', block: 'center' });
                
                // 高亮效果
                element.style.transition = 'background-color 0.3s';
                element.style.backgroundColor = '#fef3c7';
                setTimeout(() => {
                    element.style.backgroundColor = '';
                }, 2000);
            }
        }, 300);
    }

    /**
     * 清除搜索
     */
    function clearSearch() {
        if (searchInput) {
            searchInput.value = '';
        }
        if (resultsContainer) {
            resultsContainer.style.display = 'none';
        }
        matchedElements = [];
        currentIndex = -1;
    }

    /**
     * 更新搜索索引（动态添加）
     * @param {Object} item - 配置项
     */
    function addToIndex(item) {
        searchIndex.push(item);
    }

    // 公开 API
    return {
        init,
        clearSearch,
        addToIndex,
        getSearchIndex: () => searchIndex,
    };
})();

// 导出模块
if (typeof module !== 'undefined' && module.exports) {
    module.exports = ConfigSearch;
}
