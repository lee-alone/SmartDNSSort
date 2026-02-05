// Main Application Entry Point
// This file loads all modular components

// Modules are loaded in order of dependency:
// 1. utils.js - Utility functions used by other modules
// 2. navigation.js - Navigation and view management
// 3. config.js - Configuration management
// 4. dashboard.js - Dashboard and statistics
// 5. adblock.js - AdBlock functionality
// 6. custom-settings.js - Custom settings management

// Wait for components to be loaded before initializing app
document.addEventListener('componentsLoaded', () => {
    // Initialize app when i18n is ready
    window.addEventListener('languageChanged', () => {
        // App initialized with language
    });

    // Load config on startup
    loadConfig();
    
    // Load custom settings on startup
    loadCustomSettings();
});
