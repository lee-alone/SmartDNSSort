// Dashboard / Stats Logic Module

const API_URL = '/api/stats';

function updateDashboard() {
    // Fetch main stats and hot domains
    fetch(API_URL)
        .then(response => {
            if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);
            return response.json();
        })
        .then(data => {
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
            if (data.upstream_stats) {
                const upstreamTable = document.getElementById('upstream_stats').getElementsByTagName('tbody')[0];
                upstreamTable.innerHTML = '';
                const servers = Object.keys(data.upstream_stats).sort();
                servers.forEach(server => {
                    const stats = data.upstream_stats[server];
                    const row = upstreamTable.insertRow();
                    row.innerHTML = `<td class="px-6 py-3">${server}</td><td class="px-6 py-3 value">${stats.success || 0}</td><td class="px-6 py-3 value">${stats.failure || 0}</td>`;
                });
            }
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

            // Render blocked domains
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
        })
        .catch(error => {
            console.error('Error fetching stats:', error);
            const statusEl = document.getElementById('status');
            const statusText = statusEl.querySelector('.status-text');
            if (statusText) statusText.textContent = i18n.t('status.error');
            statusEl.className = 'status-indicator error';
        });

    // Fetch upstream server stats
    fetchUpstreamStats();

    // Fetch recent queries
    fetch('/api/recent-queries')
        .then(response => response.ok ? response.json() : Promise.reject('Failed to load recent queries'))
        .then(data => {
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
        })
        .catch(error => {
            console.error('Error fetching recent queries:', error);
            const recentQueriesList = document.getElementById('recent_queries_list');
            recentQueriesList.innerHTML = `<div style="text-align:center; color: red;">${i18n.t('dashboard.errorLoadingData')}</div>`;
        });

    // Fetch recently blocked domains
    fetchRecentlyBlocked();
}

function initializeDashboardButtons() {
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
        fetch('/api/stats/clear', { method: 'POST' })
            .then(response => {
                if (response.ok) {
                    alert(i18n.t('messages.statsCleared'));
                    updateDashboard();
                } else {
                    alert(i18n.t('messages.statsClearFailed'));
                }
            })
            .catch(error => alert(i18n.t('messages.statsClearError')));
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
            console.log('[DEBUG] Calling restart API');
            fetch('/api/restart', { method: 'POST' })
                .then(response => {
                    console.log('[DEBUG] Restart API response status:', response.status);
                    if (response.ok) {
                        alert(i18n.t('messages.restarting'));
                        setTimeout(() => {
                            location.reload();
                        }, 5000);
                    } else {
                        response.json().then(data => {
                            alert(i18n.t('messages.restartFailed'));
                            console.error('Restart failed:', data);
                        }).catch(() => {
                            alert(i18n.t('messages.restartFailed'));
                        });
                    }
                })
                .catch(error => {
                    alert(i18n.t('messages.restartError', { error: error }));
                    console.error('Restart error:', error);
                });
        };
        
        const form = document.getElementById('configForm');
        if (form && form.style.display !== 'none') {
            if (confirm('Do you want to save the current configuration changes before restarting?')) {
                console.log('[DEBUG] Saving config before restart');
                saveConfig()
                    .then(() => {
                        console.log('[DEBUG] Config saved successfully, performing restart');
                        setTimeout(performRestart, 500);
                    })
                    .catch(error => {
                        console.error('[DEBUG] Config save failed, aborting restart:', error);
                    });
            } else {
                performRestart();
            }
        } else {
            performRestart();
        }
    });
}

document.addEventListener('componentsLoaded', initializeDashboardButtons);

window.addEventListener('languageChanged', () => {
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
            console.error('Error fetching recently blocked:', error);
            const recentlyBlockedList = document.getElementById('recently_blocked_list');
            recentlyBlockedList.innerHTML = `<div style="text-align:center; color: red;">${i18n.t('dashboard.errorLoadingData')}</div>`;
        });
}



// 获取上游服务器详细状态
function fetchUpstreamStats() {
    fetch('/api/upstream-stats')
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
            console.error('Error fetching upstream stats:', error);
            showUpstreamLoadError();
        });
}

// 渲染增强的上游表格
function renderEnhancedUpstreamTable(upstreamData) {
    const tbody = document.getElementById('upstream_stats')?.getElementsByTagName('tbody')[0];
    if (!tbody) return;
    
    tbody.innerHTML = '';
    
    upstreamData.forEach(server => {
        const row = tbody.insertRow();
        
        // 成功率进度条颜色
        const rateColor = getRateColor(server.success_rate);
        
        // 健康状态图标
        const statusIcon = getStatusIcon(server.status);
        
        // 延迟状态
        const latencyClass = getLatencyClass(server.latency_ms);
        
        row.innerHTML = `
            <td class="px-6 py-3 font-medium">${server.address}</td>
            <td class="px-6 py-3">${getProtocolBadge(server.protocol)}</td>
            <td class="px-6 py-3">
                <div class="flex items-center gap-2">
                    <div class="w-20 bg-gray-200 rounded-full h-2">
                        <div class="h-2 rounded-full ${rateColor}" style="width: ${server.success_rate}%"></div>
                    </div>
                    <span class="text-sm font-medium">${server.success_rate.toFixed(1)}%</span>
                </div>
            </td>
            <td class="px-6 py-3">${statusIcon} ${server.status}</td>
            <td class="px-6 py-3 ${latencyClass}">${server.latency_ms.toFixed(1)} ms</td>
            <td class="px-6 py-3 text-gray-500">${server.total}</td>
            <td class="px-6 py-3 text-green-600">${server.success}</td>
            <td class="px-6 py-3 text-red-600">${server.failure}</td>
        `;
    });
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
                console.warn('i18n translation failed, using default message:', e);
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

// 获取健康状态图标
function getStatusIcon(status) {
    switch(status) {
        case 'healthy': return '🟢';
        case 'degraded': return '🟡';
        case 'unhealthy': return '🔴';
        default: return '⚪';
    }
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
