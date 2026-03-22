// IP Pool Monitor Module

// Global variables for expand/collapse functionality
let allDeadIPs = [];
let allHealthyIPs = [];
let isExpanded = false;
let showDeadIPsMode = false; // true: show dead IPs, false: show healthy IPs

// Format number with K/M suffix for large numbers
function formatNumber(num) {
	if (num >= 1000000) {
		return (num / 1000000).toFixed(1) + 'M';
	} else if (num >= 1000) {
		return (num / 1000).toFixed(1) + 'K';
	}
	return num.toString();
}

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
				total_planned_pings: 0,
				total_actual_pings: 0,
				total_skipped_pings: 0,
				t0_pool_size: 0,
				t1_pool_size: 0,
				t2_pool_size: 0,
				downgraded_ips: 0,
				hourly_quota_used: 0,
				hourly_quota_limit: 5000
			};

			// Update stats - Resource Efficiency
			const plannedPings = monitorStats.total_planned_pings || 0;
			const actualPings = monitorStats.total_actual_pings || 0;
			const skippedPings = monitorStats.total_skipped_pings || 0;
			const efficiencyRate = plannedPings > 0 ? ((skippedPings / plannedPings) * 100).toFixed(1) : 0;

			// Update with number pulse animation for skipped pings
			const skippedElement = document.getElementById('ip_pool_skipped_pings');
			const previousSkipped = skippedElement ? parseInt(skippedElement.textContent.replace(/[^0-9]/g, '')) || 0 : 0;
			
			const plannedElement = document.getElementById('ip_pool_planned_pings');
			const actualElement = document.getElementById('ip_pool_actual_pings');
			const efficiencyElement = document.getElementById('ip_pool_efficiency_rate');
			
			if (plannedElement) plannedElement.textContent = formatNumber(plannedPings);
			if (actualElement) actualElement.textContent = formatNumber(actualPings);
			if (skippedElement) skippedElement.textContent = formatNumber(skippedPings);
			if (efficiencyElement) efficiencyElement.textContent = `${efficiencyRate}%`;
			
			// Add pulse animation if skipped pings increased
			if (skippedElement && skippedPings > previousSkipped) {
				skippedElement.classList.remove('number-pulse');
				void skippedElement.offsetWidth; // Trigger reflow
				skippedElement.classList.add('number-pulse');
			}

			// Update progress bar: Actual vs Planned
			const ratioText = document.getElementById('ip_pool_ratio_text');
			const ratioBar = document.getElementById('ip_pool_ratio_bar');
			if (ratioText && ratioBar) {
				ratioText.textContent = `${formatNumber(actualPings)}/${formatNumber(plannedPings)}`;
				const ratioPercent = plannedPings > 0 ? (actualPings / plannedPings) * 100 : 0;
				ratioBar.style.width = `${ratioPercent}%`;
				// Color based on efficiency
				if (ratioPercent < 20) {
					ratioBar.className = 'bg-green-600 dark:bg-green-400 h-2.5 rounded-full transition-all duration-300';
				} else if (ratioPercent < 50) {
					ratioBar.className = 'bg-yellow-600 dark:bg-yellow-400 h-2.5 rounded-full transition-all duration-300';
				} else {
					ratioBar.className = 'bg-orange-600 dark:bg-orange-400 h-2.5 rounded-full transition-all duration-300';
				}
			}

			// Update stats - Pool Dynamics
			const t0SizeElement = document.getElementById('ip_pool_t0_size');
			const t1SizeElement = document.getElementById('ip_pool_t1_size');
			const t2SizeElement = document.getElementById('ip_pool_t2_size');
			const downgradedElement = document.getElementById('ip_pool_downgraded_ips');
			
			if (t0SizeElement) t0SizeElement.textContent = monitorStats.t0_pool_size || 0;
			if (t1SizeElement) t1SizeElement.textContent = monitorStats.t1_pool_size || 0;
			if (t2SizeElement) t2SizeElement.textContent = monitorStats.t2_pool_size || 0;
			if (downgradedElement) downgradedElement.textContent = monitorStats.downgraded_ips || 0;

			// Update T0 note
			const t0Note = document.getElementById('ip_pool_t0_note');
			if (t0Note) {
				const t0Size = monitorStats.t0_pool_size || 0;
				if (t0Size > 0) {
					t0Note.textContent = window.i18n
						? window.i18n.t('dashboard.t0PoolNote', { size: t0Size })
						: `Core pool with ${t0Size} active IPs`;
				} else {
					t0Note.textContent = '';
				}
			}

			// Update stats - Quota Monitoring
			const quotaUsed = monitorStats.hourly_quota_used || 0;
			const quotaLimit = monitorStats.hourly_quota_limit || 5000;
			const quotaPercent = quotaLimit > 0 ? (quotaUsed / quotaLimit) * 100 : 0;

			const quotaUsedElement = document.getElementById('ip_pool_quota_used');
			const quotaLimitElement = document.getElementById('ip_pool_quota_limit');
			
			if (quotaUsedElement) quotaUsedElement.textContent = formatNumber(quotaUsed);
			if (quotaLimitElement) quotaLimitElement.textContent = formatNumber(quotaLimit);

			const quotaBar = document.getElementById('ip_pool_quota_bar');
			const quotaStatus = document.getElementById('ip_pool_quota_status');
			if (quotaBar && quotaStatus) {
				quotaBar.style.width = `${quotaPercent}%`;
				
				// Update status and color based on quota usage with gradient bar
				quotaBar.className = 'quota-gradient-bar h-2.5 rounded-full transition-all duration-300 progress-bar-enhanced';
				
				// Remove all gradient classes first
				quotaBar.classList.remove('low', 'medium', 'high', 'critical');
				
				if (quotaPercent < 70) {
					quotaBar.classList.add('low');
					quotaStatus.textContent = window.i18n ? window.i18n.t('dashboard.statusHealthy') : 'Status: Healthy';
					quotaStatus.className = 'text-xs font-medium text-green-600 dark:text-green-400';
				} else if (quotaPercent < 90) {
					quotaBar.classList.add('medium');
					quotaStatus.textContent = window.i18n ? window.i18n.t('dashboard.statusWarning') : 'Status: Warning';
					quotaStatus.className = 'text-xs font-medium text-yellow-600 dark:text-yellow-400';
				} else {
					quotaBar.classList.add('critical');
					quotaStatus.textContent = window.i18n ? window.i18n.t('dashboard.statusLimitReached') : 'Status: Limit Reached';
					quotaStatus.className = 'text-xs font-medium text-red-600 dark:text-red-400';
				}
			}

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
			
			// Render table based on current state
			renderIPPoolTable();
		} else {
			throw new Error(result.message || 'Failed to parse IP pool data');
		}
	} catch (error) {
	const tbody = document.getElementById('ip_pool_table_body');
	if (tbody) {
	// 安全：使用 DOM 操作替代 innerHTML
	tbody.innerHTML = '';
	const row = document.createElement('tr');
	const cell = document.createElement('td');
	cell.colSpan = 6;
	cell.className = 'px-6 py-4 text-center text-red-500';
	
	const iconSpan = document.createElement('span');
	iconSpan.className = 'material-symbols-outlined inline-block align-middle mr-2';
	iconSpan.textContent = 'error';
	
	const textSpan = document.createElement('span');
	textSpan.setAttribute('data-i18n', 'dashboard.errorLoadingData');
	textSpan.textContent = 'Failed to load IP pool data';
	
	cell.appendChild(iconSpan);
	cell.appendChild(textSpan);
	row.appendChild(cell);
	tbody.appendChild(row);
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
	const expandBtn = document.getElementById('btn-expand-ips');
	
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
	if (ip.rtt < 9000) {
	rttClass = 'text-green-600 dark:text-green-400';
	if (ip.rtt > 100) rttClass = 'text-yellow-600 dark:text-yellow-400';
	if (ip.rtt > 300) rttClass = 'text-orange-600 dark:text-orange-400';
	if (ip.rtt > 1000) rttClass = 'text-red-600 dark:text-red-400';
	}
	if (ip.rtt <= 0) rttClass = 'text-gray-400';
	
	// 安全：使用 DOM 操作替代 innerHTML
	// 单元格 1: IP 地址
	const cell1 = document.createElement('td');
	cell1.className = 'px-6 py-3 font-mono text-xs md:text-sm';
	cell1.textContent = ip.ip || '-';
	row.appendChild(cell1);
	
	// 单元格 2: 代表域名
	const cell2 = document.createElement('td');
	cell2.className = 'px-6 py-3 truncate max-w-[120px] md:max-w-xs';
	cell2.title = ip.rep_domain || ''; // 安全：通过 title 属性设置
	cell2.textContent = ip.rep_domain || '-';
	row.appendChild(cell2);
	
	// 单元格 3: 引用计数
	const cell3 = document.createElement('td');
	cell3.className = 'px-6 py-3 text-center md:text-left';
	cell3.textContent = ip.ref_count || 0;
	row.appendChild(cell3);
	
	// 单元格 4: 访问热度
	const cell4 = document.createElement('td');
	cell4.className = 'px-6 py-3 text-center md:text-left';
	cell4.textContent = ip.access_heat || 0;
	row.appendChild(cell4);
	
	// 单元格 5: RTT
	const cell5 = document.createElement('td');
	cell5.className = `px-6 py-3 font-mono ${rttClass}`;
	cell5.textContent = ip.rtt >= 9000 ? 'DEAD' : (ip.rtt >= 0 ? ip.rtt : '-');
	row.appendChild(cell5);
	
	// 单元格 6: 最后访问时间
	const cell6 = document.createElement('td');
	cell6.className = 'px-6 py-3 text-xs opacity-80 hidden md:table-cell';
	cell6.textContent = lastAccessStr;
	row.appendChild(cell6);
	
	tbody.appendChild(row);
	});
	} else {
	const emptyMsg = showDeadIPsMode
	? 'dashboard.noDeadIPs'
	: 'dashboard.noIpData';
	const defaultEmptyMsg = showDeadIPsMode ? 'No dead IPs detected' : 'No IP pool data available';
	
	// 安全：使用 DOM 操作替代 innerHTML
	const row = document.createElement('tr');
	const cell = document.createElement('td');
	cell.colSpan = 6;
	cell.className = 'px-6 py-4 text-center text-text-sub-light dark:text-text-sub-dark';
	
	const iconSpan = document.createElement('span');
	iconSpan.className = 'material-symbols-outlined inline-block align-middle mr-2';
	iconSpan.textContent = 'check_circle';
	
	const textSpan = document.createElement('span');
	textSpan.setAttribute('data-i18n', emptyMsg);
	textSpan.textContent = defaultEmptyMsg;
	
	cell.appendChild(iconSpan);
	cell.appendChild(textSpan);
	row.appendChild(cell);
	tbody.appendChild(row);
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
	
	// Handle expand button visibility and labels
	if (expandContainer && countInfo && expandBtn) {
		if (displayCount > 10) {
			expandContainer.classList.remove('hidden');
			
			// Update info text
			const infoKey = showDeadIPsMode ? 'dashboard.deadIPCountInfo' : 'dashboard.ipCountInfo';
			const countText = window.i18n
				? window.i18n.t(infoKey, { count: displayCount })
				: `Showing ${itemsToShow.length} of ${displayCount} IPs`;
			countInfo.textContent = countText;
			
			// Update button text and class
			if (isExpanded) {
				expandBtn.textContent = window.i18n ? window.i18n.t('actions.showLess') || 'Show Less' : 'Show Less';
				expandBtn.classList.remove('bg-blue-600', 'hover:bg-blue-700');
				expandBtn.classList.add('bg-gray-500', 'hover:bg-gray-600');
			} else {
				const btnKey = showDeadIPsMode ? 'dashboard.showAllDeadIPs' : 'dashboard.showAllTopIPs';
				expandBtn.textContent = window.i18n ? window.i18n.t(btnKey) : 'Show All';
				expandBtn.classList.remove('bg-gray-500', 'hover:bg-gray-600');
				expandBtn.classList.add('bg-blue-600', 'hover:bg-blue-700');
			}
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

	// Add expand button listener
	const expandBtn = document.getElementById('btn-expand-ips');
	if (expandBtn) {
		expandBtn.addEventListener('click', () => {
			isExpanded = !isExpanded;
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
			if (result.data.ip_monitor) {
				toggleMonitor.checked = result.data.ip_monitor.enabled;
			}
		});

		toggleMonitor.addEventListener('change', async () => {
			const enabled = toggleMonitor.checked;
			try {
				const response = await CSRFManager.secureFetch(`/api/ip-pool/toggle?enabled=${enabled}`, {
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
