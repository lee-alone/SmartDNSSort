// IP Pool Monitor Module

// Global variables for expand/collapse functionality
let allDeadIPs = [];
let isExpanded = false;

async function loadIPPoolData() {
	try {
		const response = await fetch('/api/ip-pool/top');
		if (!response.ok) {
			throw new Error('Network response was not ok');
		}
		const result = await response.json();

		if (result.success && result.data) {
			const data = result.data;

			// Update stats
			document.getElementById('ip_pool_total_ips').textContent = data.total_ips || 0;
			document.getElementById('ip_pool_total_refreshes').textContent = data.monitor_stats?.total_refreshes || 0;
			document.getElementById('ip_pool_total_ips_refreshed').textContent = data.monitor_stats?.total_ips_refreshed || 0;
			document.getElementById('ip_pool_t0_size').textContent = data.monitor_stats?.t0_pool_size || 0;
			document.getElementById('ip_pool_t1_size').textContent = data.monitor_stats?.t1_pool_size || 0;
			document.getElementById('ip_pool_t2_size').textContent = data.monitor_stats?.t2_pool_size || 0;

			// Reset expand state on new data load
			isExpanded = false;
			
			// Render table with dead IPs
			renderIPPoolTable(data.top_ips || []);
		}
	} catch (error) {
		console.error('Failed to load IP pool data:', error);
		const tbody = document.getElementById('ip_pool_table_body');
		tbody.innerHTML = `
			<tr>
				<td colspan="6" class="px-6 py-4 text-center text-red-500">
					<span class="material-symbols-outlined inline-block align-middle mr-2">error</span>
					Failed to load IP pool data
				</td>
			</tr>
		`;
		// Hide expand container on error
		const expandContainer = document.getElementById('expand-container');
		if (expandContainer) {
			expandContainer.classList.add('hidden');
		}
	}
}

function renderIPPoolTable(ips) {
	// Save all dead IPs for expand/collapse functionality
	allDeadIPs = ips;
	
	const tbody = document.getElementById('ip_pool_table_body');
	const expandContainer = document.getElementById('expand-container');
	const countInfo = document.getElementById('dead-ip-count-info');
	
	tbody.innerHTML = '';
	
	// Determine how many to display
	const displayList = isExpanded ? allDeadIPs : allDeadIPs.slice(0, 10);
	
	if (displayList.length > 0) {
		displayList.forEach(ip => {
			const row = document.createElement('tr');
			row.className = 'hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors';

			// Format last access time
			const lastAccess = new Date(ip.last_access);
			const lastAccessStr = lastAccess.toLocaleString();

			// RTT color coding - dead IPs (RTT >= 999999) are shown in red
			let rttClass = 'text-red-600 dark:text-red-400 font-bold';
			if (ip.rtt < 999999) {
				rttClass = 'text-green-600 dark:text-green-400';
				if (ip.rtt > 100) rttClass = 'text-yellow-600 dark:text-yellow-400';
				if (ip.rtt > 300) rttClass = 'text-red-600 dark:text-red-400';
			}
			if (ip.rtt === 0) rttClass = 'text-gray-400';

			row.innerHTML = `
				<td class="px-6 py-3 font-mono">${ip.ip || '-'}</td>
				<td class="px-6 py-3 truncate max-w-xs">${ip.rep_domain || '-'}</td>
				<td class="px-6 py-3">${ip.ref_count || 0}</td>
				<td class="px-6 py-3">${ip.access_heat || 0}</td>
				<td class="px-6 py-3 font-mono ${rttClass}">${ip.rtt >= 999999 ? 'DEAD' : (ip.rtt >= 0 ? ip.rtt : '-')}</td>
				<td class="px-6 py-3 text-xs">${lastAccessStr}</td>
			`;
			tbody.appendChild(row);
		});
	} else {
		tbody.innerHTML = `
		<tr>
			<td colspan="6" class="px-6 py-4 text-center text-text-sub-light dark:text-text-sub-dark">
				<span class="material-symbols-outlined inline-block align-middle mr-2">check_circle</span>
				<span data-i18n="dashboard.noDeadIPs">No dead IPs detected</span>
			</td>
		</tr>
		`;
	}
	
	// Handle expand button visibility
	if (allDeadIPs.length > 10 && !isExpanded) {
		expandContainer.classList.remove('hidden');
		// Update count info text
		const countText = window.i18n
			? window.i18n.t('dashboard.deadIPCountInfo', { count: allDeadIPs.length })
			: `Showing 10 of ${allDeadIPs.length} dead IPs`;
		countInfo.textContent = countText;
	} else {
		expandContainer.classList.add('hidden');
	}
	
	// Apply translations if available
	if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
		window.i18n.applyTranslations();
	}
}

// Initialize IP Pool Monitor
function initializeIPPoolMonitor() {
	// Load initial data
	loadIPPoolData();

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
			renderIPPoolTable(allDeadIPs);
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
	if (allDeadIPs.length > 0) {
		renderIPPoolTable(allDeadIPs);
	}
	// Re-apply translations when language changes
	if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
		window.i18n.applyTranslations();
	}
});
