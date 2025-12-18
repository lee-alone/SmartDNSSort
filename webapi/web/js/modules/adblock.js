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
        .catch(error => console.error('Error fetching AdBlock settings:', error));
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
            console.error('Error fetching AdBlock status:', error);
            document.getElementById('adblock_status').textContent = i18n.t('adblock.statusError');
            document.getElementById('adblock_status').className = 'value status-error';
        });

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
                                response.text().then(text => alert(i18n.t('messages.adblockSourceAddError', { error: text })));
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

function initializeAdBlockHandlers() {
    document.getElementById('adblock_enable_toggle').addEventListener('change', function (e) {
        const enabled = e.target.checked;

        fetch('/api/adblock/toggle', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enabled: enabled })
        })
            .then(response => {
                if (response.ok) {
                    const statusEl = document.getElementById('adblock_status');
                    statusEl.textContent = enabled ? i18n.t('adblock.statusEnabled') : i18n.t('adblock.statusDisabled');
                    statusEl.className = enabled ? 'value status-success' : 'value status-error';
                    console.log('AdBlock status changed to:', enabled);
                } else {
                    e.target.checked = !enabled;
                    response.text().then(text => alert(i18n.t('messages.adblockToggleError', { error: text })));
                }
            })
            .catch(error => {
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
                    updateAdBlockTab();
                } else {
                    response.json().then(data => alert(i18n.t('messages.adblockSourceAddError', { error: data.message })));
                }
            })
            .catch(error => alert(i18n.t('messages.adblockSourceAddError', { error: error })));
    });

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
                        updateAdBlockTab();
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
}

document.addEventListener('componentsLoaded', initializeAdBlockHandlers);

window.addEventListener('languageChanged', () => {
    updateAdBlockTab();
});
