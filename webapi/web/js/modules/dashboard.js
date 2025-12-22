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
            document.getElementById('cache_hit_rate').textContent = (data.cache_hit_rate || 0).toFixed(2) + '%';
            document.getElementById('upstream_failures').textContent = data.upstream_failures || 0;
            if (data.system_stats) {
                const sys = data.system_stats;
                document.getElementById('cpu_usage_pct').textContent = (sys.cpu_usage_pct || 0).toFixed(1) + '%';
                document.getElementById('mem_usage_pct').textContent = (sys.mem_usage_pct || 0).toFixed(1) + '%';
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

                memoryUsageBar.style.width = `${mem.memory_percent.toFixed(2)}%`;
                memoryUsageText.textContent = `${mem.current_memory_mb} MB / ${mem.max_memory_mb} MB`;
                cacheEntries.textContent = `${mem.current_entries.toLocaleString()} / ${mem.max_entries.toLocaleString()}`;
                expiredEntries.textContent = `${mem.expired_entries.toLocaleString()} (${(mem.expired_percent || 0).toFixed(1)}%)`;
                protectedEntries.textContent = mem.protected_entries.toLocaleString();
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
