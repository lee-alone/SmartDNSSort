// Configuration Sidebar Navigation and Collapsible Sections

function initConfigSidebar() {
    const navButtons = document.querySelectorAll('.config-nav-btn');
    
    if (navButtons.length === 0) {
        console.warn('No config navigation buttons found');
        return;
    }
    
    navButtons.forEach(button => {
        button.addEventListener('click', (e) => {
            e.preventDefault();
            const sectionId = button.dataset.section;
            if (sectionId) {
                scrollToSection(sectionId);
                updateActiveNavButton(button);
            }
        });
    });
}

function scrollToSection(sectionId) {
    const section = document.getElementById(`section-${sectionId}`);
    if (section) {
        section.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
}

function updateActiveNavButton(activeButton) {
    document.querySelectorAll('.config-nav-btn').forEach(btn => {
        btn.classList.remove('active', 'bg-primary', 'text-black');
        btn.classList.add('text-text-sub-light', 'dark:text-text-sub-dark', 'hover:bg-black/5', 'dark:hover:bg-white/5');
    });
    
    activeButton.classList.add('active', 'bg-primary', 'text-black');
    activeButton.classList.remove('text-text-sub-light', 'dark:text-text-sub-dark', 'hover:bg-black/5', 'dark:hover:bg-white/5');
}

function toggleConfigSection(headerElement) {
    const section = headerElement.closest('.config-section');
    if (!section) return;
    
    const content = section.querySelector('.section-content');
    const icon = headerElement.querySelector('.section-toggle-icon');
    
    if (!content) return;
    if (!icon) return;
    
    const isExpanded = content.style.display !== 'none';
    
    if (isExpanded) {
        // Collapse - show expand_more (▼) to indicate it can be expanded
        content.style.display = 'none';
        icon.textContent = 'expand_more';
        icon.style.transform = 'rotate(0deg)';
    } else {
        // Expand - show expand_less (▲) to indicate it can be collapsed
        content.style.display = '';
        icon.textContent = 'expand_less';
        icon.style.transform = 'rotate(0deg)';
    }
}

// Initialize on component load
document.addEventListener('componentsLoaded', () => {
    initConfigSidebar();
    
    // Expand all sections by default
    document.querySelectorAll('.config-section').forEach(section => {
        const content = section.querySelector('.section-content');
        if (content) {
            content.style.display = '';
        }
    });
});
