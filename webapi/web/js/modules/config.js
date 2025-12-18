// Configuration Management Module

const CONFIG_API_URL = '/api/config';
let originalConfig = {};

function populateForm(config) {
    try {
        originalConfig = config;

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
        setChecked('upstream.dnssec', config.upstream.dnssec);

        if (config.upstream.health_check) {
            setChecked('upstream.health_check.enabled', config.upstream.health_check.enabled);
            setValue('upstream.health_check.failure_threshold', config.upstream.health_check.failure_threshold || 3);
            setValue('upstream.health_check.circuit_breaker_threshold', config.upstream.health_check.circuit_breaker_threshold || 5);
            setValue('upstream.health_check.circuit_breaker_timeout', config.upstream.health_check.circuit_breaker_timeout || 30);
            setValue('upstream.health_check.success_threshold', config.upstream.health_check.success_threshold || 2);
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
        document.getElementById('ping.enabled').addEventListener('change', togglePingSettingsState);
        
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
    return new Promise((resolve, reject) => {
        const form = document.getElementById('configForm');
        if (!form) {
            alert('Configuration form not found');
            return reject('Form not found');
        }

        const data = {
            dns: {
                listen_port: parseInt(form.elements['dns.listen_port'].value) || 53,
                enable_tcp: form.elements['dns.enable_tcp'].checked,
                enable_ipv6: form.elements['dns.enable_ipv6'].checked,
            },
            upstream: {
                servers: form.elements['upstream.servers'].value
                    .split('\n')
                    .map(s => s.trim())
                    .filter(s => s !== ''),
                bootstrap_dns: form.elements['upstream.bootstrap_dns'].value
                    .split('\n')
                    .map(s => s.trim())
                    .filter(s => s !== ''),
                strategy: form.elements['upstream.strategy'].value || 'sequential',
                timeout_ms: parseInt(form.elements['upstream.timeout_ms'].value) || 5000,
                concurrency: parseInt(form.elements['upstream.concurrency'].value) || 3,
                sequential_timeout: parseInt(form.elements['upstream.sequential_timeout'].value) || 300,
                racing_delay: parseInt(form.elements['upstream.racing_delay'].value) || 100,
                racing_max_concurrent: parseInt(form.elements['upstream.racing_max_concurrent'].value) || 2,
                nxdomain_for_errors: form.elements['upstream.nxdomain_for_errors'].checked,
                dnssec: form.elements['upstream.dnssec'].checked,
                health_check: {
                    enabled: form.elements['upstream.health_check.enabled'].checked,
                    failure_threshold: parseInt(form.elements['upstream.health_check.failure_threshold'].value) || 3,
                    circuit_breaker_threshold: parseInt(form.elements['upstream.health_check.circuit_breaker_threshold'].value) || 5,
                    circuit_breaker_timeout: parseInt(form.elements['upstream.health_check.circuit_breaker_timeout'].value) || 30,
                    success_threshold: parseInt(form.elements['upstream.health_check.success_threshold'].value) || 2,
                }
            },
            ping: {
                enabled: form.elements['ping.enabled'].checked,
                count: parseInt(form.elements['ping.count'].value) || 3,
                timeout_ms: parseInt(form.elements['ping.timeout_ms'].value) || 1000,
                concurrency: parseInt(form.elements['ping.concurrency'].value) || 16,
                strategy: form.elements['ping.strategy'].value || 'min',
                max_test_ips: parseInt(form.elements['ping.max_test_ips'].value) || 0,
                rtt_cache_ttl_seconds: parseInt(form.elements['ping.rtt_cache_ttl_seconds'].value) || 300,
                enable_http_fallback: form.elements['ping.enable_http_fallback'].checked,
            },
            cache: {
                fast_response_ttl: parseInt(form.elements['cache.fast_response_ttl'].value) || 15,
                user_return_ttl: parseInt(form.elements['cache.user_return_ttl'].value) || 600,
                min_ttl_seconds: parseInt(form.elements['cache.min_ttl_seconds'].value) || 3600,
                max_ttl_seconds: parseInt(form.elements['cache.max_ttl_seconds'].value) || 84600,
                negative_ttl_seconds: parseInt(form.elements['cache.negative_ttl_seconds'].value) || 300,
                error_cache_ttl_seconds: parseInt(form.elements['cache.error_cache_ttl_seconds'].value) || 30,
                max_memory_mb: parseInt(form.elements['cache.max_memory_mb'].value) || 128,
                eviction_threshold: parseFloat(form.elements['cache.eviction_threshold'].value) || 0.9,
                eviction_batch_percent: parseFloat(form.elements['cache.eviction_batch_percent'].value) || 0.1,
                keep_expired_entries: form.elements['cache.keep_expired_entries'].checked,
                protect_prefetch_domains: form.elements['cache.protect_prefetch_domains'].checked,
                save_to_disk_interval_minutes: parseInt(form.elements['cache.save_to_disk_interval_minutes'].value) || 60,
            },
            prefetch: {
                enabled: form.elements['prefetch.enabled'].checked,
            },
            webui: {
                enabled: form.elements['webui.enabled'].checked,
                listen_port: parseInt(form.elements['webui.listen_port'].value) || 8080,
            },
            system: {
                max_cpu_cores: parseInt(form.elements['system.max_cpu_cores'].value) || 0,
                sort_queue_workers: parseInt(form.elements['system.sort_queue_workers'].value) || 4,
                refresh_workers: parseInt(form.elements['system.refresh_workers'].value) || 4,
            },
        };

        if (originalConfig && originalConfig.adblock) {
            data.adblock = originalConfig.adblock;
        }
        if (originalConfig && originalConfig.stats) {
            data.stats = originalConfig.stats;
        }

        console.log('[DEBUG] Collected form data:', JSON.stringify(data, null, 2));

        fetch(CONFIG_API_URL, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data)
        })
            .then(response => {
                console.log('[DEBUG] Server response status:', response.status);
                if (!response.ok) {
                    return response.text().then(text => {
                        console.error('[ERROR] Server error response:', text);
                        throw new Error(text || `HTTP ${response.status}`);
                    });
                }
                return response.json();
            })
            .then(result => {
                console.log('[DEBUG] Config saved successfully:', result);
                alert(i18n.t('messages.configSaved'));
                loadConfig();
                resolve(result);
            })
            .catch(error => {
                console.error('[ERROR] Failed to save config:', error);
                alert(i18n.t('messages.configSaveError', { error: error.message }));
                reject(error);
            });
    });
}

function updateStrategyUI() {
    const strategySelect = document.getElementById('upstream.strategy');
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
    const pingSettings = document.getElementById('config-ping');

    const elements = pingSettings.querySelectorAll('input:not(#ping\\.enabled), select');

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
    const strategySelect = document.getElementById('upstream.strategy');
    if (strategySelect) {
        strategySelect.addEventListener('change', updateStrategyUI);
        setTimeout(() => {
            updateStrategyUI();
        }, 100);
    }
}

document.addEventListener('DOMContentLoaded', initializeConfigUI);

window.addEventListener('languageChanged', () => {
    loadConfig();
    updateStrategyUI();
});
