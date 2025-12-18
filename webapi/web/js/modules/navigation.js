// Navigation / View Management Module

function showView(viewId) {
    // Hide all main views
    document.querySelectorAll('.view-content').forEach(view => {
        view.style.display = 'none';
    });

    // Show the selected view
    const activeView = document.getElementById(viewId);
    if (activeView) {
        activeView.style.display = 'block';
    }

    // Update active state on nav buttons
    document.querySelectorAll('.nav-button').forEach(button => {
        button.classList.remove('bg-primary', 'text-black', 'shadow-sm');
        button.classList.add('text-text-sub-light', 'dark:text-text-sub-dark', 'hover:bg-black/5', 'dark:hover:bg-white/5');
    });

    const activeButton = document.getElementById(`nav-${viewId.split('-')[1]}`);
    if (activeButton) {
        activeButton.classList.add('bg-primary', 'text-black', 'shadow-sm');
        activeButton.classList.remove('text-text-sub-light', 'dark:text-text-sub-dark', 'hover:bg-black/5', 'dark:hover:bg-white/5');
    }
}

function initializeNavigation() {
    const navDashboard = document.getElementById('nav-dashboard');
    const navConfig = document.getElementById('nav-config');
    const navRules = document.getElementById('nav-rules');

    if (navDashboard) {
        navDashboard.addEventListener('click', (e) => {
            e.preventDefault();
            showView('view-dashboard');
        });
    }
    if (navConfig) {
        navConfig.addEventListener('click', (e) => {
            e.preventDefault();
            showView('view-config');
            updateAdBlockTab();
        });
    }
    if (navRules) {
        navRules.addEventListener('click', (e) => {
            e.preventDefault();
            showView('view-rules');
        });
    }

    // Show dashboard by default
    showView('view-dashboard');
}

document.addEventListener('componentsLoaded', initializeNavigation);
