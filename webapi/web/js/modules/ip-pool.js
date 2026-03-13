// IP Pool Monitor Module

async function loadIPPoolData() {
    try {
        const response = await fetch('/api/ip-pool/top?n=20');
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
            
            // Update table
            const tbody = document.getElementById('ip_pool_table_body');
            if (data.top_ips && data.top_ips.length > 0) {
                tbody.innerHTML = '';
                data.top_ips.forEach(ip => {
                    const row = document.createElement('tr');
                    row.className = 'hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors';
                    
                    // Format last access time
                    const lastAccess = new Date(ip.last_access);
                    const lastAccessStr = lastAccess.toLocaleString();
                    
                    // RTT color coding
                    let rttClass = 'text-green-600 dark:text-green-400';
                    if (ip.rtt > 100) rttClass = 'text-yellow-600 dark:text-yellow-400';
                    if (ip.rtt > 300) rttClass = 'text-red-600 dark:text-red-400';
                    if (ip.rtt === 0) rttClass = 'text-gray-400';
                    
                    row.innerHTML = `
                        <td class="px-6 py-3 font-mono">${ip.ip || '-'}</td>
                        <td class="px-6 py-3 truncate max-w-xs">${ip.rep_domain || '-'}</td>
                        <td class="px-6 py-3">${ip.ref_count || 0}</td>
                        <td class="px-6 py-3">${ip.access_heat || 0}</td>
                        <td class="px-6 py-3 font-mono ${rttClass}">${ip.rtt >= 0 ? ip.rtt : '-'}</td>
                        <td class="px-6 py-3 text-xs">${lastAccessStr}</td>
                    `;
                    tbody.appendChild(row);
                });
            } else {
                tbody.innerHTML = `
                    <tr>
                        <td colspan="6" class="px-6 py-4 text-center text-text-sub-light dark:text-text-sub-dark">
                            <span class="material-symbols-outlined inline-block align-middle mr-2">info</span>
                            <span data-i18n="dashboard.noIpData">No IP pool data available</span>
                        </td>
                    </tr>
                `;
            }
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
    // Re-apply translations when language changes
    if (window.i18n && typeof window.i18n.applyTranslations === 'function') {
        window.i18n.applyTranslations();
    }
});
