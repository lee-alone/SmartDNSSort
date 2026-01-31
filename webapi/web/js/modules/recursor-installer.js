/**
 * Recursor Installer Module
 * Handles Unbound installation, status monitoring, and UI updates
 */

class RecursorInstaller {
    constructor() {
        this.statusCheckInterval = null;
        this.statusCheckDelay = 2000; // 2 seconds
        this.maxRetries = 3;
        this.retryCount = 0;
        this.isInstalling = false;
        this.init();
    }

    /**
     * Initialize the module
     */
    init() {
        this.setupEventListeners();
        this.loadSystemInfo();
        this.checkInstallStatus();
    }

    /**
     * Setup event listeners for buttons
     */
    setupEventListeners() {
        // Event listeners removed - use config save/apply instead
    }



    /**
     * Check installation status
     */
    async checkInstallStatus() {
        try {
            const response = await fetch('/api/recursor/install-status');
            if (!response.ok) {
                throw new Error('Failed to fetch install status');
            }

            const data = await response.json();
            this.updateStatusUI(data);

            // If installation is complete or error, stop polling
            if (data.state === 'installed' || data.state === 'error') {
                this.stopStatusPolling();
                this.isInstalling = false;

                if (data.state === 'error') {
                    this.showError(data.error_msg || 'Installation failed');
                } else {
                    this.hideError();
                }
            }

            this.retryCount = 0;
        } catch (error) {
            console.error('Error checking install status:', error);
            this.retryCount++;

            if (this.retryCount >= this.maxRetries) {
                this.showError('Failed to check installation status');
                this.stopStatusPolling();
                this.isInstalling = false;
            }
        }
    }

    /**
     * Load system information
     */
    async loadSystemInfo() {
        try {
            const response = await fetch('/api/recursor/system-info');
            if (!response.ok) {
                throw new Error('Failed to fetch system info');
            }

            const data = await response.json();
            this.updateSystemInfoUI(data);
        } catch (error) {
            console.error('Error loading system info:', error);
        }
    }

    /**
     * Update status UI based on installation state
     */
    updateStatusUI(status) {
        const statusText = document.getElementById('recursor-status-text');
        const statusIndicator = document.getElementById('recursor-status-indicator');
        const checkbox = document.getElementById('upstream.enable_recursor');

        if (!statusText || !statusIndicator) {
            return;
        }

        let text = 'Unknown';
        let color = 'bg-gray-400';
        let isEnabled = false;

        switch (status.state) {
            case 'not_installed':
                text = 'Not Installed';
                color = 'bg-gray-400';
                isEnabled = false;
                break;
            case 'installing':
                text = 'Installing...';
                color = 'bg-yellow-500';
                isEnabled = false;
                this.updateInstallProgress(status.progress || 50, status.message);
                break;
            case 'installed':
                text = 'Running';
                color = 'bg-green-500';
                isEnabled = true;
                this.hideInstallProgress();
                break;
            case 'error':
                text = 'Error';
                color = 'bg-red-500';
                isEnabled = false;
                break;
        }

        statusText.textContent = text;
        statusIndicator.className = `w-3 h-3 rounded-full ${color}`;

        if (checkbox) {
            checkbox.checked = isEnabled;
        }
    }

    /**
     * Update system information UI
     */
    updateSystemInfoUI(sysInfo) {
        const osEl = document.getElementById('recursor-sys-os');
        const cpuEl = document.getElementById('recursor-sys-cpu');
        const memoryEl = document.getElementById('recursor-sys-memory');
        const unboundEl = document.getElementById('recursor-sys-unbound');

        if (osEl) {
            osEl.textContent = sysInfo.os || '-';
        }
        if (cpuEl) {
            cpuEl.textContent = sysInfo.cpu_cores || '-';
        }
        if (memoryEl) {
            memoryEl.textContent = sysInfo.memory_gb ? `${sysInfo.memory_gb.toFixed(2)} GB` : '-';
        }
        if (unboundEl) {
            unboundEl.textContent = sysInfo.unbound_version || '-';
        }

        // Show system info panel
        const sysInfoPanel = document.getElementById('recursor-system-info');
        if (sysInfoPanel) {
            sysInfoPanel.classList.remove('hidden');
        }
    }

    /**
     * Update installation progress
     */
    updateInstallProgress(progress, message) {
        const progressBar = document.getElementById('recursor-install-progress-bar');
        const progressMessage = document.getElementById('recursor-install-message');

        if (progressBar) {
            progressBar.style.width = `${Math.min(progress, 100)}%`;
        }

        if (progressMessage && message) {
            progressMessage.textContent = message;
        }
    }

    /**
     * Show installation progress UI
     */
    showInstallProgress() {
        const progressDiv = document.getElementById('recursor-install-progress');
        if (progressDiv) {
            progressDiv.classList.remove('hidden');
            this.updateInstallProgress(0, 'Initializing installation...');
        }
    }

    /**
     * Hide installation progress UI
     */
    hideInstallProgress() {
        const progressDiv = document.getElementById('recursor-install-progress');
        if (progressDiv) {
            progressDiv.classList.add('hidden');
        }
    }

    /**
     * Show error message
     */
    showError(message) {
        const errorAlert = document.getElementById('recursor-error-alert');
        const errorMessage = document.getElementById('recursor-error-message');

        if (errorAlert && errorMessage) {
            errorMessage.textContent = message;
            errorAlert.classList.remove('hidden');
        }
    }

    /**
     * Hide error message
     */
    hideError() {
        const errorAlert = document.getElementById('recursor-error-alert');
        if (errorAlert) {
            errorAlert.classList.add('hidden');
        }
    }



    /**
     * Start status polling
     */
    startStatusPolling() {
        if (this.statusCheckInterval) {
            clearInterval(this.statusCheckInterval);
        }

        this.statusCheckInterval = setInterval(() => {
            this.checkInstallStatus();
        }, this.statusCheckDelay);

        // Check immediately
        this.checkInstallStatus();
    }

    /**
     * Stop status polling
     */
    stopStatusPolling() {
        if (this.statusCheckInterval) {
            clearInterval(this.statusCheckInterval);
            this.statusCheckInterval = null;
        }
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    new RecursorInstaller();
});
