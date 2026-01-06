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

    // Update active state on nav buttons (both desktop and mobile)
    document.querySelectorAll('.nav-button').forEach(button => {
        button.classList.remove('bg-primary', 'text-black', 'shadow-sm');
        button.classList.add('text-text-sub-light', 'dark:text-text-sub-dark', 'hover:bg-black/5', 'dark:hover:bg-white/5');
    });

    const viewName = viewId.split('-')[1];
    const activeButton = document.getElementById(`nav-${viewName}`);
    const activeMobileButton = document.getElementById(`nav-${viewName}-mobile`);
    
    if (activeButton) {
        activeButton.classList.add('bg-primary', 'text-black', 'shadow-sm');
        activeButton.classList.remove('text-text-sub-light', 'dark:text-text-sub-dark', 'hover:bg-black/5', 'dark:hover:bg-white/5');
    }
    if (activeMobileButton) {
        activeMobileButton.classList.add('bg-primary', 'text-black', 'shadow-sm');
        activeMobileButton.classList.remove('text-text-sub-light', 'dark:text-text-sub-dark', 'hover:bg-black/5', 'dark:hover:bg-white/5');
    }

    // Close mobile menu after navigation
    const mobileNav = document.getElementById('mobile-nav');
    if (mobileNav) {
        mobileNav.style.display = 'none';
    }

    // Initialize module-specific logic
    if (viewId === 'view-resolver' && window.resolverModule) {
        window.resolverModule.init();
    }
}

function toggleMobileMenu() {
    const mobileNav = document.getElementById('mobile-nav');
    if (mobileNav) {
        mobileNav.style.display = mobileNav.style.display === 'none' ? 'block' : 'none';
    }
}

function initializeNavigation() {
    const navDashboard = document.getElementById('nav-dashboard');
    const navConfig = document.getElementById('nav-config');
    const navRules = document.getElementById('nav-rules');
    const navResolver = document.getElementById('nav-resolver');
    
    const navDashboardMobile = document.getElementById('nav-dashboard-mobile');
    const navConfigMobile = document.getElementById('nav-config-mobile');
    const navRulesMobile = document.getElementById('nav-rules-mobile');
    const navResolverMobile = document.getElementById('nav-resolver-mobile');
    
    const mobileMenuToggle = document.getElementById('mobileMenuToggle');

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
    if (navResolver) {
        navResolver.addEventListener('click', (e) => {
            e.preventDefault();
            showView('view-resolver');
        });
    }

    // Mobile navigation
    if (navDashboardMobile) {
        navDashboardMobile.addEventListener('click', (e) => {
            e.preventDefault();
            showView('view-dashboard');
        });
    }
    if (navConfigMobile) {
        navConfigMobile.addEventListener('click', (e) => {
            e.preventDefault();
            showView('view-config');
            updateAdBlockTab();
        });
    }
    if (navRulesMobile) {
        navRulesMobile.addEventListener('click', (e) => {
            e.preventDefault();
            showView('view-rules');
        });
    }
    if (navResolverMobile) {
        navResolverMobile.addEventListener('click', (e) => {
            e.preventDefault();
            showView('view-resolver');
        });
    }

    // Mobile menu toggle
    if (mobileMenuToggle) {
        mobileMenuToggle.addEventListener('click', toggleMobileMenu);
    }

    // Initialize Resolver module
    if (window.ResolverModule) {
        window.resolverModule = new ResolverModule();
    }

    // Show dashboard by default
    showView('view-dashboard');
}

document.addEventListener('componentsLoaded', initializeNavigation);
