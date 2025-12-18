// Utility Functions Module

function formatUptime(seconds) {
    const d = Math.floor(seconds / (3600 * 24));
    const h = Math.floor((seconds % (3600 * 24)) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = Math.floor(seconds % 60);
    return `${d}d ${h}h ${m}m ${s}s`;
}

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

function updateCounter(textareaId, lineCountId, charCountId) {
    const textarea = document.getElementById(textareaId);
    const content = textarea.value;
    const lines = content ? content.split('\n').length : 0;
    const chars = content.length;

    document.getElementById(lineCountId).textContent = `${lines} line${lines !== 1 ? 's' : ''}`;
    document.getElementById(charCountId).textContent = `${chars} character${chars !== 1 ? 's' : ''}`;
}
