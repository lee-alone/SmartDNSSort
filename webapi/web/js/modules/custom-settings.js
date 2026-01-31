// Custom Settings Module

function initializeCounters() {
    const blockedTextarea = document.getElementById('custom-blocked-content');
    if (blockedTextarea) {
        blockedTextarea.addEventListener('input', () => {
            updateCounter('custom-blocked-content', 'blocked-line-count', 'blocked-char-count');
        });
        updateCounter('custom-blocked-content', 'blocked-line-count', 'blocked-char-count');
    }

    const responseTextarea = document.getElementById('custom-response-content');
    if (responseTextarea) {
        responseTextarea.addEventListener('input', () => {
            updateCounter('custom-response-content', 'response-line-count', 'response-char-count');
        });
        updateCounter('custom-response-content', 'response-line-count', 'response-char-count');
    }

    const unboundTextarea = document.getElementById('unbound-config-content');
    if (unboundTextarea) {
        unboundTextarea.addEventListener('input', () => {
            updateCounter('unbound-config-content', 'unbound-line-count', 'unbound-char-count');
        });
    }
}

function loadCustomSettings() {
    fetch('/api/custom/blocked')
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                const el = document.getElementById('custom-blocked-content');
                if (el) {
                    el.value = data.data.content;
                    updateCounter('custom-blocked-content', 'blocked-line-count', 'blocked-char-count');
                }
            } else {
                console.error('Failed to load blocked domains:', data.message);
            }
        })
        .catch(err => console.error('Error loading blocked domains:', err));

    fetch('/api/custom/response')
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                const el = document.getElementById('custom-response-content');
                if (el) {
                    el.value = data.data.content;
                    updateCounter('custom-response-content', 'response-line-count', 'response-char-count');
                }
            } else {
                console.error('Failed to load custom responses:', data.message);
            }
        })
        .catch(err => console.error('Error loading custom responses:', err));

    // 加载 Unbound 配置
    loadUnboundConfig();
}

function loadUnboundConfig() {
    fetch('/api/unbound/config')
        .then(response => response.json())
        .then(data => {
            const section = document.getElementById('unbound-config-section');
            const el = document.getElementById('unbound-config-content');
            
            // 检查递归是否启用
            if (data.enabled === false) {
                // 递归未启用，隐藏编辑器
                if (section) section.classList.add('hidden');
                return;
            }
            
            // 递归已启用，显示编辑器
            if (section) section.classList.remove('hidden');
            if (el) {
                el.value = data.content || '';
                updateCounter('unbound-config-content', 'unbound-line-count', 'unbound-char-count');
            }
        })
        .catch(err => {
            console.error('Error loading unbound config:', err);
            const section = document.getElementById('unbound-config-section');
            if (section) section.classList.add('hidden');
        });
}

function reloadUnboundConfig(button) {
    if (!button) button = event?.target;
    if (!button) return;

    button.disabled = true;
    loadUnboundConfig();
    
    setTimeout(() => {
        button.disabled = false;
    }, 500);
}

function saveUnboundConfig(button) {
    const el = document.getElementById('unbound-config-content');
    if (!el) return;
    
    const content = el.value;
    if (!button) button = event?.target;
    if (!button) return;

    button.disabled = true;

    fetch('/api/unbound/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: content })
    })
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                addButtonFeedback(button, true);
                alert(i18n.t('messages.unboundConfigSaved'));
            } else {
                addButtonFeedback(button, false);
                alert(i18n.t('messages.unboundConfigSaveError', { error: data.message }));
            }
        })
        .catch(error => {
            addButtonFeedback(button, false);
            alert(i18n.t('messages.unboundConfigSaveError', { error: error.message }));
        })
        .finally(() => {
            button.disabled = false;
        });
}

function saveCustomBlocked(button) {
    const el = document.getElementById('custom-blocked-content');
    if (!el) return;
    
    const content = el.value;
    if (!button) button = event?.target;
    if (!button) return;

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

function saveCustomResponse(button) {
    const el = document.getElementById('custom-response-content');
    if (!el) return;
    
    const content = el.value;
    if (!button) button = event?.target;
    if (!button) return;

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

document.addEventListener('componentsLoaded', initializeCounters);

window.addEventListener('languageChanged', () => {
    loadCustomSettings();
    initializeCounters();
});
