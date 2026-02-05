// Configuration Management Module

const CONFIG_API_URL = '/api/config';
let originalConfig = {};

// Helper function to safely get form element value
function getFormValue(form, fieldName, defaultValue = '') {
    try {
        const element = form.elements[fieldName];
        if (!element) {
            return defaultValue;
        }
        if (element.type === 'checkbox') {
            return element.checked;
        }
        return element.value !== undefined ? element.value : defaultValue;
    } catch (e) {
        return defaultValue;
    }
}

function populateForm(config) {
    try {
        originalConfig = config;

        const setValue = (id, value) => {
            const el = document.getElementById(id);
            if (el) {
                el.value = value;
            }
        };
        const setChecked = (id, checked) => {
            const el = document.getElementById(id);
            if (el) {
                el.checked = checked;
            }
        };

        setValue('dns.listen_port', config.dns.listen_port);
        setChecked('dns.enable_tcp', config.dns.enable_tcp);
        setChecked('dns.enable_ipv6', config.dns.enable_ipv6);
        setValue('upstream.strategy', config.upstream.strategy);
        setValue('upstream.timeout_ms', config.upstream.timeout_ms);
        setValue('upstream.concurrency', config.upstream.concurrency);
        setValue('upstream.max_connections', config.upstream.max_connections || 0);
        setValue('upstream.sequential_timeout', config.upstream.sequential_timeout || 300);
        setValue('upstream.racing_delay', config.upstream.racing_delay || 100);
        setValue('upstream.racing_max_concurrent', config.upstream.racing_max_concurrent || 2);

        setChecked('upstream.dnssec', config.upstream.dnssec);

        if (config.upstream.health_check) {
            setChecked('upstream.health_check.enabled', config.upstream.health_check.enabled);
            setValue('upstream.health_check.failure_threshold', config.upstream.health_check.failure_threshold || 3);
            setValue('upstream.health_check.circuit_breaker_threshold', config.upstream.health_check.circuit_breaker_threshold || 5);
            setValue('upstream.health_check.circuit_breaker_timeout', config.upstream.health_check.circuit_breaker_timeout || 30);
            setValue('upstream.health_check.success_threshold', config.upstream.health_check.success_threshold || 2);
        }

        if (config.upstream.dynamic_param_optimization) {
            setValue('upstream.dynamic_param_optimization.ewma_alpha', config.upstream.dynamic_param_optimization.ewma_alpha || 0.2);
            setValue('upstream.dynamic_param_optimization.max_step_ms', config.upstream.dynamic_param_optimization.max_step_ms || 10);
        }

        setChecked('ping.enabled', config.ping.enabled);
        setValue('ping.count', config.ping.count);
        setValue('ping.timeout_ms', config.ping.timeout_ms);
        setValue('ping.concurrency', config.ping.concurrency);
        setValue('ping.strategy', config.ping.strategy);
        setValue('ping.max_test_ips', config.ping.max_test_ips);
        setValue('ping.rtt_cache_ttl_seconds', config.ping.rtt_cache_ttl_seconds);
        setChecked('ping.enable_http_fallback', config.ping.enable_http_fallback);

        togglePingSettingsState();
        const pingEnabledCheckbox = document.getElementById('ping.enabled');
        if (pingEnabledCheckbox) {
            pingEnabledCheckbox.addEventListener('change', togglePingSettingsState);
        }

        setValue('cache.fast_response_ttl', config.cache.fast_response_ttl);
        setValue('cache.user_return_ttl', config.cache.user_return_ttl);
        setValue('cache.min_ttl_seconds', config.cache.min_ttl_seconds);
        setValue('cache.max_ttl_seconds', config.cache.max_ttl_seconds);
        setValue('cache.negative_ttl_seconds', config.cache.negative_ttl_seconds);
        setValue('cache.error_cache_ttl_seconds', config.cache.error_cache_ttl_seconds);

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

        setValue('upstream.servers', (config.upstream.servers || []).join('\n'));
        setValue('upstream.bootstrap_dns', (config.upstream.bootstrap_dns || []).join('\n'));

        // Recursor 配置
        setChecked('upstream.enable_recursor', config.upstream.enable_recursor || false);
        setValue('upstream.recursor_port', config.upstream.recursor_port || 5353);

        // 初始化 Recursor 状态
        if (typeof updateRecursorStatus === 'function') {
            updateRecursorStatus();
        }
        
        // 添加递归状态变化监听
        const recursorCheckbox = document.getElementById('upstream.enable_recursor');
        if (recursorCheckbox) {
            recursorCheckbox.addEventListener('change', updateUpstreamRecursorAlert);
            // 初始化提示
            updateUpstreamRecursorAlert();
        }

        // Load AdBlock settings
        if (config.adblock) {
            setValue('adblock_update_interval_hours', config.adblock.update_interval_hours || 0);
            setValue('adblock_max_cache_age_hours', config.adblock.max_cache_age_hours || 0);
            setValue('adblock_max_cache_size_mb', config.adblock.max_cache_size_mb || 0);
            setValue('adblock_blocked_ttl', config.adblock.blocked_ttl || 0);
            setValue('adblock_block_mode', config.adblock.block_mode || 'nxdomain');
        }
    } catch (e) {
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
        .then(config => {
            // 延迟执行以确保所有组件都已加载
            setTimeout(() => {
                populateForm(config);
            }, 100);
        })
        .catch(error => {
            alert('Could not load configuration from server. Please open the browser developer console (F12) and check for errors.');
        });
}

function saveConfig() {
    return new Promise((resolve, reject) => {
        const form = document.getElementById('configForm');
        if (!form) {
            alert('Configuration form not found');
            return reject('Form not found');
        }

        const data = {
            dns: {
                listen_port: parseInt(getFormValue(form, 'dns.listen_port', '53')) || 53,
                enable_tcp: getFormValue(form, 'dns.enable_tcp', false),
                enable_ipv6: getFormValue(form, 'dns.enable_ipv6', false),
            },
            upstream: {
                servers: (getFormValue(form, 'upstream.servers', '') || '')
                    .split('\n')
                    .map(s => s.trim())
                    .filter(s => s !== ''),
                bootstrap_dns: (getFormValue(form, 'upstream.bootstrap_dns', '') || '')
                    .split('\n')
                    .map(s => s.trim())
                    .filter(s => s !== ''),
                strategy: getFormValue(form, 'upstream.strategy', 'auto') || 'auto',
                timeout_ms: parseInt(getFormValue(form, 'upstream.timeout_ms', '5000')) || 5000,
                concurrency: parseInt(getFormValue(form, 'upstream.concurrency', '3')) || 3,
                max_connections: parseInt(getFormValue(form, 'upstream.max_connections', '0')) || 0,
                sequential_timeout: parseInt(getFormValue(form, 'upstream.sequential_timeout', '300')) || 300,
                racing_delay: parseInt(getFormValue(form, 'upstream.racing_delay', '100')) || 100,
                racing_max_concurrent: parseInt(getFormValue(form, 'upstream.racing_max_concurrent', '2')) || 2,
                dnssec: getFormValue(form, 'upstream.dnssec', false),
                health_check: {
                    enabled: getFormValue(form, 'upstream.health_check.enabled', false),
                    failure_threshold: parseInt(getFormValue(form, 'upstream.health_check.failure_threshold', '3')) || 3,
                    circuit_breaker_threshold: parseInt(getFormValue(form, 'upstream.health_check.circuit_breaker_threshold', '5')) || 5,
                    circuit_breaker_timeout: parseInt(getFormValue(form, 'upstream.health_check.circuit_breaker_timeout', '30')) || 30,
                    success_threshold: parseInt(getFormValue(form, 'upstream.health_check.success_threshold', '2')) || 2,
                },
                dynamic_param_optimization: {
                    ewma_alpha: parseFloat(getFormValue(form, 'upstream.dynamic_param_optimization.ewma_alpha', '0.2')) || 0.2,
                    max_step_ms: parseInt(getFormValue(form, 'upstream.dynamic_param_optimization.max_step_ms', '10')) || 10,
                },
                enable_recursor: getFormValue(form, 'upstream.enable_recursor', false),
                recursor_port: parseInt(getFormValue(form, 'upstream.recursor_port', '5353')) || 5353,
            },
            ping: {
                enabled: getFormValue(form, 'ping.enabled', false),
                count: parseInt(getFormValue(form, 'ping.count', '3')) || 3,
                timeout_ms: parseInt(getFormValue(form, 'ping.timeout_ms', '1000')) || 1000,
                concurrency: parseInt(getFormValue(form, 'ping.concurrency', '16')) || 16,
                strategy: getFormValue(form, 'ping.strategy', 'auto') || 'auto',
                max_test_ips: parseInt(getFormValue(form, 'ping.max_test_ips', '0')) || 0,
                rtt_cache_ttl_seconds: parseInt(getFormValue(form, 'ping.rtt_cache_ttl_seconds', '300')) || 300,
                enable_http_fallback: getFormValue(form, 'ping.enable_http_fallback', false),
            },
            cache: {
                fast_response_ttl: parseInt(getFormValue(form, 'cache.fast_response_ttl', '15')) || 15,
                user_return_ttl: parseInt(getFormValue(form, 'cache.user_return_ttl', '600')) || 600,
                min_ttl_seconds: parseInt(getFormValue(form, 'cache.min_ttl_seconds', '3600')) || 3600,
                max_ttl_seconds: parseInt(getFormValue(form, 'cache.max_ttl_seconds', '84600')) || 84600,
                negative_ttl_seconds: parseInt(getFormValue(form, 'cache.negative_ttl_seconds', '300')) || 300,
                error_cache_ttl_seconds: parseInt(getFormValue(form, 'cache.error_cache_ttl_seconds', '30')) || 30,
                max_memory_mb: parseInt(getFormValue(form, 'cache.max_memory_mb', '128')) || 128,
                eviction_threshold: parseFloat(getFormValue(form, 'cache.eviction_threshold', '0.9')) || 0.9,
                eviction_batch_percent: parseFloat(getFormValue(form, 'cache.eviction_batch_percent', '0.1')) || 0.1,
                keep_expired_entries: getFormValue(form, 'cache.keep_expired_entries', false),
                protect_prefetch_domains: getFormValue(form, 'cache.protect_prefetch_domains', false),
                save_to_disk_interval_minutes: parseInt(getFormValue(form, 'cache.save_to_disk_interval_minutes', '60')) || 60,
            },
            prefetch: {
                enabled: getFormValue(form, 'prefetch.enabled', false),
            },
            webui: {
                enabled: getFormValue(form, 'webui.enabled', false),
                listen_port: parseInt(getFormValue(form, 'webui.listen_port', '8080')) || 8080,
            },
            system: {
                max_cpu_cores: parseInt(getFormValue(form, 'system.max_cpu_cores', '0')) || 0,
                sort_queue_workers: parseInt(getFormValue(form, 'system.sort_queue_workers', '4')) || 4,
                refresh_workers: parseInt(getFormValue(form, 'system.refresh_workers', '4')) || 4,
            },
        };

        // Handle AdBlock settings from form if they exist
        if (originalConfig && originalConfig.adblock) {
            data.adblock = originalConfig.adblock;
            // Update from form if elements exist
            const updateInterval = getFormValue(form, 'adblock_update_interval_hours', '');
            const maxCacheAge = getFormValue(form, 'adblock_max_cache_age_hours', '');
            const maxCacheSize = getFormValue(form, 'adblock_max_cache_size_mb', '');
            const blockedTtl = getFormValue(form, 'adblock_blocked_ttl', '');
            const blockMode = getFormValue(form, 'adblock_block_mode', '');

            if (updateInterval) {
                data.adblock.update_interval_hours = parseInt(updateInterval);
            }
            if (maxCacheAge) {
                data.adblock.max_cache_age_hours = parseInt(maxCacheAge);
            }
            if (maxCacheSize) {
                data.adblock.max_cache_size_mb = parseInt(maxCacheSize);
            }
            if (blockedTtl) {
                data.adblock.blocked_ttl = parseInt(blockedTtl);
            }
            if (blockMode) {
                data.adblock.block_mode = blockMode;
            }
        }
        if (originalConfig && originalConfig.stats) {
            data.stats = originalConfig.stats;
        }

        fetch(CONFIG_API_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data)
        })
            .then(response => {
                if (!response.ok) {
                    return response.text().then(text => {
                        throw new Error(text || `HTTP ${response.status}`);
                    });
                }
                return response.json();
            })
            .then(result => {
                alert(i18n.t('messages.configSaved'));
                loadConfig();
                // 重新加载自定义设置，以便显示/隐藏 unbound 配置窗口
                if (typeof loadCustomSettings === 'function') {
                    loadCustomSettings();
                }
                resolve(result);
            })
            .catch(error => {
                alert(i18n.t('messages.configSaveError', { error: error.message }));
                reject(error);
            });
    });
}

function updateStrategyUI() {
    const strategySelect = document.getElementById('upstream.strategy');
    if (!strategySelect) return;
    
    const sequentialParams = document.getElementById('sequential-params');
    const racingParams = document.getElementById('racing-params');
    const racingParamsConcurrent = document.getElementById('racing-params-concurrent');

    const strategy = strategySelect.value;

    if (sequentialParams) sequentialParams.style.display = strategy === 'sequential' ? 'block' : 'none';
    if (racingParams) racingParams.style.display = strategy === 'racing' ? 'block' : 'none';
    if (racingParamsConcurrent) racingParamsConcurrent.style.display = strategy === 'racing' ? 'block' : 'none';
}

function togglePingSettingsState() {
    const pingEnabled = document.getElementById('ping.enabled').checked;
    const pingSection = document.getElementById('section-ping');

    if (!pingSection) return;

    const elements = pingSection.querySelectorAll('input:not(#ping\\.enabled), select');

    elements.forEach(el => {
        el.disabled = !pingEnabled;
        if (!pingEnabled) {
            el.classList.add('disabled-input');
        } else {
            el.classList.remove('disabled-input');
        }
    });
}

function initializeConfigUI() {
    // 延迟执行以确保所有组件都已加载
    setTimeout(() => {
        const strategySelect = document.getElementById('upstream.strategy');
        if (strategySelect) {
            strategySelect.addEventListener('change', updateStrategyUI);
            updateStrategyUI();
        }
    }, 100);
}

/**
 * 更新上游配置中的递归状态提示
 */
function updateUpstreamRecursorAlert() {
    const recursorCheckbox = document.getElementById('upstream.enable_recursor');
    const alertBox = document.getElementById('recursor-status-alert');
    const upstreamServersField = document.getElementById('upstream.servers');
    
    if (!recursorCheckbox || !alertBox) return;
    
    if (recursorCheckbox.checked) {
        // 显示提示
        alertBox.classList.remove('hidden');
    } else {
        // 隐藏提示
        alertBox.classList.add('hidden');
        
        // 当取消递归时，检查是否需要填充默认服务器
        if (upstreamServersField) {
            const currentServers = upstreamServersField.value.trim();
            
            // 如果上游服务器为空，自动填充默认的 DoH 服务器
            if (!currentServers) {
                const defaultServers = [
                    'https://dns.google/dns-query',
                    'https://cloudflare-dns.com/dns-query'
                ].join('\n');
                
                upstreamServersField.value = defaultServers;
                
                // 显示提示信息
                showDefaultServersNotification();
            }
        }
    }
}

/**
 * 显示默认服务器已填充的通知
 */
function showDefaultServersNotification() {
    // 创建临时通知
    const notification = document.createElement('div');
    notification.className = 'fixed bottom-4 right-4 p-4 rounded-lg bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 shadow-lg z-50 max-w-sm';
    notification.innerHTML = `
        <div class="flex items-start gap-3">
            <svg class="w-5 h-5 text-green-600 dark:text-green-400 flex-shrink-0 mt-0.5" fill="currentColor" viewBox="0 0 20 20">
                <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd" />
            </svg>
            <div>
                <h4 class="font-semibold text-green-900 dark:text-green-100 mb-1" data-i18n="config.upstream.defaultServersAdded">Default Servers Added</h4>
                <p class="text-sm text-green-800 dark:text-green-200" data-i18n="config.upstream.defaultServersAddedDesc">
                    Google and Cloudflare DoH servers have been added to prevent DNS resolution failure.
                </p>
            </div>
        </div>
    `;
    
    document.body.appendChild(notification);
    
    // 3 秒后自动移除
    setTimeout(() => {
        notification.remove();
    }, 3000);
}

document.addEventListener('componentsLoaded', initializeConfigUI);

window.addEventListener('languageChanged', () => {
    loadConfig();
    updateStrategyUI();
});
