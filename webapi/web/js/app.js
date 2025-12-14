let originalConfig = {}; // Store the original config to preserve uneditable fields

// --- Tab Management ---
function showTab(tabName) {
    document.querySelectorAll('.tab-content').forEach(tab => tab.classList.remove('active'));
    document.querySelectorAll('.tab-button').forEach(btn => btn.classList.remove('active'));
    document.getElementById(tabName).classList.add('active');
    document.querySelector(`.tab-button[onclick="showTab('${tabName}')"]`).classList.add('active');
}

// --- Dashboard / Stats Logic ---
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
                    row.innerHTML = `<td>${server}</td><td class="value">${stats.success || 0}</td><td class="value">${stats.failure || 0}</td>`;
                });
            }
            const hotDomainsTable = document.getElementById('hot_domains_table').getElementsByTagName('tbody')[0];
            hotDomainsTable.innerHTML = '';
            if (data.top_domains && data.top_domains.length > 0) {
                data.top_domains.forEach(item => {
                    const row = hotDomainsTable.insertRow();
                    row.innerHTML = `<td>${item.Domain}</td><td class="value">${item.Count}</td>`;
                });
            } else {
                hotDomainsTable.innerHTML = `<tr><td colspan="2" style="text-align:center;">${i18n.t('dashboard.noDomainData')}</td></tr>`;
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
}

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
                updateDashboard(); // Refresh stats to show zeros
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
    fetch('/api/restart', { method: 'POST' })
        .then(response => {
            if (response.ok) {
                alert(i18n.t('messages.restarting'));
                // 等待5秒后自动刷新页面
                setTimeout(() => {
                    location.reload();
                }, 5000);
            } else {
                alert(i18n.t('messages.restartFailed'));
            }
        })
        .catch(error => {
            alert(i18n.t('messages.restartError', { error: error }));
            console.error('Restart error:', error);
        });
});


// --- Configuration Logic ---
const CONFIG_API_URL = '/api/config';

function populateForm(config) {
    try {
        originalConfig = config; // Save the original config

        const setValue = (id, value) => {
            const el = document.getElementById(id);
            if (el) el.value = value;
            else console.error(`Form element with ID not found: ${id}`);
        };
        const setChecked = (id, checked) => {
            const el = document.getElementById(id);
            if (el) el.checked = checked;
            else console.error(`Form element with ID not found: ${id}`);
        };

        // Use helpers
        setValue('dns.listen_port', config.dns.listen_port);
        setChecked('dns.enable_tcp', config.dns.enable_tcp);
        setChecked('dns.enable_ipv6', config.dns.enable_ipv6);
        setValue('upstream.strategy', config.upstream.strategy);
        setValue('upstream.timeout_ms', config.upstream.timeout_ms);
        setValue('upstream.concurrency', config.upstream.concurrency);
        setValue('upstream.sequential_timeout', config.upstream.sequential_timeout || 300);
        setValue('upstream.racing_delay', config.upstream.racing_delay || 100);
        setValue('upstream.racing_max_concurrent', config.upstream.racing_max_concurrent || 2);
        setChecked('upstream.nxdomain_for_errors', config.upstream.nxdomain_for_errors);
        
        // Health Check settings
        if (config.upstream.health_check) {
            setChecked('upstream.health_check.enabled', config.upstream.health_check.enabled);
            setValue('upstream.health_check.failure_threshold', config.upstream.health_check.failure_threshold || 3);
            setValue('upstream.health_check.circuit_breaker_threshold', config.upstream.health_check.circuit_breaker_threshold || 5);
            setValue('upstream.health_check.circuit_breaker_timeout', config.upstream.health_check.circuit_breaker_timeout || 30);
            setValue('upstream.health_check.success_threshold', config.upstream.health_check.success_threshold || 2);
        }
        
        setChecked('ping.enabled', config.ping.enabled); // Add this line
        setValue('ping.count', config.ping.count);
        setValue('ping.timeout_ms', config.ping.timeout_ms);
        setValue('ping.concurrency', config.ping.concurrency);
        setValue('ping.strategy', config.ping.strategy);
        setValue('ping.max_test_ips', config.ping.max_test_ips);
        setValue('ping.rtt_cache_ttl_seconds', config.ping.rtt_cache_ttl_seconds);
        setChecked('ping.enable_http_fallback', config.ping.enable_http_fallback);

        // UI/UX logic: Toggle other ping settings based on ping.enabled switch
        togglePingSettingsState();
        document.getElementById('ping.enabled').addEventListener('change', togglePingSettingsState);
        setValue('cache.fast_response_ttl', config.cache.fast_response_ttl);
        setValue('cache.user_return_ttl', config.cache.user_return_ttl);
        setValue('cache.min_ttl_seconds', config.cache.min_ttl_seconds);
        setValue('cache.max_ttl_seconds', config.cache.max_ttl_seconds);
        setValue('cache.negative_ttl_seconds', config.cache.negative_ttl_seconds);
        setValue('cache.error_cache_ttl_seconds', config.cache.error_cache_ttl_seconds);

        // Advanced Cache Settings
        setValue('cache.max_memory_mb', config.cache.max_memory_mb);
        setValue('cache.eviction_threshold', config.cache.eviction_threshold);
        setValue('cache.eviction_batch_percent', config.cache.eviction_batch_percent);
        setChecked('cache.keep_expired_entries', config.cache.keep_expired_entries);
        setChecked('cache.protect_prefetch_domains', config.cache.protect_prefetch_domains);
        setValue('cache.save_to_disk_interval_minutes', config.cache.save_to_disk_interval_minutes || 60);

        setChecked('prefetch.enabled', config.prefetch.enabled);
        setChecked('webui.enabled', config.webui.enabled);
        setValue('webui.listen_port', config.webui.listen_port);
        setValue('system.max_cpu_cores', config.system.max_cpu_cores);
        setValue('system.sort_queue_workers', config.system.sort_queue_workers);
        setValue('system.refresh_workers', config.system.refresh_workers);

        // Array values
        setValue('upstream.servers', (config.upstream.servers || []).join('\n'));
        setValue('upstream.bootstrap_dns', (config.upstream.bootstrap_dns || []).join('\n'));
    } catch (e) {
        console.error("Error inside populateForm:", e);
        alert("An error occurred while displaying the configuration. Check developer console (F12).");
    }
}

function loadConfig() {
    fetch(CONFIG_API_URL)
        .then(response => {
            if (!response.ok) {
                throw new Error(`HTTP error! Status: ${response.status}`);
            }
            return response.json();
        })
        .then(populateForm)
        .catch(error => {
            console.error('Could not load or process configuration:', error);
            alert('Could not load configuration from server. Please open the browser developer console (F12) and check for errors.');
        });
}

function saveConfig() {
    const form = document.getElementById('configForm');

    // Deep copy the original config to preserve uneditable fields like adblock
    const data = JSON.parse(JSON.stringify(originalConfig));

    // Log the values being sent for debugging
    console.log('[DEBUG] Form values before sending:');
    console.log('  fast_response_ttl:', form.elements['cache.fast_response_ttl'].value);
    console.log('  user_return_ttl:', form.elements['cache.user_return_ttl'].value);

    // Overwrite with form values
    data.dns = {
        listen_port: parseInt(form.elements['dns.listen_port'].value),
        enable_tcp: form.elements['dns.enable_tcp'].checked,
        enable_ipv6: form.elements['dns.enable_ipv6'].checked,
    };
    data.upstream = {
        servers: form.elements['upstream.servers'].value.split('\n').filter(s => s.trim() !== ''),
        bootstrap_dns: form.elements['upstream.bootstrap_dns'].value.split('\n').filter(s => s.trim() !== ''),
        strategy: form.elements['upstream.strategy'].value,
        timeout_ms: parseInt(form.elements['upstream.timeout_ms'].value),
        concurrency: parseInt(form.elements['upstream.concurrency'].value),
        sequential_timeout: parseInt(form.elements['upstream.sequential_timeout'].value) || 300,
        racing_delay: parseInt(form.elements['upstream.racing_delay'].value) || 100,
        racing_max_concurrent: parseInt(form.elements['upstream.racing_max_concurrent'].value) || 2,
        nxdomain_for_errors: form.elements['upstream.nxdomain_for_errors'].checked,
        health_check: {
            enabled: form.elements['upstream.health_check.enabled'].checked,
            failure_threshold: parseInt(form.elements['upstream.health_check.failure_threshold'].value) || 3,
            circuit_breaker_threshold: parseInt(form.elements['upstream.health_check.circuit_breaker_threshold'].value) || 5,
            circuit_breaker_timeout: parseInt(form.elements['upstream.health_check.circuit_breaker_timeout'].value) || 30,
            success_threshold: parseInt(form.elements['upstream.health_check.success_threshold'].value) || 2
        }
    };
    data.ping = {
        enabled: form.elements['ping.enabled'].checked, // Add this line
        count: parseInt(form.elements['ping.count'].value),
        timeout_ms: parseInt(form.elements['ping.timeout_ms'].value),
        concurrency: parseInt(form.elements['ping.concurrency'].value),
        strategy: form.elements['ping.strategy'].value,
        max_test_ips: parseInt(form.elements['ping.max_test_ips'].value),
        rtt_cache_ttl_seconds: parseInt(form.elements['ping.rtt_cache_ttl_seconds'].value),
        enable_http_fallback: form.elements['ping.enable_http_fallback'].checked,
    };
    data.cache = {
        fast_response_ttl: parseInt(form.elements['cache.fast_response_ttl'].value),
        user_return_ttl: parseInt(form.elements['cache.user_return_ttl'].value),
        min_ttl_seconds: parseInt(form.elements['cache.min_ttl_seconds'].value),
        max_ttl_seconds: parseInt(form.elements['cache.max_ttl_seconds'].value),
        negative_ttl_seconds: parseInt(form.elements['cache.negative_ttl_seconds'].value),
        error_cache_ttl_seconds: parseInt(form.elements['cache.error_cache_ttl_seconds'].value),
        // Memory cache settings
        max_memory_mb: parseInt(form.elements['cache.max_memory_mb'].value),
        eviction_threshold: parseFloat(form.elements['cache.eviction_threshold'].value),
        eviction_batch_percent: parseFloat(form.elements['cache.eviction_batch_percent'].value),
        keep_expired_entries: form.elements['cache.keep_expired_entries'].checked,
        protect_prefetch_domains: form.elements['cache.protect_prefetch_domains'].checked,
        save_to_disk_interval_minutes: parseInt(form.elements['cache.save_to_disk_interval_minutes'].value) || 60
    };
    data.prefetch = {
        enabled: form.elements['prefetch.enabled'].checked,
    };
    data.webui = {
        enabled: form.elements['webui.enabled'].checked,
        listen_port: parseInt(form.elements['webui.listen_port'].value),
    };
    data.system = {
        max_cpu_cores: parseInt(form.elements['system.max_cpu_cores'].value),
        sort_queue_workers: parseInt(form.elements['system.sort_queue_workers'].value),
        refresh_workers: parseInt(form.elements['system.refresh_workers'].value),
    };

    // Log the data being sent to the server
    console.log('[DEBUG] Data being sent to server:', JSON.stringify(data, null, 2));
    console.log('[DEBUG] Cache config being sent:', data.cache);

    fetch(CONFIG_API_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
    })
        .then(response => {
            if (response.ok) {
                alert(i18n.t('messages.configSaved'));
            } else {
                response.text().then(text => alert(i18n.t('messages.configSaveError', { error: text })));
            }
        })
        .catch(error => {
            console.error('Error saving config:', error);
            alert(i18n.t('messages.configSaveErrorGeneric'));
        });
}

// Initial Load
// updateDashboard(); // Called by i18n.init() or manually after i18n load?
// loadConfig(); // This can be called immediately as it doesn't depend on i18n for values, only labels which are static HTML
// updateAdBlockTab();

// We should wait for i18n to be ready before rendering dynamic content that needs translation.
// i18n.js dispatches 'languageChanged' event, but also we can just init everything after DOMContentLoaded which i18n also hooks into.
// To be safe, let's wrap initial calls or let i18n trigger them?
// Actually, i18n.js runs init() on DOMContentLoaded.
// We can listen to a custom event or just run these.
// Since i18n.init() is async, we might have a race condition if we run updateDashboard immediately.
// Let's modify i18n.js to dispatch an event when ready, or just use a simple timeout/callback.
// Or better, just call these functions. updateDashboard uses i18n.t, so i18n must be loaded.
// i18n.init() awaits loadTranslations.
// Let's add a listener for 'languageChanged' which is fired after setLanguage (and we can fire it after init too).

document.addEventListener('DOMContentLoaded', () => {
    // i18n.init() is called in i18n.js on DOMContentLoaded as well.
    // We can't guarantee order.
    // Let's rely on i18n.js exposing a promise or callback?
    // Or just wait for a bit?
    // The cleanest way is to have i18n.js call a callback or dispatch an event "i18nReady".

    // For now, let's assume i18n.js is loaded before app.js (it is in HTML).
    // But i18n.init() is async.

    // Let's modify app.js to wait for i18n.
    // But i18n.js as written above auto-inits on DOMContentLoaded.

    // I'll add a check.
});

// Actually, let's just hook into the window event if we can, or just poll.
// But wait, I can modify i18n.js to dispatch "i18nReady".
// Let's assume I'll modify i18n.js slightly to dispatch "i18nReady" after init.
// But I can't modify i18n.js easily now without another tool call.
// Let's look at i18n.js again.
// It dispatches 'languageChanged' in setLanguage.
// It DOES NOT dispatch anything in init().
// I should probably modify i18n.js to dispatch 'i18nReady' or 'languageChanged' in init().

// However, for now, I will just wrap the init calls in a function and export it, 
// and maybe call it from the bottom of i18n.js? No, that's circular.

// Let's just use a simple polling or event listener if I modify i18n.js.
// Since I have to modify app.js anyway, I can just put the init calls inside a function `initializeApp()`
// and assign it to `window.initializeApp`. Then in `i18n.js`, after init, call `window.initializeApp()` if it exists.
// That seems coupled.

// Better: Listen for `languageChanged`.
// In i18n.js:
// async init() { ... await this.loadTranslations(...); this.applyTranslations(); ... window.dispatchEvent(new CustomEvent('languageChanged', ...)); }
// I should update i18n.js to dispatch the event in init() too.

// Let's update app.js first.

window.addEventListener('languageChanged', (e) => {
    updateDashboard();
    updateAdBlockTab();
    loadCustomSettings();
    initializeCounters();
    // loadConfig doesn't need i18n for the form values themselves, but if we had dynamic labels it would.
    // The labels are static HTML handled by applyTranslations().
    // So loadConfig is fine to run whenever.
});

// Also run once on start?
// If i18n emits languageChanged on init, then yes.
// If not, we need to know when it's ready.

// I will modify i18n.js to dispatch 'languageChanged' at the end of init().
// And I will remove the auto-execution of updateDashboard() etc from global scope in app.js
// and instead put them in the event listener.

loadConfig(); // This is safe to run immediately as it just fetches values.

// --- Strategy Selection UI Logic ---
function updateStrategyUI() {
    const strategySelect = document.getElementById('upstream.strategy');
    const sequentialParams = document.getElementById('sequential-params');
    const racingParams = document.getElementById('racing-params');
    const racingParamsConcurrent = document.getElementById('racing-params-concurrent');
    
    const strategy = strategySelect.value;
    
    // Show/hide params based on selected strategy
    if (sequentialParams) sequentialParams.style.display = strategy === 'sequential' ? 'block' : 'none';
    if (racingParams) racingParams.style.display = strategy === 'racing' ? 'block' : 'none';
    if (racingParamsConcurrent) racingParamsConcurrent.style.display = strategy === 'racing' ? 'block' : 'none';
}

// Add event listener for strategy selection
document.addEventListener('DOMContentLoaded', () => {
    const strategySelect = document.getElementById('upstream.strategy');
    if (strategySelect) {
        strategySelect.addEventListener('change', updateStrategyUI);
        // Initial call to set the UI state
        setTimeout(() => {
            updateStrategyUI();
        }, 100);
    }
});

// Also update when config is loaded
window.addEventListener('languageChanged', updateStrategyUI, false);

// --- AdBlock Tab Logic ---

function loadAdBlockSettings() {
    fetch('/api/adblock/settings')
        .then(response => response.ok ? response.json() : Promise.reject('Failed to load AdBlock settings'))
        .then(response => {
            const settings = response.data;
            document.getElementById('adblock_update_interval_hours').value = settings.update_interval_hours;
            document.getElementById('adblock_max_cache_age_hours').value = settings.max_cache_age_hours;
            document.getElementById('adblock_max_cache_size_mb').value = settings.max_cache_size_mb;
            document.getElementById('adblock_blocked_ttl').value = settings.blocked_ttl;
        })
        .catch(error => console.error('Error fetching AdBlock settings:', error));
}

document.getElementById('adblockSaveSettingsButton').addEventListener('click', function (e) {
    e.preventDefault();

    const payload = {
        update_interval_hours: parseInt(document.getElementById('adblock_update_interval_hours').value, 10),
        max_cache_age_hours: parseInt(document.getElementById('adblock_max_cache_age_hours').value, 10),
        max_cache_size_mb: parseInt(document.getElementById('adblock_max_cache_size_mb').value, 10),
        blocked_ttl: parseInt(document.getElementById('adblock_blocked_ttl').value, 10),
    };

    fetch('/api/adblock/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
    })
        .then(response => {
            if (response.ok) {
                alert(i18n.t('messages.adblockSettingsSaved'));
            } else {
                response.json().then(data => alert(i18n.t('messages.adblockSettingsSaveError', { error: data.message })));
            }
        })
        .catch(error => {
            console.error('Error saving AdBlock settings:', error);
            alert(i18n.t('messages.adblockSettingsSaveErrorGeneric'));
        });
});

function updateAdBlockTab() {
    // Fetch AdBlock status
    fetch('/api/adblock/status')
        .then(response => response.ok ? response.json() : Promise.reject('Failed to load AdBlock status'))
        .then(response => {
            const data = response.data;
            if (!data.enabled) {
                document.getElementById('adblock_status').textContent = i18n.t('adblock.statusDisabled');
                document.getElementById('adblock_status').className = 'value status-error';
                return; // Stop if not enabled
            }
            document.getElementById('adblock_status').textContent = i18n.t('adblock.statusEnabled');
            document.getElementById('adblock_status').className = 'value status-success';

            // Set toggle switch state
            const toggle = document.getElementById('adblock_enable_toggle');
            if (toggle) {
                toggle.checked = data.enabled;
                toggle.disabled = false;
            }
            document.getElementById('adblock_engine').textContent = data.engine;
            document.getElementById('adblock_total_rules').textContent = (data.total_rules || 0).toLocaleString();
            document.getElementById('adblock_blocked_today').textContent = (data.blocked_today || 0).toLocaleString();
            document.getElementById('adblock_blocked_total').textContent = (data.blocked_total || 0).toLocaleString();
            document.getElementById('adblock_last_update').textContent = data.last_update ? new Date(data.last_update).toLocaleString() : 'Never';
        })
        .catch(error => {
            console.error('Error fetching AdBlock status:', error);
            document.getElementById('adblock_status').textContent = i18n.t('adblock.statusError');
            document.getElementById('adblock_status').className = 'value status-error';
        });

    // Load BlockMode from config
    fetch('/api/config')
        .then(response => response.ok ? response.json() : Promise.reject('Failed to load config'))
        .then(config => {
            const blockMode = config.adblock.block_mode || 'nxdomain';
            const blockModeSelect = document.getElementById('adblock_block_mode');
            if (blockModeSelect) {
                blockModeSelect.value = blockMode;
            }
        })
        .catch(error => {
            console.error('Error loading BlockMode:', error);
        });

    // Fetch AdBlock sources
    fetch('/api/adblock/sources')
        .then(response => response.ok ? response.json() : Promise.reject('Failed to load AdBlock sources'))
        .then(response => {
            const sources = response.data;
            const sourcesTable = document.getElementById('adblock_sources_table').getElementsByTagName('tbody')[0];
            sourcesTable.innerHTML = ''; // Clear existing rows
            if (sources && sources.length > 0) {
                sources.forEach(source => {
                    const row = sourcesTable.insertRow();
                    let statusClass = 'status-active';
                    if (source.status === 'failed') statusClass = 'status-warning';
                    if (source.status === 'bad') statusClass = 'status-error';

                    row.innerHTML = `
                        <td class="adblock-url">${source.url}</td>
                        <td><span class="status-indicator ${statusClass}">${source.status}</span></td>
                        <td class="value">${source.rule_count || 0}</td>
                        <td>${source.last_update ? new Date(source.last_update).toLocaleString() : 'Never'}</td>
                        <td><input type="checkbox" class="source-enable-toggle" data-url="${source.url}" ${source.enabled ? 'checked' : ''}></td>
                        <td><button class="btn btn-danger btn-sm" data-url="${source.url}">Delete</button></td>
                    `;
                });
                document.querySelectorAll('.source-enable-toggle').forEach(cb => {
                    cb.addEventListener('change', (e) => {
                        const url = e.target.dataset.url;
                        const enabled = e.target.checked;
                        fetch('/api/adblock/sources', {
                            method: 'PUT',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ url: url, enabled: enabled })
                        }).then(response => {
                            if (!response.ok) {
                                e.target.checked = !enabled;
                                response.text().then(text => alert(i18n.t('messages.adblockSourceAddError', { error: text }))); // Reusing error msg
                            }
                        }).catch(err => {
                            e.target.checked = !enabled;
                            alert(i18n.t('messages.adblockSourceAddError', { error: err }));
                        });
                    });
                });
            } else {
                sourcesTable.innerHTML = `<tr><td colspan="6" style="text-align:center;">${i18n.t('adblock.noSources')}</td></tr>`;
            }
        })
        .catch(error => {
            console.error('Error fetching AdBlock sources:', error);
            const sourcesTable = document.getElementById('adblock_sources_table').getElementsByTagName('tbody')[0];
            sourcesTable.innerHTML = `<tr><td colspan="5" style="text-align:center; color: red;">${i18n.t('adblock.errorLoadingSources')}</td></tr>`;
        });

    loadAdBlockSettings();
}

// AdBlock Enable/Disable Toggle Event Handler
document.getElementById('adblock_enable_toggle').addEventListener('change', function (e) {
    const enabled = e.target.checked;

    fetch('/api/adblock/toggle', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: enabled })
    })
        .then(response => {
            if (response.ok) {
                // Update status text
                const statusEl = document.getElementById('adblock_status');
                statusEl.textContent = enabled ? i18n.t('adblock.statusEnabled') : i18n.t('adblock.statusDisabled');
                statusEl.className = enabled ? 'value status-success' : 'value status-error';
                console.log('AdBlock status changed to:', enabled);
            } else {
                // Revert toggle on failure
                e.target.checked = !enabled;
                response.text().then(text => alert(i18n.t('messages.adblockToggleError', { error: text })));
            }
        })
        .catch(error => {
            // Revert toggle on error
            e.target.checked = !enabled;
            alert(i18n.t('messages.adblockToggleError', { error: error }));
        });
});

document.getElementById('adblockUpdateRulesButton').addEventListener('click', () => {
    if (!confirm(i18n.t('messages.adblockUpdateConfirm'))) return;
    fetch('/api/adblock/update', { method: 'POST' })
        .then(response => {
            if (response.ok) {
                alert(i18n.t('messages.adblockUpdateStarted'));
            } else {
                response.text().then(text => alert(i18n.t('messages.adblockUpdateFailed', { error: text })));
            }
        })
        .catch(error => alert(i18n.t('messages.adblockUpdateError', { error: error })));
});

document.getElementById('adblockAddSourceButton').addEventListener('click', () => {
    const urlInput = document.getElementById('adblock_new_source_url');
    const url = urlInput.value.trim();
    if (!url) {
        alert(i18n.t('messages.enterUrl'));
        return;
    }
    fetch('/api/adblock/sources', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: url })
    })
        .then(response => {
            if (response.ok) {
                alert(i18n.t('messages.adblockSourceAdded'));
                urlInput.value = '';
                updateAdBlockTab(); // Refresh the list
            } else {
                response.json().then(data => alert(i18n.t('messages.adblockSourceAddError', { error: data.message })));
            }
        })
        .catch(error => alert(i18n.t('messages.adblockSourceAddError', { error: error })));
});

// Event delegation for delete buttons
document.getElementById('adblock_sources_table').addEventListener('click', function (e) {
    if (e.target && e.target.matches('button.btn-danger')) {
        const url = e.target.dataset.url;
        if (!confirm(i18n.t('messages.deleteConfirm') + '\n' + url)) return;

        fetch('/api/adblock/sources', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url: url })
        })
            .then(response => {
                if (response.ok) {
                    alert(i18n.t('messages.adblockSourceDeleted'));
                    updateAdBlockTab(); // Refresh the list
                } else {
                    response.json().then(data => alert(i18n.t('messages.adblockSourceDeleteError', { error: data.message })));
                }
            })
            .catch(error => alert(i18n.t('messages.adblockSourceDeleteError', { error: error })));
    }
});

document.getElementById('adblockTestDomainButton').addEventListener('click', () => {
    const domainInput = document.getElementById('adblock_test_domain');
    const domain = domainInput.value.trim();
    const resultDiv = document.getElementById('adblock_test_result');

    if (!domain) {
        resultDiv.textContent = i18n.t('messages.enterDomain');
        resultDiv.className = 'test-result status-error';
        return;
    }

    resultDiv.textContent = i18n.t('messages.testing');
    resultDiv.className = 'test-result';

    fetch('/api/adblock/test', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ domain: domain })
    })
        .then(response => response.ok ? response.json() : Promise.reject('Test request failed'))
        .then(response => {
            const result = response.data;
            if (result.blocked) {
                resultDiv.innerHTML = `<strong>${i18n.t('messages.blocked')}</strong><br>${i18n.t('messages.rule')}: <code>${result.rule}</code>`;
                resultDiv.className = 'test-result status-error';
            } else {
                resultDiv.textContent = i18n.t('messages.notBlocked');
                resultDiv.className = 'test-result status-success';
            }
        })
        .catch(error => {
            resultDiv.textContent = i18n.t('messages.testError');
            resultDiv.className = 'test-result status-error';
            console.error('Test domain error:', error);
        });
});


// AdBlock BlockMode Save Button Event Handler
document.getElementById('adblockSaveBlockModeButton').addEventListener('click', () => {
    const blockModeSelect = document.getElementById('adblock_block_mode');
    const blockMode = blockModeSelect.value;

    if (!blockMode) {
        alert(i18n.t('messages.selectBlockMode'));
        return;
    }

    fetch('/api/adblock/blockmode', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ block_mode: blockMode })
    })
        .then(response => {
            if (response.ok) {
                alert(i18n.t('messages.adblockBlockModeSaved'));
            } else {
                response.text().then(text => alert(i18n.t('messages.adblockBlockModeSaveError', { error: text })));
            }
        })
        .catch(error => {
            alert(i18n.t('messages.adblockBlockModeSaveError', { error: error }));
            console.error('Save block mode error:', error);
        });
});

// ========== Custom Settings Logic ==========

// Update character and line counters
function updateCounter(textareaId, lineCountId, charCountId) {
    const textarea = document.getElementById(textareaId);
    const content = textarea.value;
    const lines = content ? content.split('\n').length : 0;
    const chars = content.length;

    document.getElementById(lineCountId).textContent = `${lines} line${lines !== 1 ? 's' : ''}`;
    document.getElementById(charCountId).textContent = `${chars} character${chars !== 1 ? 's' : ''}`;
}

// Initialize counters on load
function initializeCounters() {
    // Blocked domains counter
    const blockedTextarea = document.getElementById('custom-blocked-content');
    if (blockedTextarea) {
        blockedTextarea.addEventListener('input', () => {
            updateCounter('custom-blocked-content', 'blocked-line-count', 'blocked-char-count');
        });
        updateCounter('custom-blocked-content', 'blocked-line-count', 'blocked-char-count');
    }

    // Custom response counter
    const responseTextarea = document.getElementById('custom-response-content');
    if (responseTextarea) {
        responseTextarea.addEventListener('input', () => {
            updateCounter('custom-response-content', 'response-line-count', 'response-char-count');
        });
        updateCounter('custom-response-content', 'response-line-count', 'response-char-count');
    }
}

// Add visual feedback to button
function addButtonFeedback(button, success) {
    const originalText = button.textContent;
    button.classList.remove('success', 'error');

    if (success) {
        button.classList.add('success');
        button.textContent = '✓ Saved!';
    } else {
        button.classList.add('error');
        button.textContent = '✗ Error';
    }

    setTimeout(() => {
        button.classList.remove('success', 'error');
        button.textContent = originalText;
    }, 2000);
}

function loadCustomSettings() {
    // Load Blocked Domains
    fetch('/api/custom/blocked')
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                document.getElementById('custom-blocked-content').value = data.data.content;
                updateCounter('custom-blocked-content', 'blocked-line-count', 'blocked-char-count');
            } else {
                console.error('Failed to load blocked domains:', data.message);
            }
        })
        .catch(err => console.error('Error loading blocked domains:', err));

    // Load Custom Responses
    fetch('/api/custom/response')
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                document.getElementById('custom-response-content').value = data.data.content;
                updateCounter('custom-response-content', 'response-line-count', 'response-char-count');
            } else {
                console.error('Failed to load custom responses:', data.message);
            }
        })
        .catch(err => console.error('Error loading custom responses:', err));
}

function saveCustomBlocked() {
    const content = document.getElementById('custom-blocked-content').value;
    const button = event.target;

    button.disabled = true;

    fetch('/api/custom/blocked', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: content })
    })
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                addButtonFeedback(button, true);
                alert(i18n.t('messages.customBlockedSaved'));
            } else {
                addButtonFeedback(button, false);
                alert(i18n.t('messages.customBlockedSaveError', { error: data.message }));
            }
        })
        .catch(error => {
            addButtonFeedback(button, false);
            alert(i18n.t('messages.customBlockedSaveError', { error: error.message }));
        })
        .finally(() => {
            button.disabled = false;
        });
}

function saveCustomResponse() {
    const content = document.getElementById('custom-response-content').value;
    const button = event.target;

    button.disabled = true;

    fetch('/api/custom/response', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: content })
    })
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                addButtonFeedback(button, true);
                alert(i18n.t('messages.customResponseSaved'));
            } else {
                addButtonFeedback(button, false);
                alert(i18n.t('messages.customResponseSaveError', { error: data.message }));
            }
        })
        .catch(error => {
            addButtonFeedback(button, false);
            alert(i18n.t('messages.customResponseSaveError', { error: error.message }));
        })
        .finally(() => {
            button.disabled = false;
        });
}

// New function to enable/disable ping settings
function togglePingSettingsState() {
    const pingEnabled = document.getElementById('ping.enabled').checked;
    const pingSettings = document.getElementById('config-ping'); // The fieldset
    
    // Get all input/select elements within the ping settings fieldset, excluding the enable checkbox itself
    const elements = pingSettings.querySelectorAll('input:not(#ping\\.enabled), select');

    elements.forEach(el => {
        el.disabled = !pingEnabled;
        if (!pingEnabled) {
            el.classList.add('disabled-input'); // Add a class for styling
        } else {
            el.classList.remove('disabled-input');
        }
    });
}
