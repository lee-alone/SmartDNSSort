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

document.addEventListener('DOMContentLoaded', initializeCounters);

window.addEventListener('languageChanged', () => {
    loadCustomSettings();
    initializeCounters();
});
