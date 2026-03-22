// Dashboard / Stats Logic Module

// ==================== 常量定义 ====================
const STATS_PERIOD_STORAGE_KEY = 'dashboard_stats_period';
const RELOAD_DELAY_MS = 5000;
const INITIAL_RETRY_DELAY_MS = 1000;
const RETRY_BACKOFF_MULTIPLIER = 2;
const MAX_RETRIES = 3;

// ==================== 全局变量 ====================
let statsPeriodDays = 7;
let currentAbortController = null;

// 数据缓存变量（用于乐观更新）
let lastStatsData = null;
let lastUpstreamData = null;

// DOM 元素缓存（优化性能，避免重复查询）
let statsElements = null;

// 动画帧 ID 映射（用于取消动画，防止冲突）
const animationFrameIds = new Map();

// 添加清理函数
let cleanupRegistered = false;

function registerCleanup() {
    if (cleanupRegistered) return;
    cleanupRegistered = true;
    
    // 页面卸载时清理
    window.addEventListener('beforeunload', () => {
        cleanupAllResources();
    });
    
    // 导航切换时清理（如果使用了 SPA 路由）
    document.addEventListener('view-change', () => {
        if (document.visibilityState === 'hidden') {
            cleanupAllResources();
        }
    });
}

function cleanupAllResources() {
    // 清除定时器
    if (window.dashboardInterval) {
        clearInterval(window.dashboardInterval);
        window.dashboardInterval = null;
    }
    
    // 中止当前请求
    if (currentAbortController) {
        currentAbortController.abort();
        currentAbortController = null;
    }
    
    // 清理所有动画帧
    animationFrameIds.forEach((frameId) => {
        cancelAnimationFrame(frameId);
    });
    animationFrameIds.clear();
}

// 添加带重试的 fetch 封装
async function fetchWithRetry(url, { signal, ...options } = {}, maxRetries = 3) {
    let delay = 1000; // 初始延迟 1 秒
    let lastError = null;
    
    for (let attempt = 1; attempt <= maxRetries; attempt++) {
        try {
            // 传递 signal 到 fetch
            const response = await fetch(url, { signal, ...options });
            
            if (response.ok) {
                return await response.json();
            }
            
            // 非 200 响应，记录错误
            lastError = new Error(`HTTP ${response.status}`);
        } catch (error) {
            // 用户主动中止时立即抛出，不重试
            if (error.name === 'AbortError') {
                console.log(`Fetch aborted: ${url}`);
                throw error;
            }
            lastError = error;
        }
        
        // 最后一次尝试失败后，直接抛出
        if (attempt === maxRetries) {
            break;
        }
        
        // 等待时也要监听 signal
        try {
            await Promise.race([
                new Promise(resolve => setTimeout(resolve, delay)),
                new Promise((_, reject) => {
                    if (signal?.aborted) {
                        reject(new DOMException('Aborted', 'AbortError'));
                    }
                    signal?.addEventListener('abort', () => {
                        reject(new DOMException('Aborted', 'AbortError'));
                    });
                })
            ]);
        } catch (abortError) {
            if (abortError.name === 'AbortError') {
                throw abortError;
            }
        }
        
        delay *= 2; // 1s -> 2s -> 4s
    }
    
    throw lastError;
}

// 初始化 DOM 元素缓存
function initializeStatsElements() {
    if (statsElements) return; // 已初始化
    
    statsElements = {
        // 基础统计
        total_queries: document.getElementById('total_queries'),
        cache_hits: document.getElementById('cache_hits'),
        cache_misses: document.getElementById('cache_misses'),
        cache_stale_refresh: document.getElementById('cache_stale_refresh'),
        cache_hit_rate: document.getElementById('cache_hit_rate'),
        upstream_failures: document.getElementById('upstream_failures'),
        
        // 系统状态
        cpu_usage_pct: document.getElementById('cpu_usage_pct'),
        cpu_cores: document.getElementById('cpu_cores'),
        mem_usage_pct: document.getElementById('mem_usage_pct'),
        mem_usage_detail: document.getElementById('mem_usage_detail'),
        goroutines: document.getElementById('goroutines'),
        system_uptime: document.getElementById('system_uptime'),
        
        // 缓存内存统计
        memory_usage_bar: document.getElementById('memory_usage_bar'),
        memory_usage_text: document.getElementById('memory_usage_text'),
        cache_entries: document.getElementById('cache_entries'),
        expired_entries: document.getElementById('expired_entries'),
        protected_entries: document.getElementById('protected_entries'),
        evictions_per_min: document.getElementById('evictions_per_min'),
        
        // 表格
        hot_domains_table: document.getElementById('hot_domains_table'),
        blocked_domains_table: document.getElementById('blocked_domains_table'),
        
        // 网络状态
        internet_status: document.getElementById('internet-status'),
        ipPoolPausedBadge: document.getElementById('ipPoolPausedBadge'),
        
        // 错误和加载状态
        dashboard_loading: document.getElementById('dashboard-loading'),
        dashboard_error: document.getElementById('dashboard-error')
    };
}

// 添加状态管理函数
function showLoadingState() {
    if (statsElements?.dashboard_loading) {
        statsElements.dashboard_loading.classList.remove('hidden');
    }
}

function hideLoadingState() {
    if (statsElements?.dashboard_loading) {
        statsElements.dashboard_loading.classList.add('hidden');
    }
}

// 数字滚动动画函数（带取消机制，防止动画冲突）
function animateNumber(element, from, to, duration = 500) {
    // 取消之前的动画
    const prevId = animationFrameIds.get(element);
    if (prevId) {
        cancelAnimationFrame(prevId);
    }
    
    const startTime = performance.now();
    const diff = to - from;
    
    function update(currentTime) {
        const elapsed = currentTime - startTime;
        const progress = Math.min(elapsed / duration, 1);
        
        // 缓动函数
        const easeOut = 1 - Math.pow(1 - progress, 3);
        const current = Math.round(from + diff * easeOut);
        
        element.textContent = current.toLocaleString();
        
        if (progress < 1) {
            const frameId = requestAnimationFrame(update);
            animationFrameIds.set(element, frameId);
        } else {
            // 动画完成，移除映射
            animationFrameIds.delete(element);
        }
    }
    
    const frameId = requestAnimationFrame(update);
    animationFrameIds.set(element, frameId);
}

// 局部更新函数：只更新变化的数值（使用缓存的 DOM 元素）
function updateGeneralStats(newData, oldData) {
    if (!oldData) {
        // 首次加载，完整渲染
        renderGeneralStats(newData);
        return;
    }
    
    // 确保元素缓存已初始化
    if (!statsElements) {
        initializeStatsElements();
    }
    
    // 只更新变化的数值
    const fields = ['total_queries', 'cache_hits', 'cache_misses', 'cache_stale_refresh', 'upstream_failures'];
    fields.forEach(field => {
        if (newData[field] !== oldData[field]) {
            const el = statsElements[field];
            if (el) {
                // 添加数字滚动动画
                animateNumber(el, oldData[field] || 0, newData[field]);
            }
        }
    });
    
    // 更新缓存命中率
    if (newData.cache_hit_rate !== oldData.cache_hit_rate) {
        const el = statsElements.cache_hit_rate;
        if (el) {
            el.textContent = newData.cache_hit_rate.toFixed(2) + '%';
        }
    }
    
    // 更新系统状态
    if (newData.system_stats) {
        const oldSys = oldData.system_stats || {};
        const newSys = newData.system_stats;
        
        if (newSys.cpu_usage_pct !== oldSys.cpu_usage_pct) {
            const el = statsElements.cpu_usage_pct;
            if (el) el.textContent = newSys.cpu_usage_pct.toFixed(1) + '%';
        }
        
        if (newSys.mem_usage_pct !== oldSys.mem_usage_pct) {
            const el = statsElements.mem_usage_pct;
            if (el) el.textContent = newSys.mem_usage_pct.toFixed(1) + '%';
        }
        
        if (newSys.mem_used_mb !== oldSys.mem_used_mb || newSys.mem_total_mb !== oldSys.mem_total_mb) {
            const memTotalMB = newSys.mem_total_mb || 0;
            const memUsedMB = newSys.mem_used_mb || 0;
            const memAvailableMB = memTotalMB - memUsedMB;
            const el = statsElements.mem_usage_detail;
            if (el) {
                el.textContent = memUsedMB + ' MB / ' + memTotalMB + ' MB (Available: ' + memAvailableMB + ' MB)';
            }
        }
        
        if (newSys.goroutines !== oldSys.goroutines) {
            const el = statsElements.goroutines;
            if (el) el.textContent = newSys.goroutines;
        }
    }
    
    // 更新运行时间
    if (newData.uptime_seconds !== oldData.uptime_seconds) {
        const el = statsElements.system_uptime;
        if (el) el.textContent = formatUptime(newData.uptime_seconds);
    }
    
    // 更新缓存内存统计
    if (newData.cache_memory_stats) {
        const oldMem = oldData.cache_memory_stats || {};
        const newMem = newData.cache_memory_stats;
        
        if (newMem.memory_percent !== oldMem.memory_percent) {
            const bar = statsElements.memory_usage_bar;
            if (bar) bar.style.width = `${newMem.memory_percent.toFixed(2)}%`;
        }
        
        if (newMem.current_memory_mb !== oldMem.current_memory_mb || newMem.max_memory_mb !== oldMem.max_memory_mb) {
            const text = statsElements.memory_usage_text;
            if (text) text.textContent = `${newMem.current_memory_mb} MB / ${newMem.max_memory_mb} MB`;
        }
        
        if (newMem.current_entries !== oldMem.current_entries || newMem.max_entries !== oldMem.max_entries) {
            const el = statsElements.cache_entries;
            if (el) el.textContent = `${newMem.current_entries.toLocaleString()} / ${newMem.max_entries.toLocaleString()}`;
        }
        
        if (newMem.expired_entries !== oldMem.expired_entries || newMem.expired_percent !== oldMem.expired_percent) {
            const el = statsElements.expired_entries;
            if (el) el.textContent = `${newMem.expired_entries.toLocaleString()} (${(newMem.expired_percent || 0).toFixed(1)}%)`;
        }
        
        if (newMem.protected_entries !== oldMem.protected_entries) {
            const el = statsElements.protected_entries;
            if (el) el.textContent = newMem.protected_entries.toLocaleString();
        }
        
        if (newMem.evictions_per_min !== oldMem.evictions_per_min) {
            const el = statsElements.evictions_per_min;
            if (el) el.textContent = (newMem.evictions_per_min || 0).toFixed(2);
        }
    }
    
    // 更新热门域名 - 使用浅对比优化性能
    if (newData.top_domains !== oldData.top_domains) {
        const hotDomainsTable = statsElements.hot_domains_table?.getElementsByTagName('tbody')[0];
        if (hotDomainsTable) {
            hotDomainsTable.innerHTML = '';
            if (newData.top_domains && newData.top_domains.length > 0) {
                newData.top_domains.forEach(item => {
                    const row = hotDomainsTable.insertRow();
                    const cell1 = row.insertCell(0);
                    cell1.className = 'px-6 py-3';
                    cell1.textContent = item.Domain;
                    const cell2 = row.insertCell(1);
                    cell2.className = 'px-6 py-3 value';
                    cell2.textContent = item.Count;
                });
            } else {
                const row = hotDomainsTable.insertRow();
                const cell = row.insertCell(0);
                cell.colSpan = 2;
                cell.className = 'px-6 py-3';
                cell.style.textAlign = 'center';
                cell.textContent = i18n.t('dashboard.noDomainData');
            }
        }
    }
    
    // 更新被拦截域名 - 使用浅对比优化性能
    if (newData.top_blocked_domains !== oldData.top_blocked_domains) {
        const blockedDomainsTable = statsElements.blocked_domains_table?.getElementsByTagName('tbody')[0];
        if (blockedDomainsTable) {
            blockedDomainsTable.innerHTML = '';
            if (newData.top_blocked_domains && newData.top_blocked_domains.length > 0) {
                newData.top_blocked_domains.forEach(item => {
                    const row = blockedDomainsTable.insertRow();
                    const cell1 = row.insertCell(0);
                    cell1.className = 'px-6 py-3';
                    cell1.textContent = item.Domain;
                    const cell2 = row.insertCell(1);
                    cell2.className = 'px-6 py-3 value';
                    cell2.textContent = item.Count;
                });
            } else {
                const row = blockedDomainsTable.insertRow();
                const cell = row.insertCell(0);
                cell.colSpan = 2;
                cell.className = 'px-6 py-3';
                cell.style.textAlign = 'center';
                cell.textContent = i18n.t('dashboard.noBlockedDomainData');
            }
        }
    }
    
    // 更新网络状态
    if (newData.network_online !== oldData.network_online) {
        const internetStatusEl = statsElements.internet_status;
        const ipPoolPausedBadge = statsElements.ipPoolPausedBadge;
        if (internetStatusEl) {
            const internetStatusDot = internetStatusEl.querySelector('.status-dot');
            const internetStatusIcon = internetStatusEl.querySelector('.status-icon');
            const internetStatusText = internetStatusEl.querySelector('[data-i18n="status.internet"]');
            const isOnline = newData.network_online !== false;
            
            if (isOnline) {
                internetStatusDot.style.backgroundColor = '#22c55e';
                internetStatusIcon.style.color = '#22c55e';
                internetStatusText.style.color = '#22c55e';
                internetStatusIcon.textContent = 'public';
                if (ipPoolPausedBadge) {
                    ipPoolPausedBadge.classList.add('hidden');
                }
            } else {
                internetStatusDot.style.backgroundColor = '#ef4444';
                internetStatusIcon.style.color = '#ef4444';
                internetStatusText.style.color = '#ef4444';
                internetStatusIcon.textContent = 'cloud_off';
                if (ipPoolPausedBadge) {
                    ipPoolPausedBadge.classList.remove('hidden');
                }
            }
        }
    }
    
}

function showErrorState(message) {
    if (statsElements?.dashboard_error) {
        statsElements.dashboard_error.textContent = `数据加载失败：${message}`;
        statsElements.dashboard_error.classList.remove('hidden');
    }
}

function hideErrorState() {
    if (statsElements?.dashboard_error) {
        statsElements.dashboard_error.classList.add('hidden');
    }
}

// 新增：部分错误状态显示
function showPartialErrorState(failureCount) {
    const errorEl = document.getElementById('dashboard-error');
    if (errorEl) {
        errorEl.textContent = `部分数据加载失败 (${failureCount} 项)，已显示可用数据`;
        errorEl.classList.remove('hidden');
        errorEl.className = 'bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800';
    }
}

// 新增：区域级错误显示 - 使用安全的 DOM 操作
function showSectionError(sectionId, message) {
const section = document.getElementById(sectionId);
if (section) {
section.innerHTML = '';
const errorDiv = document.createElement('div');
errorDiv.className = 'p-4 text-center text-red-600';
const iconSpan = document.createElement('span');
iconSpan.className = 'material-symbols-outlined';
iconSpan.textContent = 'error';
errorDiv.appendChild(iconSpan);
errorDiv.appendChild(document.createTextNode(' '));
errorDiv.appendChild(document.createTextNode(message)); // 安全：使用 DOM 操作
section.appendChild(errorDiv);
}
}

function renderGeneralStats(data) {
	document.getElementById('total_queries').textContent = data.total_queries || 0;
    document.getElementById('cache_hits').textContent = data.cache_hits || 0;
    document.getElementById('cache_misses').textContent = data.cache_misses || 0;
    document.getElementById('cache_stale_refresh').textContent = data.cache_stale_refresh || 0;
    document.getElementById('cache_hit_rate').textContent = (data.cache_hit_rate || 0).toFixed(2) + '%';
    document.getElementById('upstream_failures').textContent = data.upstream_failures || 0;
    if (data.system_stats) {
        const sys = data.system_stats;
        document.getElementById('cpu_usage_pct').textContent = (sys.cpu_usage_pct || 0).toFixed(1) + '%';
        document.getElementById('cpu_cores').textContent = sys.cpu_cores || 0;
        document.getElementById('mem_usage_pct').textContent = (sys.mem_usage_pct || 0).toFixed(1) + '%';
        
        // 显示可用内存详情
        const memTotalMB = sys.mem_total_mb || 0;
        const memUsedMB = sys.mem_used_mb || 0;
        const memAvailableMB = memTotalMB - memUsedMB;
        document.getElementById('mem_usage_detail').textContent = 
            memUsedMB + ' MB / ' + memTotalMB + ' MB (Available: ' + memAvailableMB + ' MB)';
        
        document.getElementById('goroutines').textContent = sys.goroutines || 0;
    }
    if (data.uptime_seconds) {
        document.getElementById('system_uptime').textContent = formatUptime(data.uptime_seconds);
    }
    if (data.cache_memory_stats) {
        const mem = data.cache_memory_stats;
        const memoryUsageBar = document.getElementById('memory_usage_bar');
        const memoryUsageText = document.getElementById('memory_usage_text');
        const cacheEntries = document.getElementById('cache_entries');
        const expiredEntries = document.getElementById('expired_entries');
        const protectedEntries = document.getElementById('protected_entries');
        const evictionsPerMin = document.getElementById('evictions_per_min');

        memoryUsageBar.style.width = `${mem.memory_percent.toFixed(2)}%`;
        memoryUsageText.textContent = `${mem.current_memory_mb} MB / ${mem.max_memory_mb} MB`;
        cacheEntries.textContent = `${mem.current_entries.toLocaleString()} / ${mem.max_entries.toLocaleString()}`;
        expiredEntries.textContent = `${mem.expired_entries.toLocaleString()} (${(mem.expired_percent || 0).toFixed(1)}%)`;
        protectedEntries.textContent = mem.protected_entries.toLocaleString();
        evictionsPerMin.textContent = (mem.evictions_per_min || 0).toFixed(2);
    }
    // 热门域名 - 使用安全的 DOM 操作防止 XSS
    const hotDomainsTable = document.getElementById('hot_domains_table').getElementsByTagName('tbody')[0];
    hotDomainsTable.innerHTML = '';
    if (data.top_domains && data.top_domains.length > 0) {
    data.top_domains.forEach(item => {
    const row = hotDomainsTable.insertRow();
    const cell1 = row.insertCell(0);
    cell1.className = 'px-6 py-3';
    cell1.textContent = item.Domain; // 安全：使用 textContent
    const cell2 = row.insertCell(1);
    cell2.className = 'px-6 py-3 value';
    cell2.textContent = item.Count; // 安全：使用 textContent
    });
    } else {
    const row = hotDomainsTable.insertRow();
    const cell = row.insertCell(0);
    cell.colSpan = 2;
    cell.className = 'px-6 py-3';
    cell.style.textAlign = 'center';
    cell.textContent = i18n.t('dashboard.noDomainData'); // 安全：使用 textContent
    }
    
    // 被拦截域名 - 使用安全的 DOM 操作防止 XSS
    const blockedDomainsTable = document.getElementById('blocked_domains_table').getElementsByTagName('tbody')[0];
    blockedDomainsTable.innerHTML = '';
    if (data.top_blocked_domains && data.top_blocked_domains.length > 0) {
    data.top_blocked_domains.forEach(item => {
    const row = blockedDomainsTable.insertRow();
    const cell1 = row.insertCell(0);
    cell1.className = 'px-6 py-3';
    cell1.textContent = item.Domain; // 安全：使用 textContent
    const cell2 = row.insertCell(1);
    cell2.className = 'px-6 py-3 value';
    cell2.textContent = item.Count; // 安全：使用 textContent
    });
    } else {
    const row = blockedDomainsTable.insertRow();
    const cell = row.insertCell(0);
    cell.colSpan = 2;
    cell.className = 'px-6 py-3';
    cell.style.textAlign = 'center';
    cell.textContent = i18n.t('dashboard.noBlockedDomainData'); // 安全：使用 textContent
    }
    // Update status indicator
    const statusEl = document.getElementById('status');
    const statusText = statusEl.querySelector('.status-text');
    if (statusText) statusText.textContent = i18n.t('status.connected');
    statusEl.className = 'status-indicator connected';

    // Update internet status indicator
    const internetStatusEl = document.getElementById('internet-status');
    const ipPoolPausedBadge = document.getElementById('ipPoolPausedBadge');
    if (internetStatusEl) {
        const internetStatusDot = internetStatusEl.querySelector('.status-dot');
        const internetStatusIcon = internetStatusEl.querySelector('.status-icon');
        const internetStatusText = internetStatusEl.querySelector('[data-i18n="status.internet"]');
        const isOnline = data.network_online !== false; // 默认为 true
        
        if (isOnline) {
            internetStatusDot.style.backgroundColor = '#22c55e'; // 绿色
            internetStatusIcon.style.color = '#22c55e'; // 绿色
            internetStatusText.style.color = '#22c55e'; // 绿色
            internetStatusIcon.textContent = 'public';
            // 隐藏 Badge
            if (ipPoolPausedBadge) {
                ipPoolPausedBadge.classList.add('hidden');
            }
        } else {
            internetStatusDot.style.backgroundColor = '#ef4444'; // 红色
            internetStatusIcon.style.color = '#ef4444'; // 红色
            internetStatusText.style.color = '#ef4444'; // 红色
            internetStatusIcon.textContent = 'cloud_off';
            // 显示 Badge
            if (ipPoolPausedBadge) {
                ipPoolPausedBadge.classList.remove('hidden');
            }
        }
    }

}

// 虚拟列表实例缓存
let recentQueriesVirtualList = null;
let recentlyBlockedVirtualList = null;

function renderRecentQueries(data) {
    const recentQueriesList = document.getElementById('recent_queries_list');
    
    // 如果数据量大，使用分页列表
    if (data && data.length > 100) {
        // 清理之前的实例
        if (recentQueriesVirtualList) {
            recentQueriesVirtualList.destroy();
        }
        recentQueriesList.innerHTML = '';
        
        // 创建分页列表
        recentQueriesVirtualList = VirtualList.createPaginated({
            container: recentQueriesList,
            pageSize: 50,
            renderItem: (domain, index) => {
                const div = document.createElement('div');
                div.className = 'list-item';
                div.style.cssText = 'padding: 4px 8px; border-bottom: 1px solid #eee;';
                div.textContent = domain;
                return div;
            },
            renderEmpty: () => {
                const emptyDiv = document.createElement('div');
                emptyDiv.style.textAlign = 'center';
                emptyDiv.textContent = i18n.t('dashboard.noRecentQueries');
                return emptyDiv;
            }
        });
        
        recentQueriesVirtualList.setData(data);
    } else {
        // 小数据量直接渲染
        if (recentQueriesVirtualList) {
            recentQueriesVirtualList.destroy();
            recentQueriesVirtualList = null;
        }
        recentQueriesList.innerHTML = '';
        
        if (data && data.length > 0) {
            data.forEach(domain => {
                const div = document.createElement('div');
                div.className = 'list-item';
                div.style.cssText = 'padding: 4px 8px; border-bottom: 1px solid #eee;';
                div.textContent = domain;
                recentQueriesList.appendChild(div);
            });
        } else {
            const emptyDiv = document.createElement('div');
            emptyDiv.style.textAlign = 'center';
            emptyDiv.textContent = i18n.t('dashboard.noRecentQueries');
            recentQueriesList.appendChild(emptyDiv);
        }
    }
}

function renderRecentlyBlocked(data) {
    const recentlyBlockedList = document.getElementById('recently_blocked_list');
    
    // 如果数据量大，使用分页列表
    if (data && data.length > 100) {
        // 清理之前的实例
        if (recentlyBlockedVirtualList) {
            recentlyBlockedVirtualList.destroy();
        }
        recentlyBlockedList.innerHTML = '';
        
        // 创建分页列表
        recentlyBlockedVirtualList = VirtualList.createPaginated({
            container: recentlyBlockedList,
            pageSize: 50,
            renderItem: (domain, index) => {
                const div = document.createElement('div');
                div.className = 'list-item';
                div.style.cssText = 'padding: 4px 8px; border-bottom: 1px solid #eee;';
                div.textContent = domain;
                return div;
            },
            renderEmpty: () => {
                const emptyDiv = document.createElement('div');
                emptyDiv.style.textAlign = 'center';
                emptyDiv.textContent = i18n.t('dashboard.noRecentlyBlocked');
                return emptyDiv;
            }
        });
        
        recentlyBlockedVirtualList.setData(data);
    } else {
        // 小数据量直接渲染
        if (recentlyBlockedVirtualList) {
            recentlyBlockedVirtualList.destroy();
            recentlyBlockedVirtualList = null;
        }
        recentlyBlockedList.innerHTML = '';
        
        if (data && data.length > 0) {
            data.forEach(domain => {
                const div = document.createElement('div');
                div.className = 'list-item';
                div.style.cssText = 'padding: 4px 8px; border-bottom: 1px solid #eee;';
                div.textContent = domain;
                recentlyBlockedList.appendChild(div);
            });
        } else {
            const emptyDiv = document.createElement('div');
            emptyDiv.style.textAlign = 'center';
            emptyDiv.textContent = i18n.t('dashboard.noRecentlyBlocked');
            recentlyBlockedList.appendChild(emptyDiv);
        }
    }
}

// 保存时间范围到 localStorage
function saveStatsPeriod() {
    localStorage.setItem(STATS_PERIOD_STORAGE_KEY, statsPeriodDays);
}

// 从 localStorage 加载时间范围
function loadStatsPeriod() {
    const saved = localStorage.getItem(STATS_PERIOD_STORAGE_KEY);
    if (saved) {
        statsPeriodDays = parseInt(saved, 10);
    } else {
        statsPeriodDays = 7; // 默认7天
    }
}

async function updateDashboard(isManualRefresh = false) {
    const period = statsPeriodDays;
    
    // 取消之前的请求
    if (currentAbortController) {
        currentAbortController.abort();
    }
    currentAbortController = new AbortController();
    
    // 刷新按钮显示加载状态
    const refreshBtn = document.getElementById('refreshButton');
    if (refreshBtn) {
        refreshBtn.classList.add('loading');
    }
    
    // 显示进度条
    const progressBar = document.getElementById('refresh-progress');
    if (progressBar) {
        progressBar.classList.add('active');
        progressBar.style.width = '10%'; // 初始进度
    }
    
    try {
        // 只在手动刷新时显示加载弹窗
        if (isManualRefresh) {
            showLoadingState();
        }
        
        // 分阶段加载数据并更新进度条
        const statsResult = await fetchWithRetry(`/api/stats?days=${period}`, { signal: currentAbortController.signal });
        if (progressBar) progressBar.style.width = '35%'; // 第一个请求完成
        
        const upstreamResult = await fetchWithRetry(`/api/upstream-stats?days=${period}`, { signal: currentAbortController.signal });
        if (progressBar) progressBar.style.width = '60%'; // 第二个请求完成
        
        const queriesResult = await fetchWithRetry(`/api/recent-queries?days=${period}`, { signal: currentAbortController.signal });
        if (progressBar) progressBar.style.width = '80%'; // 第三个请求完成
        
        const blockedResult = await fetchWithRetry(`/api/recent-blocked?days=${period}`, { signal: currentAbortController.signal });
        if (progressBar) progressBar.style.width = '95%'; // 第四个请求完成

        // 提取成功的数据，失败的数据转换为错误对象
        const statsData = statsResult.success ? statsResult : { error: new Error('Stats request failed') };
        const upstreamData = upstreamResult.success ? upstreamResult : { error: new Error('Upstream request failed') };
        const queriesData = queriesResult.success ? queriesResult : { error: new Error('Queries request failed') };
        const blockedData = blockedResult.success ? blockedResult : { error: new Error('Blocked request failed') };

        // 乐观更新：只更新变化的部分
        if (statsData.success && statsData.data) {
            updateGeneralStats(statsData.data, lastStatsData);
            lastStatsData = statsData.data;
        } else if (isManualRefresh) {
            // 手动刷新时，首次加载或错误时完整渲染
            if (statsData && statsData.success && statsData.data) {
                renderGeneralStats(statsData.data);
            } else {
                showSectionError('general-stats', statsData?.error?.message || '统计数据获取失败');
            }
        }
        
        if (upstreamData.success && upstreamData.data && upstreamData.data.servers) {
            renderEnhancedUpstreamTable(upstreamData.data.servers);
            lastUpstreamData = upstreamData.data;
        } else if (isManualRefresh) {
            showSectionError('upstream-stats', upstreamData?.error?.message || '上游统计获取失败');
        }
        
        if (queriesData.success && queriesData.data) {
        	renderRecentQueries(queriesData.data);
        } else if (isManualRefresh) {
        	showSectionError('recent-queries', queriesData?.error?.message || '最近查询获取失败');
        }
       
        if (blockedData.success && blockedData.data) {
        	renderRecentlyBlocked(blockedData.data);
        } else if (isManualRefresh) {
        	showSectionError('recent-blocked', blockedData?.error?.message || '最近拦截获取失败');
        }
        
        // 检查是否有失败，显示部分错误提示
        const failures = results.filter(r => r.status === 'rejected');
        if (failures.length > 0) {
            showPartialErrorState(failures.length);
        } else {
            hideErrorState();
        }
        
        // Update AdBlock status statistics (Blocked Today and Blocked Total)
        if (typeof updateAdBlockTab === 'function') {
            updateAdBlockTab();
        }
    } catch (error) {
        if (error.name === 'AbortError') {
            console.log('Dashboard update aborted');
            return; // 被中止时不显示错误
        }
        console.error('Dashboard update failed:', error);
        if (isManualRefresh) {
            showErrorState(error.message); // 只在手动刷新时显示错误提示
        }
    } finally {
        // 只在手动刷新时隐藏加载弹窗
        if (isManualRefresh) {
            hideLoadingState();
        }
        // 移除刷新按钮加载状态
        if (refreshBtn) {
            refreshBtn.classList.remove('loading');
        }
        // 隐藏进度条
        if (progressBar) {
            progressBar.style.width = '100%'; // 完成进度
            setTimeout(() => {
                progressBar.classList.remove('active');
                progressBar.style.width = '0%';
            }, 300);
        }
    }
}

function initializeDashboardButtons() {
    // 初始化时间范围选择器
    initializeStatsSelectors();
    
    document.getElementById('clearCacheButton').addEventListener('click', async () => {
        if (!confirm(i18n.t('messages.confirmClearCache'))) return;
        try {
            const response = await CSRFManager.secureFetch('/api/cache/clear', { method: 'POST' });
            if (response.ok) {
                alert(i18n.t('messages.cacheCleared'));
                updateDashboard(true);  // 清除缓存后手动刷新
            } else {
                alert(i18n.t('messages.cacheClearFailed'));
            }
        } catch (error) {
            console.error('Error clearing cache:', error);
            alert(i18n.t('messages.cacheClearError'));
        }
    });

    document.getElementById('clearStatsButton').addEventListener('click', async () => {
        if (!confirm(i18n.t('messages.confirmClearStats'))) return;
        
        try {
            // 清除常规统计
            const statsResponse = await CSRFManager.secureFetch('/api/stats/clear', { method: 'POST' });
            if (!statsResponse.ok) throw new Error('Failed to clear stats');
            
            // 清除上游服务器统计
            const upstreamResponse = await CSRFManager.secureFetch('/api/upstream-stats/clear', { method: 'POST' });
            if (upstreamResponse.ok) {
                alert(i18n.t('messages.statsCleared'));
                updateDashboard(true);  // 清除统计后手动刷新
            } else {
                alert(i18n.t('messages.statsClearFailed'));
            }
        } catch (error) {
            console.error('Error clearing stats:', error);
            alert(i18n.t('messages.statsClearError'));
        }
    });

    document.getElementById('refreshButton').addEventListener('click', () => {
        updateDashboard(true);  // 手动刷新，传入 true
        updateAdBlockTab();
    });

    document.getElementById('restartButton').addEventListener('click', () => {
        if (!confirm(i18n.t('messages.restartConfirm'))) return;
        
        const currentConfig = originalConfig;
        if (currentConfig && Object.keys(currentConfig).length === 0) {
            alert('Configuration not loaded. Please refresh and try again.');
            return;
        }
        
        const performRestart = async () => {
        	try {
        		await ServiceMonitor.restartWithMonitor({
        			restartEndpoint: '/api/restart',
        			onSuccess: () => {
        				// 服务已恢复，刷新页面
        				location.reload();
        			},
        			onFailure: (error) => {
        				console.error('Restart failed:', error);
        				alert(i18n.t('messages.restartFailed'));
        			},
        			onStatusChange: (status) => {
        				console.log('Restart status:', status);
        			}
        		});
        	} catch (error) {
        		console.error('Restart error:', error);
        		alert(i18n.t('messages.restartError', { error: error.message || error }));
        	}
        };
        
        const form = document.getElementById('configForm');
        if (form && form.style.display !== 'none') {
            if (confirm('Do you want to save the current configuration changes before restarting?')) {
                saveConfig()
                    .then(() => {
                        setTimeout(performRestart, 500);
                    })
                    .catch(error => {
                        // Config save failed, abort restart
                    });
            } else {
                performRestart();
            }
        } else {
            performRestart();
        }
    });
    
    // 触发动态加载组件的翻译
    if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
        window.i18n.applyTranslations();
    }
}

document.addEventListener('componentsLoaded', () => {
    initializeStatsElements();  // 初始化 DOM 元素缓存
    initializeDashboardButtons();
    registerCleanup();
});

window.addEventListener('languageChanged', () => {
    // 应用翻译到所有 DOM 元素
    if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
        window.i18n.applyTranslations();
    }
    updateDashboard(true);  // 语言切换时手动刷新
    if (!window.dashboardInterval) {
        window.dashboardInterval = setInterval(() => updateDashboard(false), 5000);  // 自动刷新，传入 false
    }
});


function initializeStatsSelectors() {
    // 加载保存的时间范围
    loadStatsPeriod();
    
    const periodSelect = document.getElementById('stats_period_select');
    if (periodSelect) {
        // 设置下拉菜单的当前值
        periodSelect.value = statsPeriodDays;
        
        // 监听变化
        periodSelect.addEventListener('change', (e) => {
            statsPeriodDays = parseInt(e.target.value, 10);
            saveStatsPeriod(); // 保存到 localStorage
            updateDashboard(); // 使用新时间范围更新数据
        });
    }
}

// 添加行更新辅助函数
function updateRow(row, data, columns) {
    const cells = row.querySelectorAll('td');
    if (cells.length !== columns.length) {
        return false; // 列数不匹配，需要重新渲染
    }
    
    columns.forEach((col, index) => {
        const value = data[col.field];
        if (cells[index].dataset.value != String(value)) {
            cells[index].textContent = value;
            cells[index].dataset.value = String(value);
        }
    });
    return true;
}

// 优化 renderEnhancedUpstreamTable
function renderEnhancedUpstreamTable(upstreamData) {
    const tbody = document.getElementById('upstream_stats')?.getElementsByTagName('tbody')[0];
    if (!tbody) return;

    const validServers = upstreamData.filter(server =>
        server.address && server.protocol && server.success_rate !== undefined
    );

    if (validServers.length === 0) {
        showUpstreamLoadError();
        return;
    }

    const existingRows = tbody.querySelectorAll('tr');
    
    // 构建地址到行的映射
    const rowMap = new Map();
    existingRows.forEach(row => {
        const address = row.dataset.serverAddress;
        if (address) {
            rowMap.set(address, row);
        }
    });
    
    // 检查是否需要重新排序
    let needsReorder = false;
    if (existingRows.length === validServers.length) {
        for (let i = 0; i < validServers.length; i++) {
            const expectedAddress = validServers[i].address;
            const actualRow = existingRows[i];
            if (actualRow.dataset.serverAddress !== expectedAddress) {
                needsReorder = true;
                break;
            }
        }
    } else {
        needsReorder = true;
    }
    
    // 如果需要重新排序，使用 DocumentFragment 全量渲染
    if (needsReorder) {
        const fragment = document.createDocumentFragment();
        validServers.forEach(server => {
            const row = createServerRow(server);
            row.dataset.serverAddress = server.address; // 添加唯一标识
            fragment.appendChild(row);
        });
        tbody.innerHTML = '';
        tbody.appendChild(fragment);
        return;
    }
    
    // 不需要重新排序时，按地址映射增量更新
    validServers.forEach(server => {
        const row = rowMap.get(server.address);
        if (row) {
            updateServerRow(row, server);
        }
    });
}

// 创建服务器行 - 使用安全的 DOM 操作防止 XSS
function createServerRow(server) {
const row = document.createElement('tr');
row.className = 'divide-y divide-[#e9e8ce] dark:divide-[#3a3922]';
row.dataset.serverAddress = server.address;

// 单元格 1: 地址
const cell1 = document.createElement('td');
cell1.className = 'px-6 py-3 font-medium';
cell1.textContent = server.address; // 安全：使用 textContent
row.appendChild(cell1);

// 单元格 2: 协议徽章（getProtocolBadge 返回静态 HTML）
const cell2 = document.createElement('td');
cell2.className = 'px-6 py-3';
cell2.innerHTML = getProtocolBadge(server.protocol); // 静态函数，安全
row.appendChild(cell2);

// 单元格 3: 成功率进度条
const cell3 = document.createElement('td');
cell3.className = 'px-6 py-3';
const progressContainer = document.createElement('div');
progressContainer.className = 'flex items-center gap-2';
const progressBg = document.createElement('div');
progressBg.className = 'w-20 bg-gray-200 rounded-full h-2';
const progressBar = document.createElement('div');
progressBar.className = `h-2 rounded-full ${getRateColor(server.success_rate)}`;
progressBar.style.width = `${server.success_rate}%`;
progressBg.appendChild(progressBar);
progressContainer.appendChild(progressBg);
const rateSpan = document.createElement('span');
rateSpan.className = 'text-sm font-medium';
rateSpan.textContent = `${server.success_rate.toFixed(1)}%`; // 安全：使用 textContent
progressContainer.appendChild(rateSpan);
cell3.appendChild(progressContainer);
row.appendChild(cell3);

// 单元格 4: 状态（getStatusIcon 返回静态 HTML，getStatusText 返回文本）
const cell4 = document.createElement('td');
cell4.className = 'px-6 py-3';
cell4.innerHTML = `${getStatusIcon(server.status)} `; // 静态函数，安全
const statusText = document.createTextNode(getStatusText(server.status));
cell4.appendChild(statusText);
row.appendChild(cell4);

// 单元格 5: 延迟
const cell5 = document.createElement('td');
cell5.className = `px-6 py-3 ${getLatencyClass(server.latency_ms)}`;
cell5.textContent = `${server.latency_ms.toFixed(1)} ms`; // 安全：使用 textContent
row.appendChild(cell5);

// 单元格 6: 总数
const cell6 = document.createElement('td');
cell6.className = 'px-6 py-3 text-gray-500';
cell6.textContent = server.total; // 安全：使用 textContent
row.appendChild(cell6);

// 单元格 7: 成功数
const cell7 = document.createElement('td');
cell7.className = 'px-6 py-3 text-green-600';
cell7.textContent = server.success; // 安全：使用 textContent
row.appendChild(cell7);

// 单元格 8: 失败数
const cell8 = document.createElement('td');
cell8.className = 'px-6 py-3 text-red-600';
cell8.textContent = server.failure; // 安全：使用 textContent
row.appendChild(cell8);

return row;
}

// 更新服务器行（增量）
function updateServerRow(row, server) {
    try {
        const cells = row.querySelectorAll('td');
        if (cells.length < 8) return false;
        
        // 只更新变化的内容
        if (cells[0].textContent !== server.address) {
            cells[0].textContent = server.address;
        }
        
        // 更新成功率进度条
        const progressBar = cells[2].querySelector('.h-2');
        if (progressBar) {
            progressBar.style.width = `${server.success_rate}%`;
            progressBar.className = `h-2 rounded-full ${getRateColor(server.success_rate)}`;
        }
        
        // 更新成功率文本
        const rateText = cells[2].querySelector('.text-sm');
        if (rateText) rateText.textContent = `${server.success_rate.toFixed(1)}%`;
        
        // 更新状态 - 使用安全的 DOM 操作
        cells[3].innerHTML = getStatusIcon(server.status); // 静态函数，安全
        const statusTextNode = document.createTextNode(' ' + getStatusText(server.status));
        cells[3].appendChild(statusTextNode);
        
        // 更新延迟
        cells[4].className = `px-6 py-3 ${getLatencyClass(server.latency_ms)}`;
        cells[4].textContent = `${server.latency_ms.toFixed(1)} ms`;
        
        // 更新统计数字
        cells[5].textContent = server.total;
        cells[6].textContent = server.success;
        cells[7].textContent = server.failure;
        
        return true;
    } catch (e) {
        return false;
    }
}

// 显示加载失败提示 - 使用安全的 DOM 操作
function showUpstreamLoadError() {
const tbody = document.getElementById('upstream_stats')?.getElementsByTagName('tbody')[0];
if (tbody) {
// 确保 i18n 已初始化，否则使用默认英文消息
let errorMsg = 'Failed to load upstream server data - Retrying in next update cycle';
if (window.i18n && typeof window.i18n.t === 'function') {
try {
errorMsg = `${i18n.t('upstream.dataLoadFailed')} - ${i18n.t('upstream.retryingNextCycle')}`;
} catch (e) {
// i18n translation failed, use default message
}
}

// 安全：使用 DOM 操作替代 innerHTML
tbody.innerHTML = '';
const row = document.createElement('tr');
const cell = document.createElement('td');
cell.colSpan = 8;
cell.className = 'px-6 py-4 text-center text-red-600';
cell.textContent = errorMsg; // 安全：使用 textContent
row.appendChild(cell);
tbody.appendChild(row);
}
}

// 获取成功率进度条颜色
function getRateColor(rate) {
    if (rate >= 90) return 'bg-green-500';
    if (rate >= 70) return 'bg-yellow-500';
    return 'bg-red-500';
}

// 获取健康状态图标和翻译
function getStatusIcon(status) {
    switch(status) {
        case 'healthy': return '🟢';
        case 'degraded': return '🟡';
        case 'unhealthy': return '🔴';
        default: return '⚪';
    }
}

// 获取状态的本地化文本
function getStatusText(status) {
    const statusMap = {
        'healthy': 'upstream.status.healthy',
        'degraded': 'upstream.status.degraded',
        'unhealthy': 'upstream.status.unhealthy'
    };
    
    const i18nKey = statusMap[status] || 'upstream.status.unknown';
    
    // 如果 i18n 可用，使用翻译；否则返回原始状态
    if (window.i18n && typeof window.i18n.t === 'function') {
        try {
            return window.i18n.t(i18nKey);
        } catch (e) {
            return status;
        }
    }
    return status;
}

// 获取延迟颜色分类
function getLatencyClass(latency) {
    if (latency < 50) return 'text-green-600';
    if (latency < 200) return 'text-yellow-600';
    return 'text-red-600';
}

// 获取协议 Badge
function getProtocolBadge(protocol) {
    const badges = {
        'udp': '<span class="px-2 py-1 text-xs bg-blue-100 text-blue-800 rounded">UDP</span>',
        'tcp': '<span class="px-2 py-1 text-xs bg-purple-100 text-purple-800 rounded">TCP</span>',
        'doh': '<span class="px-2 py-1 text-xs bg-green-100 text-green-800 rounded">DoH</span>',
        'dot': '<span class="px-2 py-1 text-xs bg-orange-100 text-orange-800 rounded">DoT</span>'
    };
    return badges[protocol.toLowerCase()] || `<span class="px-2 py-1 text-xs bg-gray-100 text-gray-800 rounded">${protocol}</span>`;
}
