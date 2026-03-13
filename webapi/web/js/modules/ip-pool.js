// IP Pool Monitor Module

// Global variables for expand/collapse functionality
let allDeadIPs = [];
let allHealthyIPs = [];
let isExpanded = false;
let showDeadIPsMode = true; // true: show dead IPs, false: show healthy IPs

async function loadIPPoolData() {
	try {
		const view = showDeadIPsMode ? 'dead' : 'all';
		const response = await fetch(`/api/ip-pool/top?view=${view}`);
		if (!response.ok) {
			throw new Error('Network response was not ok');
		}
		const result = await response.json();

		if (result.success && result.data) {
			const data = result.data;

			// Ensure monitor_stats is not null
			const monitorStats = data.monitor_stats || {
				total_refreshes: 0,
				total_ips_refreshed: 0,
				t0_pool_size: 0,
				t1_pool_size: 0,
				t2_pool_size: 0
			};

			// Update stats
			document.getElementById('ip_pool_total_ips').textContent = data.total_ips || 0;
			document.getElementById('ip_pool_total_refreshes').textContent = monitorStats.total_refreshes || 0;
			document.getElementById('ip_pool_total_ips_refreshed').textContent = monitorStats.total_ips_refreshed || 0;
			document.getElementById('ip_pool_t0_size').textContent = monitorStats.t0_pool_size || 0;
			document.getElementById('ip_pool_t1_size').textContent = monitorStats.t1_pool_size || 0;
			document.getElementById('ip_pool_t2_size').textContent = monitorStats.t2_pool_size || 0;

			// Update monitor status badge/switch (if we add it)
			const statusBadge = document.getElementById('ipPoolPausedBadge');
			if (statusBadge) {
				if (data.monitor_enabled) {
					statusBadge.classList.add('hidden');
				} else {
					statusBadge.classList.remove('hidden');
				}
			}

			// Store IPs based on mode
			if (showDeadIPsMode) {
				allDeadIPs = data.top_ips || [];
			} else {
				allHealthyIPs = data.top_ips || [];
			}
			
			// Reset expand state on new data load
			isExpanded = false;
			
			// Render table based on current mode
			renderIPPoolTable();
		} else {
			throw new Error(result.message || 'Failed to parse IP pool data');
		}
	} catch (error) {
		console.error('Failed to load IP pool data:', error);
		const tbody = document.getElementById('ip_pool_table_body');
		if (tbody) {
			tbody.innerHTML = `
				<tr>
					<td colspan="6" class="px-6 py-4 text-center text-red-500">
						<span class="material-symbols-outlined inline-block align-middle mr-2">error</span>
						<span data-i18n="dashboard.errorLoadingData">Failed to load IP pool data</span>
					</td>
				</tr>
			`;
		}
		// Hide expand container on error
		const expandContainer = document.getElementById('expand-container');
		if (expandContainer) {
			expandContainer.classList.add('hidden');
		}
	}
}

function renderIPPoolTable() {
	const tbody = document.getElementById('ip_pool_table_body');
	const expandContainer = document.getElementById('expand-container');
	const countInfo = document.getElementById('dead-ip-count-info');
	
	if (!tbody) return;
	
	tbody.innerHTML = '';
	
	// Determine which list to display
	const displayList = showDeadIPsMode ? allDeadIPs : allHealthyIPs;
	const displayCount = displayList.length;
	
	// Determine how many to display
	const itemsToShow = isExpanded ? displayList : displayList.slice(0, 10);
	
	if (itemsToShow.length > 0) {
		itemsToShow.forEach(ip => {
			const row = document.createElement('tr');
			row.className = 'hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors cursor-default';

			// Format last access time
			const lastAccess = new Date(ip.last_access);
			const lastAccessStr = lastAccess.toLocaleString();

			// RTT color coding
			let rttClass = 'text-red-600 dark:text-red-400 font-bold';
			if (ip.rtt < 999999) {
				rttClass = 'text-green-600 dark:text-green-400';
				if (ip.rtt > 100) rttClass = 'text-yellow-600 dark:text-yellow-400';
				if (ip.rtt > 300) rttClass = 'text-orange-600 dark:text-orange-400';
				if (ip.rtt > 1000) rttClass = 'text-red-600 dark:text-red-400';
			}
			if (ip.rtt <= 0) rttClass = 'text-gray-400';

			row.innerHTML = `
				<td class="px-6 py-3 font-mono">${ip.ip || '-'}</td>
				<td class="px-6 py-3 truncate max-w-xs" title="${ip.rep_domain || ''}">${ip.rep_domain || '-'}</td>
				<td class="px-6 py-3">${ip.ref_count || 0}</td>
				<td class="px-6 py-3">${ip.access_heat || 0}</td>
				<td class="px-6 py-3 font-mono ${rttClass}">${ip.rtt >= 999999 ? 'DEAD' : (ip.rtt >= 0 ? ip.rtt : '-')}</td>
				<td class="px-6 py-3 text-xs opacity-80">${lastAccessStr}</td>
			`;
			tbody.appendChild(row);
		});
	} else {
		const emptyMsg = showDeadIPsMode 
			? 'dashboard.noDeadIPs' 
			: 'dashboard.noIpData';
		const defaultEmptyMsg = showDeadIPsMode ? 'No dead IPs detected' : 'No IP pool data available';
		
		tbody.innerHTML = `
		<tr>
			<td colspan="6" class="px-6 py-4 text-center text-text-sub-light dark:text-text-sub-dark">
				<span class="material-symbols-outlined inline-block align-middle mr-2">check_circle</span>
				<span data-i18n="${emptyMsg}">${defaultEmptyMsg}</span>
			</td>
		</tr>
		`;
	}
	
	const modeToggleBtn = document.getElementById('btn-toggle-ip-mode');
	if (modeToggleBtn) {
		const deadIpsText = window.i18n ? window.i18n.t('dashboard.deadIPs') : 'Dead IPs';
		const topIpsText = window.i18n ? window.i18n.t('dashboard.topIPs') : 'Top IPs';
		modeToggleBtn.textContent = showDeadIPsMode ? deadIpsText : topIpsText;
		
		// Style the button based on mode
		if (showDeadIPsMode) {
			modeToggleBtn.classList.remove('bg-blue-100', 'text-blue-700', 'dark:bg-blue-900', 'dark:text-blue-200');
			modeToggleBtn.classList.add('bg-red-100', 'text-red-700', 'dark:bg-red-900', 'dark:text-red-200');
		} else {
			modeToggleBtn.classList.remove('bg-red-100', 'text-red-700', 'dark:bg-red-900', 'dark:text-red-200');
			modeToggleBtn.classList.add('bg-blue-100', 'text-blue-700', 'dark:bg-blue-900', 'dark:text-blue-200');
		}
	}
	
	// Handle expand button visibility
	if (expandContainer && countInfo) {
		if (displayCount > 10 && !isExpanded) {
			expandContainer.classList.remove('hidden');
			// Update count info text
			const countText = window.i18n
				? window.i18n.t('dashboard.ipCountInfo', { count: displayCount })
				: `Showing 10 of ${displayCount} IPs`;
			countInfo.textContent = countText;
		} else {
			expandContainer.classList.add('hidden');
		}
	}
	
	// Apply translations if available
	if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
		window.i18n.applyTranslations();
	}
}

// Initialize IP Pool Monitor
function initializeIPPoolMonitor() {
	// Load initial data
	loadIPPoolData().then(() => {
		// Update toggle switch state based on monitor_enabled from data
		// Since loadIPPoolData updates it via data.monitor_enabled, 
		// we just need to ensure the event listener is set up.
	});

	// Add refresh button listener
	const refreshBtn = document.getElementById('refreshIpPoolBtn');
	if (refreshBtn) {
		refreshBtn.addEventListener('click', loadIPPoolData);
	}
	
	// Add expand button listener
	const expandBtn = document.getElementById('btn-expand-ips');
	if (expandBtn) {
		expandBtn.addEventListener('click', () => {
			isExpanded = true;
			renderIPPoolTable();
		});
	}

	// Add mode toggle button listener (Dead IPs vs Top IPs)
	const modeToggleBtn = document.getElementById('btn-toggle-ip-mode');
	if (modeToggleBtn) {
		modeToggleBtn.addEventListener('click', () => {
			showDeadIPsMode = !showDeadIPsMode;
			isExpanded = false; // Reset expand state when switching modes
			loadIPPoolData(); // Fetch new data for the selected mode
		});
	}

	// Add monitor enabled toggle listener
	const toggleMonitor = document.getElementById('toggleIPMonitorEnabled');
	if (toggleMonitor) {
		// Fetch current config to set initial state if data not loaded yet
		fetch('/api/config').then(r => r.json()).then(result => {
			if (result.ip_monitor) {
				toggleMonitor.checked = result.ip_monitor.enabled;
			}
		});

		toggleMonitor.addEventListener('change', async () => {
			const enabled = toggleMonitor.checked;
			try {
				const response = await fetch(`/api/ip-pool/toggle?enabled=${enabled}`, {
					method: 'POST'
				});
				if (!response.ok) throw new Error('Toggle failed');
				const result = await response.json();
				if (result.success) {
					// Refresh data to show new status
					loadIPPoolData();
					
					// Show toast message if available
					if (window.showToast) {
						window.showToast(window.i18n ? window.i18n.t('messages.configSaved') : 'Settings saved');
					}
				}
			} catch (error) {
				console.error('Failed to toggle IP monitor:', error);
				toggleMonitor.checked = !enabled; // Revert
			}
		});
	}

	// Apply translations to the component
	if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
		window.i18n.applyTranslations();
	}
}

// Listen for component load event
document.addEventListener('componentsLoaded', () => {
	// Check if IP pool monitor component exists
	if (document.getElementById('ip_pool_table')) {
		initializeIPPoolMonitor();
	}
});

// Auto-refresh every 30 seconds
setInterval(() => {
	if (document.getElementById('ip_pool_table')) {
		loadIPPoolData();
	}
}, 30000);

// Listen for language changes
window.addEventListener('languageChanged', () => {
	// Re-render table to update language-dependent text
	if (allDeadIPs.length > 0 || allHealthyIPs.length > 0) {
		renderIPPoolTable();
	}
	// Re-apply translations when language changes
	if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
		window.i18n.applyTranslations();
	}
});
