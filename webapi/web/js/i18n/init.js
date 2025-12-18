// i18n Initialization - Auto-init on DOMContentLoaded

document.addEventListener('DOMContentLoaded', () => {
    if (window.i18n && typeof window.i18n.init === 'function') {
        window.i18n.init();
    } else {
        console.error('i18n core not loaded. Make sure to load i18n/core.js before i18n/init.js');
    }
});
