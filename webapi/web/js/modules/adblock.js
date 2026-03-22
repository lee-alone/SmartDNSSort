// AdBlock Module

function loadAdBlockSettings() {
    fetch('/api/adblock/settings')
        .then(response => response.ok ? response.json() : Promise.reject('Failed to load AdBlock settings'))
        .then(response => {
            const settings = response.data;
            const updateIntervalEl = document.getElementById('adblock_update_interval_hours');
            const maxCacheAgeEl = document.getElementById('adblock_max_cache_age_hours');
            const maxCacheSizeEl = document.getElementById('adblock_max_cache_size_mb');
            const blockedTtlEl = document.getElementById('adblock_blocked_ttl');
            
            if (updateIntervalEl) updateIntervalEl.value = settings.update_interval_hours;
            if (maxCacheAgeEl) maxCacheAgeEl.value = settings.max_cache_age_hours;
            if (maxCacheSizeEl) maxCacheSizeEl.value = settings.max_cache_size_mb;
            if (blockedTtlEl) blockedTtlEl.value = settings.blocked_ttl;
        })
        .catch(error => {
            // 静默处理错误
        });
}

function updateAdBlockTab() {
    fetch('/api/adblock/status')
        .then(response => response.ok ? response.json() : Promise.reject('Failed to load AdBlock status'))
        .then(response => {
            const data = response.data;
     
            if (!data.enabled) {
                document.getElementById('adblock_status').textContent = i18n.t('adblock.statusDisabled');
                document.getElementById('adblock_status').className = 'value status-error';
                return;
            }
            document.getElementById('adblock_status').textContent = i18n.t('adblock.statusEnabled');
            document.getElementById('adblock_status').className = 'value status-success';

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
            document.getElementById('adblock_status').textContent = i18n.t('adblock.statusError');
            document.getElementById('adblock_status').className = 'value status-error';
        });

    fetch('/api/config')
        .then(response => response.ok ? response.json() : Promise.reject('Failed to load config'))
        .then(config => {
            const blockMode = config.data.adblock.block_mode || 'nxdomain';
            const blockModeSelect = document.getElementById('adblock_block_mode');
            if (blockModeSelect) {
                blockModeSelect.value = blockMode;
            }
        })
        .catch(error => {
            // 静默处理错误
        });

    fetch('/api/adblock/sources')
    .then(response => response.ok ? response.json() : Promise.reject('Failed to load AdBlock sources'))
    .then(response => {
    const sources = response.data;
    const sourcesTable = document.getElementById('adblock_sources_table').getElementsByTagName('tbody')[0];
    sourcesTable.innerHTML = '';
    if (sources && sources.length > 0) {
    sources.forEach(source => {
    const row = sourcesTable.insertRow();
    let statusClass = 'status-active';
    if (source.status === 'initializing') statusClass = 'status-warning';
    if (source.status === 'failed') statusClass = 'status-warning';
    if (source.status === 'bad') statusClass = 'status-error';
    
    // 使用安全的 DOM 操作防止 XSS
    // 单元格 1: URL
    const cell1 = row.insertCell(0);
    cell1.className = 'adblock-url';
    cell1.textContent = source.url; // 安全：使用 textContent
    
    // 单元格 2: 状态
    const cell2 = row.insertCell(1);
    const statusSpan = document.createElement('span');
    statusSpan.className = `status-indicator ${statusClass}`;
    statusSpan.textContent = source.status; // 安全：使用 textContent
    cell2.appendChild(statusSpan);
    
    // 单元格 3: 规则数
    const cell3 = row.insertCell(2);
    cell3.className = 'value';
    cell3.textContent = source.rule_count || 0; // 安全：使用 textContent
    
    // 单元格 4: 最后更新时间
    const cell4 = row.insertCell(3);
    cell4.textContent = source.last_update ? new Date(source.last_update).toLocaleString() : 'Never'; // 安全：使用 textContent
    
    // 单元格 5: 启用复选框
    const cell5 = row.insertCell(4);
    const checkbox = document.createElement('input');
    checkbox.type = 'checkbox';
    checkbox.className = 'source-enable-toggle';
    checkbox.dataset.url = source.url; // 安全：通过 dataset 设置
    checkbox.checked = source.enabled;
    cell5.appendChild(checkbox);
    
    // 单元格 6: 删除按钮
    const cell6 = row.insertCell(5);
    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'btn btn-danger btn-sm';
    deleteBtn.dataset.url = source.url; // 安全：通过 dataset 设置
    deleteBtn.textContent = 'Delete';
    cell6.appendChild(deleteBtn);
    });
    document.querySelectorAll('.source-enable-toggle').forEach(cb => {
    cb.addEventListener('change', async (e) => {
    const url = e.target.dataset.url;
    const enabled = e.target.checked;
    try {
    const response = await CSRFManager.secureFetch('/api/adblock/sources', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: url, enabled: enabled })
    });
    if (!response.ok) {
    e.target.checked = !enabled;
    const text = await response.text();
    alert(i18n.t('messages.adblockSourceAddError', { error: text }));
    }
    } catch (err) {
    e.target.checked = !enabled;
    alert(i18n.t('messages.adblockSourceAddError', { error: err }));
    }
    });
    });
    } else {
    // 安全：使用 DOM 操作
    const row = sourcesTable.insertRow();
    const cell = row.insertCell(0);
    cell.colSpan = 6;
    cell.style.textAlign = 'center';
    cell.textContent = i18n.t('adblock.noSources');
    }
    })
    .catch(error => {
    const sourcesTable = document.getElementById('adblock_sources_table').getElementsByTagName('tbody')[0];
    // 安全：使用 DOM 操作
    sourcesTable.innerHTML = '';
    const row = sourcesTable.insertRow();
    const cell = row.insertCell(0);
    cell.colSpan = 5;
    cell.style.textAlign = 'center';
    cell.style.color = 'red';
    cell.textContent = i18n.t('adblock.errorLoadingSources');
    });

    loadAdBlockSettings();
}

function initializeAdBlockHandlers() {
    document.getElementById('adblock_enable_toggle').addEventListener('change', async function (e) {
        const enabled = e.target.checked;

        try {
            const response = await CSRFManager.secureFetch('/api/adblock/toggle', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ enabled: enabled })
            });
            if (response.ok) {
                const statusEl = document.getElementById('adblock_status');
                statusEl.textContent = enabled ? i18n.t('adblock.statusEnabled') : i18n.t('adblock.statusDisabled');
                statusEl.className = enabled ? 'value status-success' : 'value status-error';
            } else {
                e.target.checked = !enabled;
                const text = await response.text();
                alert(i18n.t('messages.adblockToggleError', { error: text }));
            }
        } catch (error) {
            e.target.checked = !enabled;
            alert(i18n.t('messages.adblockToggleError', { error: error }));
        }
    });

    document.getElementById('adblockUpdateRulesButton').addEventListener('click', async () => {
        if (!confirm(i18n.t('messages.adblockUpdateConfirm'))) return;
        try {
            const response = await CSRFManager.secureFetch('/api/adblock/update', { method: 'POST' });
            if (response.ok) {
                alert(i18n.t('messages.adblockUpdateStarted'));
            } else {
                const text = await response.text();
                alert(i18n.t('messages.adblockUpdateFailed', { error: text }));
            }
        } catch (error) {
            alert(i18n.t('messages.adblockUpdateError', { error: error }));
        }
    });

    document.getElementById('adblockAddSourceButton').addEventListener('click', async () => {
        const urlInput = document.getElementById('adblock_new_source_url');
        const url = urlInput.value.trim();
        if (!url) {
            alert(i18n.t('messages.enterUrl'));
            return;
        }
        try {
            const response = await CSRFManager.secureFetch('/api/adblock/sources', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ url: url })
            });
            if (response.ok) {
                alert(i18n.t('messages.adblockSourceAdded'));
                urlInput.value = '';
                updateAdBlockTab();
            } else {
                const data = await response.json();
                alert(i18n.t('messages.adblockSourceAddError', { error: data.message }));
            }
        } catch (error) {
            alert(i18n.t('messages.adblockSourceAddError', { error: error }));
        }
    });

    document.getElementById('adblock_sources_table').addEventListener('click', async function (e) {
        if (e.target && e.target.matches('button.btn-danger')) {
            const url = e.target.dataset.url;
            if (!confirm(i18n.t('messages.deleteConfirm') + '\n' + url)) return;

            try {
                const response = await CSRFManager.secureFetch('/api/adblock/sources', {
                    method: 'DELETE',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ url: url })
                });
                if (response.ok) {
                    alert(i18n.t('messages.adblockSourceDeleted'));
                    updateAdBlockTab();
                } else {
                    const data = await response.json();
                    alert(i18n.t('messages.adblockSourceDeleteError', { error: data.message }));
                }
            } catch (error) {
                alert(i18n.t('messages.adblockSourceDeleteError', { error: error }));
            }
        }
    });

    document.getElementById('adblockTestDomainButton').addEventListener('click', async () => {
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

        try {
            const response = await CSRFManager.secureFetch('/api/adblock/test', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ domain: domain })
            });
            if (!response.ok) {
                throw new Error('Test request failed');
            }
            const resultData = await response.json();
            const result = resultData.data;
            if (result.blocked) {
                resultDiv.innerHTML = `<strong>${i18n.t('messages.blocked')}</strong><br>${i18n.t('messages.rule')}: <code>${result.rule}</code>`;
                resultDiv.className = 'test-result status-error';
            } else {
                resultDiv.textContent = i18n.t('messages.notBlocked');
                resultDiv.className = 'test-result status-success';
            }
        } catch (error) {
            resultDiv.textContent = i18n.t('messages.testError');
            resultDiv.className = 'test-result status-error';
        }
    });
}

document.addEventListener('componentsLoaded', initializeAdBlockHandlers);

window.addEventListener('languageChanged', () => {
    updateAdBlockTab();
});
