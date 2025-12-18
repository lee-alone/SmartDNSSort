// i18n Core Engine - Translation management and application

const i18nCore = {
    locale: 'en',
    translations: {},
    availableLocales: ['en', 'zh-CN'],

    /**
     * Initialize i18n system
     * Determines language, loads translations, and applies to page
     */
    async init() {
        // 1. Determine language
        const savedLang = localStorage.getItem('smartdns_lang');
        const browserLang = navigator.language;

        if (savedLang && this.availableLocales.includes(savedLang)) {
            this.locale = savedLang;
        } else if (browserLang.startsWith('zh')) {
            this.locale = 'zh-CN';
        } else {
            this.locale = 'en';
        }

        // 2. Load translations
        await this.loadTranslations(this.locale);

        // 3. Apply to page
        this.applyTranslations();

        // 4. Update select if exists
        const langSelect = document.getElementById('langSwitch');
        if (langSelect) {
            langSelect.value = this.locale;
            langSelect.addEventListener('change', (e) => {
                this.setLanguage(e.target.value);
            });
        }

        // 5. Set html lang attribute
        document.documentElement.lang = this.locale;

        // 6. Dispatch ready event
        window.dispatchEvent(new CustomEvent('languageChanged', { detail: this.locale }));
    },

    /**
     * Load translations for specified language
     * @param {string} lang - Language code (e.g., 'en', 'zh-CN')
     */
    async loadTranslations(lang) {
        // Determine which resource object to use
        let resources = {};
        
        if (lang === 'zh-CN' && typeof resourcesZhCn !== 'undefined') {
            resources = resourcesZhCn;
        } else if (lang === 'en' && typeof resourcesEn !== 'undefined') {
            resources = resourcesEn;
        } else {
            console.warn(`Language ${lang} not found in resources, falling back to en`);
            resources = typeof resourcesEn !== 'undefined' ? resourcesEn : {};
            this.locale = 'en';
        }

        this.translations = resources;
    },

    /**
     * Set language and update UI
     * @param {string} lang - Language code
     */
    async setLanguage(lang) {
        if (!this.availableLocales.includes(lang)) return;

        this.locale = lang;
        localStorage.setItem('smartdns_lang', lang);
        document.documentElement.lang = lang;

        await this.loadTranslations(lang);
        this.applyTranslations();

        // Dispatch event for other components
        window.dispatchEvent(new CustomEvent('languageChanged', { detail: lang }));
    },

    /**
     * Get current language
     * @returns {string} Current language code
     */
    getCurrentLanguage() {
        return this.locale;
    },

    /**
     * Translate a key with optional parameters
     * @param {string} key - Translation key (dot-separated path)
     * @param {object} params - Parameters for placeholder replacement
     * @returns {string} Translated string
     */
    t(key, params = {}) {
        const keys = key.split('.');
        let value = this.translations;

        for (const k of keys) {
            if (value && value[k]) {
                value = value[k];
            } else {
                console.warn(`Missing translation: ${key}`);
                return key;
            }
        }

        // If value is an object, try to get the _label property
        if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
            if (value._label && typeof value._label === 'string') {
                value = value._label;
            } else {
                console.warn(`Missing translation or _label for: ${key}`);
                return key;
            }
        }

        if (typeof value !== 'string') {
            return key;
        }

        // Replace placeholders
        return value.replace(/{(\w+)}/g, (match, p1) => {
            return params[p1] !== undefined ? params[p1] : match;
        });
    },

    /**
     * Apply translations to DOM elements
     * Supports data-i18n, data-i18n-ph, and data-i18n-title attributes
     */
    applyTranslations() {
        // 1. Text content
        document.querySelectorAll('[data-i18n]').forEach(el => {
            const key = el.getAttribute('data-i18n');
            el.textContent = this.t(key);
        });

        // 2. Placeholders
        document.querySelectorAll('[data-i18n-ph]').forEach(el => {
            const key = el.getAttribute('data-i18n-ph');
            el.placeholder = this.t(key);
        });

        // 3. Titles
        document.querySelectorAll('[data-i18n-title]').forEach(el => {
            const key = el.getAttribute('data-i18n-title');
            el.title = this.t(key);
        });
    }
};

// Expose to window
window.i18n = i18nCore;
