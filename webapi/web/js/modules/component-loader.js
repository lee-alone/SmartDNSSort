/**
 * Component Loader Module
 * Dynamically loads HTML components into the page
 */

const ComponentLoader = {
    /**
     * Load a component from file and insert into container
     * @param {string} componentPath - Path to component file
     * @param {string} containerId - ID of container element
     */
    async loadComponent(componentPath, containerId) {
        try {
            const response = await fetch(componentPath);
            if (!response.ok) {
                throw new Error(`Failed to load ${componentPath}: ${response.statusText}`);
            }
            const html = await response.text();
            const container = document.getElementById(containerId);
            if (container) {
                container.innerHTML = html;
            } else {
                console.warn(`Container with ID "${containerId}" not found`);
            }
        } catch (error) {
            console.error(`Error loading component ${componentPath}:`, error);
        }
    },

    /**
     * Load multiple components in parallel
     * @param {Array} components - Array of {path, containerId} objects
     */
    async loadComponents(components) {
        const promises = components.map(comp => 
            this.loadComponent(comp.path, comp.containerId)
        );
        await Promise.all(promises);
    },

    /**
     * Initialize all components
     */
    async init() {
        const components = [
            { path: 'components/sidebar.html', containerId: 'sidebar-container' },
            { path: 'components/header.html', containerId: 'header-container' },
            { path: 'components/dashboard.html', containerId: 'view-dashboard' },
            { path: 'components/config.html', containerId: 'view-config' },
            { path: 'components/custom-rules.html', containerId: 'view-rules' },
            { path: 'components/footer.html', containerId: 'footer-container' },
        ];

        await this.loadComponents(components);

        // Load config sub-components
        const configComponents = [
            { path: 'components/config-upstream.html', containerId: 'upstream-config-container' },
            { path: 'components/config-recursor.html', containerId: 'recursor-config-container' },
            { path: 'components/config-ping.html', containerId: 'ping-config-container' },
            { path: 'components/config-cache.html', containerId: 'cache-config-container' },
            { path: 'components/config-adblock.html', containerId: 'adblock-config-container' },
            { path: 'components/config-other.html', containerId: 'other-config-container' },
        ];

        await this.loadComponents(configComponents);

        // Emit custom event to signal components are ready
        document.dispatchEvent(new CustomEvent('componentsLoaded'));
    }
};

// Initialize components when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    ComponentLoader.init();
});
