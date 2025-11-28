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
                hotDomainsTable.innerHTML = '<tr><td colspan="2" style="text-align:center;">No domain data yet.</td></tr>';
            }
            document.getElementById('status').textContent = 'Connected';
            document.getElementById('status').className = 'status connected';
        })
        .catch(error => {
            console.error('Error fetching stats:', error);
            const statusEl = document.getElementById('status');
            statusEl.textContent = 'Error: Could not fetch stats.';
            statusEl.className = 'status error';
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
                recentQueriesList.innerHTML = '<div style="text-align:center;">No recent queries.</div>';
            }
        })
        .catch(error => {
            console.error('Error fetching recent queries:', error);
            const recentQueriesList = document.getElementById('recent_queries_list');
            recentQueriesList.innerHTML = '<div style="text-align:center; color: red;">Error loading data.</div>';
        });
}

document.getElementById('clearCacheButton').addEventListener('click', () => {
    if (!confirm('Are you sure you want to clear the entire DNS cache?')) return;
    fetch('/api/cache/clear', { method: 'POST' })
        .then(response => {
            if (response.ok) {
                alert('DNS cache cleared successfully!');
                updateDashboard();
            } else {
                alert('Failed to clear DNS cache.');
            }
        })
        .catch(error => alert('An error occurred while trying to clear the DNS cache.'));
});

document.getElementById('clearStatsButton').addEventListener('click', () => {
    if (!confirm('Are you sure you want to clear all general and upstream statistics? This action cannot be undone.')) return;
    fetch('/api/stats/clear', { method: 'POST' })
        .then(response => {
            if (response.ok) {
                alert('All statistics cleared successfully!');
                updateDashboard(); // Refresh stats to show zeros
            } else {
                alert('Failed to clear statistics.');
            }
        })
        .catch(error => alert('An error occurred while trying to clear statistics.'));
});

document.getElementById('refreshButton').addEventListener('click', () => {
    updateDashboard();
    updateAdBlockTab();
});

document.getElementById('restartButton').addEventListener('click', () => {
    if (!confirm('Are you sure you want to restart the service? The connection will be temporarily interrupted.')) return;
    fetch('/api/restart', { method: 'POST' })
        .then(response => {
            if (response.ok) {
                alert('Service restart initiated. The page will reload automatically in 5 seconds.');
                // 等待5秒后自动刷新页面
                setTimeout(() => {
                    location.reload();
                }, 5000);
            } else {
                alert('Failed to restart service.');
            }
        })
        .catch(error => {
            alert('An error occurred while trying to restart the service.');
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
        setValue('ping.count', config.ping.count);
        setValue('ping.timeout_ms', config.ping.timeout_ms);
        setValue('ping.concurrency', config.ping.concurrency);
        setValue('ping.strategy', config.ping.strategy);
        setValue('ping.max_test_ips', config.ping.max_test_ips);
        setValue('ping.rtt_cache_ttl_seconds', config.ping.rtt_cache_ttl_seconds);
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

        setChecked('prefetch.enabled', config.prefetch.enabled);
        setValue('prefetch.top_domains_limit', config.prefetch.top_domains_limit);
        setValue('prefetch.refresh_before_expire_seconds', config.prefetch.refresh_before_expire_seconds);
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

    // Start with the original config to preserve uneditable fields like adblock
    const data = originalConfig;

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
    };
    data.ping = {
        count: parseInt(form.elements['ping.count'].value),
        timeout_ms: parseInt(form.elements['ping.timeout_ms'].value),
        concurrency: parseInt(form.elements['ping.concurrency'].value),
        strategy: form.elements['ping.strategy'].value,
        max_test_ips: parseInt(form.elements['ping.max_test_ips'].value),
        rtt_cache_ttl_seconds: parseInt(form.elements['ping.rtt_cache_ttl_seconds'].value),
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
    };
    data.prefetch = {
        enabled: form.elements['prefetch.enabled'].checked,
        top_domains_limit: parseInt(form.elements['prefetch.top_domains_limit'].value),
        refresh_before_expire_seconds: parseInt(form.elements['prefetch.refresh_before_expire_seconds'].value),
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

    fetch(CONFIG_API_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
    })
        .then(response => {
            if (response.ok) {
                alert('Configuration saved and applied successfully!');
            } else {
                response.text().then(text => alert('Failed to save configuration: ' + text));
            }
        })
        .catch(error => {
            console.error('Error saving config:', error);
            alert('An error occurred while saving the configuration.');
        });
}

// Initial Load
updateDashboard();
loadConfig();
updateAdBlockTab();


// --- AdBlock Tab Logic ---

function updateAdBlockTab() {
    // Fetch AdBlock status
    fetch('/api/adblock/status')
        .then(response => response.ok ? response.json() : Promise.reject('Failed to load AdBlock status'))
        .then(response => {
            const data = response.data;
            if (!data.enabled) {
                document.getElementById('adblock_status').textContent = 'Disabled';
                document.getElementById('adblock_status').className = 'value status-error';
                return; // Stop if not enabled
            }
            document.getElementById('adblock_status').textContent = 'Enabled';
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
            document.getElementById('adblock_status').textContent = 'Error';
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
                        <td><button class="btn btn-danger btn-sm" data-url="${source.url}">Delete</button></td>
                    `;
                });
            } else {
                sourcesTable.innerHTML = '<tr><td colspan="5" style="text-align:center;">No rule sources configured.</td></tr>';
            }
        })
        .catch(error => {
            console.error('Error fetching AdBlock sources:', error);
            const sourcesTable = document.getElementById('adblock_sources_table').getElementsByTagName('tbody')[0];
            sourcesTable.innerHTML = '<tr><td colspan="5" style="text-align:center; color: red;">Error loading sources.</td></tr>';
        });
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
                statusEl.textContent = enabled ? 'Enabled' : 'Disabled';
                statusEl.className = enabled ? 'value status-success' : 'value status-error';
                console.log('AdBlock status changed to:', enabled);
            } else {
                // Revert toggle on failure
                e.target.checked = !enabled;
                response.text().then(text => alert('Failed to toggle AdBlock: ' + text));
            }
        })
        .catch(error => {
            // Revert toggle on error
            e.target.checked = !enabled;
            alert('An error occurred: ' + error);
        });
});

document.getElementById('adblockUpdateRulesButton').addEventListener('click', () => {
    if (!confirm('Are you sure you want to force an update of all adblock rules? This may take a moment.')) return;
    fetch('/api/adblock/update', { method: 'POST' })
        .then(response => {
            if (response.ok) {
                alert('AdBlock rule update started in the background.');
            } else {
                response.text().then(text => alert('Failed to start update: ' + text));
            }
        })
        .catch(error => alert('An error occurred: ' + error));
});

document.getElementById('adblockAddSourceButton').addEventListener('click', () => {
    const urlInput = document.getElementById('adblock_new_source_url');
    const url = urlInput.value.trim();
    if (!url) {
        alert('Please enter a URL for the new rule source.');
        return;
    }
    fetch('/api/adblock/sources', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: url })
    })
        .then(response => {
            if (response.ok) {
                alert('Source added successfully! It will be included in the next update.');
                urlInput.value = '';
                updateAdBlockTab(); // Refresh the list
            } else {
                response.json().then(data => alert('Failed to add source: ' + data.message));
            }
        })
        .catch(error => alert('An error occurred: ' + error));
});

// Event delegation for delete buttons
document.getElementById('adblock_sources_table').addEventListener('click', function (e) {
    if (e.target && e.target.matches('button.btn-danger')) {
        const url = e.target.dataset.url;
        if (!confirm(`Are you sure you want to delete the source:\\n${url}`)) return;

        fetch('/api/adblock/sources', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url: url })
        })
            .then(response => {
                if (response.ok) {
                    alert('Source deleted successfully!');
                    updateAdBlockTab(); // Refresh the list
                } else {
                    response.json().then(data => alert('Failed to delete source: ' + data.message));
                }
            })
            .catch(error => alert('An error occurred: ' + error));
    }
});

document.getElementById('adblockTestDomainButton').addEventListener('click', () => {
    const domainInput = document.getElementById('adblock_test_domain');
    const domain = domainInput.value.trim();
    const resultDiv = document.getElementById('adblock_test_result');

    if (!domain) {
        resultDiv.textContent = 'Please enter a domain to test.';
        resultDiv.className = 'test-result status-error';
        return;
    }

    resultDiv.textContent = 'Testing...';
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
                resultDiv.innerHTML = `<strong>Blocked!</strong><br>Rule: <code>${result.rule}</code>`;
                resultDiv.className = 'test-result status-error';
            } else {
                resultDiv.textContent = 'Not Blocked.';
                resultDiv.className = 'test-result status-success';
            }
        })
        .catch(error => {
            resultDiv.textContent = 'An error occurred during the test.';
            resultDiv.className = 'test-result status-error';
            console.error('Test domain error:', error);
        });
});


// AdBlock BlockMode Save Button Event Handler
document.getElementById('adblockSaveBlockModeButton').addEventListener('click', () => {
    const blockModeSelect = document.getElementById('adblock_block_mode');
    const blockMode = blockModeSelect.value;

    if (!blockMode) {
        alert('Please select a block mode.');
        return;
    }

    fetch('/api/adblock/blockmode', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ block_mode: blockMode })
    })
        .then(response => {
            if (response.ok) {
                alert('Block mode saved successfully!');
            } else {
                response.text().then(text => alert('Failed to save block mode: ' + text));
            }
        })
        .catch(error => {
            alert('An error occurred: ' + error);
            console.error('Save block mode error:', error);
        });
});

