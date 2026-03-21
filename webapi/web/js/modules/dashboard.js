// Dashboard / Stats Logic Module

const API_URL = '/api/stats';
const STATS_PERIOD_STORAGE_KEY = 'dashboard_stats_period';

// 全局变量：统一的时间范围
let statsPeriodDays = 7;
let generalStatsAbortController = null;
let generalStatsLoading = false;

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
    
    // 保留旧的 controller 清理（向后兼容）
    if (generalStatsAbortController) {
        generalStatsAbortController.abort();
        generalStatsAbortController = null;
    }
    if (upstreamStatsAbortController) {
        upstreamStatsAbortController.abort();
        upstreamStatsAbortController = null;
    }
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

// 添加状态管理函数
function showLoadingState() {
    const loadingEl = document.getElementById('dashboard-loading');
    if (loadingEl) loadingEl.classList.remove('hidden');
}

function hideLoadingState() {
    const loadingEl = document.getElementById('dashboard-loading');
    if (loadingEl) loadingEl.classList.add('hidden');
}

function showErrorState(message) {
    const errorEl = document.getElementById('dashboard-error');
    if (errorEl) {
        errorEl.textContent = `数据加载失败：${message}`;
        errorEl.classList.remove('hidden');
    }
}

function hideErrorState() {
    const errorEl = document.getElementById('dashboard-error');
    if (errorEl) {
        errorEl.classList.add('hidden');
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

// 新增：区域级错误显示
function showSectionError(sectionId, message) {
    const section = document.getElementById(sectionId);
    if (section) {
        section.innerHTML = `
            <div class="p-4 text-center text-red-600">
                <span class="material-symbols-outlined">error</span>
                ${message}
            </div>
        `;
    }
}

// 统一的渲染函数
function renderDashboard(statsData, upstreamData, queriesData, blockedData) {
    // 核心统计优先渲染（即使其他数据失败）
    if (statsData) {
        updateGeneralStats(statsData);
    } else {
        showSectionError('general-stats', '统计数据获取失败');
    }
    
    if (upstreamData && upstreamData.data && upstreamData.data.servers) {
        renderEnhancedUpstreamTable(upstreamData.data.servers);
    } else {
        showSectionError('upstream-stats', '上游统计获取失败');
    }
    
    if (queriesData) {
        renderRecentQueries(queriesData);
    }
    
    if (blockedData) {
        renderRecentlyBlocked(blockedData);
    }
}

function updateGeneralStats(data) {
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
    // 热门域名
    const hotDomainsTable = document.getElementById('hot_domains_table').getElementsByTagName('tbody')[0];
    hotDomainsTable.innerHTML = '';
    if (data.top_domains && data.top_domains.length > 0) {
        data.top_domains.forEach(item => {
            const row = hotDomainsTable.insertRow();
            row.innerHTML = `<td class="px-6 py-3">${item.Domain}</td><td class="px-6 py-3 value">${item.Count}</td>`;
        });
    } else {
        hotDomainsTable.innerHTML = `<tr><td colspan="2" class="px-6 py-3" style="text-align:center;">${i18n.t('dashboard.noDomainData')}</td></tr>`;
    }

    // 被拦截域名
    const blockedDomainsTable = document.getElementById('blocked_domains_table').getElementsByTagName('tbody')[0];
    blockedDomainsTable.innerHTML = '';
    if (data.top_blocked_domains && data.top_blocked_domains.length > 0) {
        data.top_blocked_domains.forEach(item => {
            const row = blockedDomainsTable.insertRow();
            row.innerHTML = `<td class="px-6 py-3">${item.Domain}</td><td class="px-6 py-3 value">${item.Count}</td>`;
        });
    } else {
        blockedDomainsTable.innerHTML = `<tr><td colspan="2" class="px-6 py-3" style="text-align:center;">${i18n.t('dashboard.noBlockedDomainData')}</td></tr>`;
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

    // 渲染缓存命中率图表
    if (data.cache_hit_rate !== undefined) {
        window.Charts?.renderCacheHitRateChart('cache-hit-rate-chart', data.cache_hit_rate);
    }
}

function renderRecentQueries(data) {
    const recentQueriesList = document.getElementById('recent_queries_list');
    recentQueriesList.innerHTML = '';
    if (data && data.length > 0) {
        data.forEach(domain => {
            const div = document.createElement('div');
            div.textContent = domain;
            recentQueriesList.appendChild(div);
        });
    } else {
        recentQueriesList.innerHTML = `<div style="text-align:center;">${i18n.t('dashboard.noRecentQueries')}</div>`;
    }
}

function renderRecentlyBlocked(data) {
    const recentlyBlockedList = document.getElementById('recently_blocked_list');
    recentlyBlockedList.innerHTML = '';
    if (data && data.length > 0) {
        data.forEach(domain => {
            const div = document.createElement('div');
            div.textContent = domain;
            recentlyBlockedList.appendChild(div);
        });
    } else {
        recentlyBlockedList.innerHTML = `<div style="text-align:center;">${i18n.t('dashboard.noRecentlyBlocked')}</div>`;
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

// 当前请求的 AbortController
let currentAbortController = null;

async function updateDashboard() {
    const period = statsPeriodDays;
    
    // 取消之前的请求
    if (currentAbortController) {
        currentAbortController.abort();
    }
    currentAbortController = new AbortController();
    
    try {
        showLoadingState(); // 显示加载指示器
        
        const results = await Promise.allSettled([
            fetchWithRetry(`/api/stats?days=${period}`, { signal: currentAbortController.signal }).catch(e => ({ error: e })),
            fetchWithRetry(`/api/upstream-stats?days=${period}`, { signal: currentAbortController.signal }).catch(e => ({ error: e })),
            fetchWithRetry(`/api/recent-queries?days=${period}`, { signal: currentAbortController.signal }).catch(e => ({ error: e })),
            fetchWithRetry(`/api/recent-blocked?days=${period}`, { signal: currentAbortController.signal }).catch(e => ({ error: e }))
        ]);

        const [statsResult, upstreamResult, queriesResult, blockedResult] = results;
        
        // 提取成功的数据
        const statsData = statsResult.status === 'fulfilled' ? statsResult.value : null;
        const upstreamData = upstreamResult.status === 'fulfilled' ? upstreamResult.value : null;
        const queriesData = queriesResult.status === 'fulfilled' ? queriesResult.value : null;
        const blockedData = blockedResult.status === 'fulfilled' ? blockedResult.value : null;

        // 部分渲染：成功的数据正常显示，失败的部分显示错误
        renderDashboard(statsData, upstreamData, queriesData, blockedData);
        
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
        showErrorState(error.message); // 显示错误提示
    } finally {
        hideLoadingState(); // 隐藏加载指示器
    }
}

function initializeDashboardButtons() {
    // 初始化时间范围选择器
    initializeStatsSelectors();
    
    document.getElementById('clearCacheButton').addEventListener('click', () => {
        if (!confirm(i18n.t('messages.confirmClearCache'))) return;
        fetch('/api/cache/clear', { method: 'POST' })
            .then(response => {
                if (response.ok) {
                    alert(i18n.t('messages.cacheCleared'));
                    updateDashboard();
                } else {
                    alert(i18n.t('messages.cacheClearFailed'));
                }
            })
            .catch(error => alert(i18n.t('messages.cacheClearError')));
    });

    document.getElementById('clearStatsButton').addEventListener('click', () => {
        if (!confirm(i18n.t('messages.confirmClearStats'))) return;
        
        // 清除常规统计
        fetch('/api/stats/clear', { method: 'POST' })
            .then(response => {
                if (!response.ok) throw new Error('Failed to clear stats');
                // 清除上游服务器统计
                return fetch('/api/upstream-stats/clear', { method: 'POST' });
            })
            .then(response => {
                if (response.ok) {
                    alert(i18n.t('messages.statsCleared'));
                    updateDashboard();
                } else {
                    alert(i18n.t('messages.statsClearFailed'));
                }
            })
            .catch(error => {
                console.error('Error clearing stats:', error);
                alert(i18n.t('messages.statsClearError'));
            });
    });

    document.getElementById('refreshButton').addEventListener('click', () => {
        updateDashboard();
        updateAdBlockTab();
    });

    document.getElementById('restartButton').addEventListener('click', () => {
        if (!confirm(i18n.t('messages.restartConfirm'))) return;
        
        const currentConfig = originalConfig;
        if (currentConfig && Object.keys(currentConfig).length === 0) {
            alert('Configuration not loaded. Please refresh and try again.');
            return;
        }
        
        const performRestart = () => {
            fetch('/api/restart', { method: 'POST' })
                .then(response => {
                    if (response.ok) {
                        alert(i18n.t('messages.restarting'));
                        setTimeout(() => {
                            location.reload();
                        }, 5000);
                    } else {
                        response.json().then(data => {
                            alert(i18n.t('messages.restartFailed'));
                        }).catch(() => {
                            alert(i18n.t('messages.restartFailed'));
                        });
                    }
                })
                .catch(error => {
                    alert(i18n.t('messages.restartError', { error: error }));
                });
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
    initializeDashboardButtons();
    registerCleanup();
});

window.addEventListener('languageChanged', () => {
    // 应用翻译到所有 DOM 元素
    if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
        window.i18n.applyTranslations();
    }
    updateDashboard();
    if (!window.dashboardInterval) {
        window.dashboardInterval = setInterval(updateDashboard, 5000);
    }
});


function fetchRecentlyBlocked() {
    fetch('/api/recent-blocked')
        .then(response => response.ok ? response.json() : Promise.reject('Failed to load recently blocked'))
        .then(data => {
            const recentlyBlockedList = document.getElementById('recently_blocked_list');
            recentlyBlockedList.innerHTML = '';
            if (data && data.length > 0) {
                data.forEach(domain => {
                    const div = document.createElement('div');
                    div.textContent = domain;
                    recentlyBlockedList.appendChild(div);
                });
            } else {
                recentlyBlockedList.innerHTML = `<div style="text-align:center;">${i18n.t('dashboard.noRecentlyBlocked')}</div>`;
            }
        })
        .catch(error => {
            const recentlyBlockedList = document.getElementById('recently_blocked_list');
            recentlyBlockedList.innerHTML = `<div style="text-align:center; color: red;">${i18n.t('dashboard.errorLoadingData')}</div>`;
        });
}



// 获取上游服务器详细状态
// 使用标志防止并发请求和竞态条件
let upstreamStatsLoading = false;
let upstreamStatsAbortController = null;

// 获取上游服务器详细状态
// 使用标志防止并发请求和竞态条件
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

function fetchUpstreamStats() {
    // 防止并发请求
    if (upstreamStatsLoading) {
        return;
    }
    
    // 取消之前的请求（如果有）
    if (upstreamStatsAbortController) {
        upstreamStatsAbortController.abort();
    }
    
    upstreamStatsAbortController = new AbortController();
    upstreamStatsLoading = true;
    
    // 使用全局统一的 statsPeriodDays
    fetch(`/api/upstream-stats?days=${statsPeriodDays}`, { signal: upstreamStatsAbortController.signal })
        .then(response => {
            if (!response.ok) throw new Error('Failed to fetch upstream stats');
            return response.json();
        })
        .then(data => {
            if (data && data.data && data.data.servers) {
                renderEnhancedUpstreamTable(data.data.servers);
            } else {
                showUpstreamLoadError();
            }
        })
        .catch(error => {
            // 忽略被中止的请求
            if (error.name === 'AbortError') {
                return;
            }
            showUpstreamLoadError();
        })
        .finally(() => {
            upstreamStatsLoading = false;
        });
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

// 创建服务器行
function createServerRow(server) {
    const row = document.createElement('tr');
    row.className = 'divide-y divide-[#e9e8ce] dark:divide-[#3a3922]';
    row.dataset.serverAddress = server.address; // 添加唯一标识
    row.innerHTML = `
        <td class="px-6 py-3 font-medium">${server.address}</td>
        <td class="px-6 py-3">${getProtocolBadge(server.protocol)}</td>
        <td class="px-6 py-3">
            <div class="flex items-center gap-2">
                <div class="w-20 bg-gray-200 rounded-full h-2">
                    <div class="h-2 rounded-full ${getRateColor(server.success_rate)}" style="width: ${server.success_rate}%"></div>
                </div>
                <span class="text-sm font-medium">${server.success_rate.toFixed(1)}%</span>
            </div>
        </td>
        <td class="px-6 py-3">${getStatusIcon(server.status)} ${getStatusText(server.status)}</td>
        <td class="px-6 py-3 ${getLatencyClass(server.latency_ms)}">${server.latency_ms.toFixed(1)} ms</td>
        <td class="px-6 py-3 text-gray-500">${server.total}</td>
        <td class="px-6 py-3 text-green-600">${server.success}</td>
        <td class="px-6 py-3 text-red-600">${server.failure}</td>
    `;
    
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
        
        // 更新状态
        cells[3].innerHTML = `${getStatusIcon(server.status)} ${getStatusText(server.status)}`;
        
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

// 显示加载失败提示
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
        
        tbody.innerHTML = `
            <tr>
                <td colspan="8" class="px-6 py-4 text-center text-red-600">
                    ${errorMsg}
                </td>
            </tr>
        `;
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
