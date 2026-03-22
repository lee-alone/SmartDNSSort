/**
 * Accessibility Enhancement Module
 * Improves accessibility for screen readers and keyboard navigation
 */

const AccessibilityEnhancer = (function() {
    // 配置
    const CONFIG = {
        // 图标按钮的 ARIA 标签映射
        iconButtonLabels: {
            'refreshButton': { label: 'Refresh dashboard', labelZh: '刷新仪表板' },
            'restartButton': { label: 'Restart service', labelZh: '重启服务' },
            'themeToggle': { label: 'Toggle dark/light theme', labelZh: '切换深色/浅色主题' },
            'menuToggle': { label: 'Toggle navigation menu', labelZh: '切换导航菜单' },
            'saveConfig': { label: 'Save configuration', labelZh: '保存配置' },
            'resetConfig': { label: 'Reset configuration', labelZh: '重置配置' },
            'exportConfig': { label: 'Export configuration', labelZh: '导出配置' },
            'importConfig': { label: 'Import configuration', labelZh: '导入配置' },
            'addRule': { label: 'Add new rule', labelZh: '添加新规则' },
            'removeRule': { label: 'Remove rule', labelZh: '删除规则' },
            'editRule': { label: 'Edit rule', labelZh: '编辑规则' },
            'closeModal': { label: 'Close dialog', labelZh: '关闭对话框' },
            'expandSection': { label: 'Expand section', labelZh: '展开部分' },
            'collapseSection': { label: 'Collapse section', labelZh: '折叠部分' },
        },
        
        // 状态标签
        statusLabels: {
            loading: { label: 'Loading...', labelZh: '加载中...' },
            success: { label: 'Operation successful', labelZh: '操作成功' },
            error: { label: 'Operation failed', labelZh: '操作失败' },
            warning: { label: 'Warning', labelZh: '警告' },
        }
    };

    /**
     * 初始化可访问性增强
     */
    function init() {
        enhanceIconButtons();
        enhanceFormElements();
        enhanceLiveRegions();
        enhanceKeyboardNavigation();
        enhanceFocusIndicators();
    }

    /**
     * 增强图标按钮的可访问性
     */
    function enhanceIconButtons() {
        // 查找所有图标按钮
        const iconButtons = document.querySelectorAll('button[class*="icon"], button svg, .icon-button, [role="button"]');
        
        iconButtons.forEach(button => {
            // 找到实际的按钮元素
            const targetButton = button.tagName === 'BUTTON' ? button : button.closest('button');
            if (!targetButton) return;

            // 如果按钮没有文本内容，添加 aria-label
            if (!targetButton.textContent.trim() || targetButton.querySelector('svg, i, img')) {
                const buttonId = targetButton.id || targetButton.className;
                const labelInfo = findLabelForButton(targetButton);
                
                if (labelInfo && !targetButton.getAttribute('aria-label')) {
                    targetButton.setAttribute('aria-label', labelInfo);
                }
            }

            // 确保 button 有正确的 role
            if (targetButton.tagName !== 'BUTTON') {
                targetButton.setAttribute('role', 'button');
            }

            // 添加键盘支持
            if (!targetButton.hasAttribute('tabindex')) {
                targetButton.setAttribute('tabindex', '0');
            }
        });
    }

    /**
     * 查找按钮的标签
     */
    function findLabelForButton(button) {
        const buttonId = button.id;
        const buttonClass = button.className;
        const lang = document.documentElement.lang || 'en';
        const isZh = lang.startsWith('zh');

        // 检查 ID 匹配
        if (buttonId && CONFIG.iconButtonLabels[buttonId]) {
            return isZh ? CONFIG.iconButtonLabels[buttonId].labelZh : CONFIG.iconButtonLabels[buttonId].label;
        }

        // 检查类名匹配
        for (const [key, labels] of Object.entries(CONFIG.iconButtonLabels)) {
            if (buttonClass.includes(key)) {
                return isZh ? labels.labelZh : labels.label;
            }
        }

        // 检查按钮内的图标
        const svg = button.querySelector('svg');
        if (svg) {
            const title = svg.querySelector('title');
            if (title) {
                return title.textContent;
            }
        }

        // 检查 title 属性
        if (button.title) {
            return button.title;
        }

        return null;
    }

    /**
     * 增强表单元素的可访问性
     */
    function enhanceFormElements() {
        // 查找所有没有 label 关联的输入框
        const inputs = document.querySelectorAll('input, select, textarea');
        
        inputs.forEach(input => {
            // 跳过隐藏输入和提交按钮
            if (input.type === 'hidden' || input.type === 'submit' || input.type === 'button') {
                return;
            }

            // 检查是否有关联的 label
            const id = input.id;
            let hasLabel = false;

            if (id) {
                const label = document.querySelector(`label[for="${id}"]`);
                hasLabel = !!label;
            }

            // 如果没有 label，尝试从 placeholder 或 aria-label 获取
            if (!hasLabel && !input.getAttribute('aria-label')) {
                const placeholder = input.placeholder;
                const name = input.name;
                
                if (placeholder) {
                    input.setAttribute('aria-label', placeholder);
                } else if (name) {
                    // 将 name 转换为更友好的标签
                    const friendlyName = name
                        .replace(/_/g, ' ')
                        .replace(/([A-Z])/g, ' $1')
                        .toLowerCase()
                        .trim();
                    input.setAttribute('aria-label', friendlyName);
                }
            }

            // 添加 required 属性的 aria-required
            if (input.required && !input.hasAttribute('aria-required')) {
                input.setAttribute('aria-required', 'true');
            }

            // 添加 invalid 状态的 aria-invalid
            if (input.validity && !input.validity.valid) {
                input.setAttribute('aria-invalid', 'true');
            }
        });
    }

    /**
     * 创建实时区域用于屏幕阅读器通知
     */
    function enhanceLiveRegions() {
        // 创建状态通知区域
        let statusRegion = document.getElementById('a11y-status-region');
        if (!statusRegion) {
            statusRegion = document.createElement('div');
            statusRegion.id = 'a11y-status-region';
            statusRegion.setAttribute('role', 'status');
            statusRegion.setAttribute('aria-live', 'polite');
            statusRegion.setAttribute('aria-atomic', 'true');
            statusRegion.style.cssText = `
                position: absolute;
                width: 1px;
                height: 1px;
                padding: 0;
                margin: -1px;
                overflow: hidden;
                clip: rect(0, 0, 0, 0);
                white-space: nowrap;
                border: 0;
            `;
            document.body.appendChild(statusRegion);
        }

        // 创建警报区域
        let alertRegion = document.getElementById('a11y-alert-region');
        if (!alertRegion) {
            alertRegion = document.createElement('div');
            alertRegion.id = 'a11y-alert-region';
            alertRegion.setAttribute('role', 'alert');
            alertRegion.setAttribute('aria-live', 'assertive');
            alertRegion.setAttribute('aria-atomic', 'true');
            alertRegion.style.cssText = `
                position: absolute;
                width: 1px;
                height: 1px;
                padding: 0;
                margin: -1px;
                overflow: hidden;
                clip: rect(0, 0, 0, 0);
                white-space: nowrap;
                border: 0;
            `;
            document.body.appendChild(alertRegion);
        }
    }

    /**
     * 增强键盘导航
     */
    function enhanceKeyboardNavigation() {
        // 为可点击元素添加键盘支持
        document.addEventListener('keydown', (e) => {
            // Enter 和 Space 键激活
            if (e.key === 'Enter' || e.key === ' ') {
                const target = e.target;
                
                // 检查是否是可点击的非按钮元素
                if (target.getAttribute('role') === 'button' && target.tagName !== 'BUTTON') {
                    e.preventDefault();
                    target.click();
                }
            }

            // Escape 键关闭模态框
            if (e.key === 'Escape') {
                const modal = document.querySelector('[role="dialog"], .modal, .modal-overlay');
                if (modal) {
                    const closeBtn = modal.querySelector('[aria-label*="Close"], [aria-label*="关闭"], .close-btn, .modal-close');
                    if (closeBtn) {
                        closeBtn.click();
                    }
                }
            }
        });

        // 跳过链接
        addSkipLink();
    }

    /**
     * 添加跳过导航链接
     */
    function addSkipLink() {
        // 检查是否已存在
        if (document.getElementById('skip-to-main')) {
            return;
        }

        const skipLink = document.createElement('a');
        skipLink.id = 'skip-to-main';
        skipLink.href = '#main-content';
        skipLink.textContent = document.documentElement.lang?.startsWith('zh') 
            ? '跳转到主要内容' 
            : 'Skip to main content';
        skipLink.style.cssText = `
            position: absolute;
            top: -40px;
            left: 0;
            background: #3b82f6;
            color: white;
            padding: 8px 16px;
            z-index: 10001;
            text-decoration: none;
            border-radius: 0 0 4px 0;
            transition: top 0.2s;
        `;

        skipLink.addEventListener('focus', () => {
            skipLink.style.top = '0';
        });

        skipLink.addEventListener('blur', () => {
            skipLink.style.top = '-40px';
        });

        document.body.insertBefore(skipLink, document.body.firstChild);

        // 确保主内容区域有正确的 ID
        const mainContent = document.querySelector('main, [role="main"], #main-content');
        if (mainContent && !mainContent.id) {
            mainContent.id = 'main-content';
        }
    }

    /**
     * 增强焦点指示器
     */
    function enhanceFocusIndicators() {
        // 添加焦点样式
        const style = document.createElement('style');
        style.textContent = `
            /* 增强的焦点样式 */
            :focus-visible {
                outline: 2px solid #3b82f6;
                outline-offset: 2px;
            }
            
            /* 跳过链接样式 */
            #skip-to-main:focus {
                top: 0 !important;
            }
            
            /* 高对比度模式支持 */
            @media (prefers-contrast: high) {
                :focus {
                    outline: 3px solid currentColor;
                    outline-offset: 2px;
                }
            }
            
            /* 减少动画模式 */
            @media (prefers-reduced-motion: reduce) {
                * {
                    animation-duration: 0.01ms !important;
                    animation-iteration-count: 1 !important;
                    transition-duration: 0.01ms !important;
                }
            }
        `;
        document.head.appendChild(style);
    }

    /**
     * 向屏幕阅读器发送状态消息
     * @param {string} message - 消息内容
     * @param {string} type - 消息类型 ('status' 或 'alert')
     */
    function announce(message, type = 'status') {
        const regionId = type === 'alert' ? 'a11y-alert-region' : 'a11y-status-region';
        const region = document.getElementById(regionId);
        
        if (region) {
            // 清空并重新设置消息，确保屏幕阅读器捕获
            region.textContent = '';
            setTimeout(() => {
                region.textContent = message;
            }, 100);
        }
    }

    /**
     * 为动态内容添加可访问性属性
     * @param {HTMLElement} element - 要增强的元素
     * @param {Object} options - 配置选项
     */
    function enhanceElement(element, options = {}) {
        if (!element) return;

        // 添加 role
        if (options.role) {
            element.setAttribute('role', options.role);
        }

        // 添加 aria-label
        if (options.label) {
            element.setAttribute('aria-label', options.label);
        }

        // 添加 aria-expanded
        if (options.expanded !== undefined) {
            element.setAttribute('aria-expanded', String(options.expanded));
        }

        // 添加 aria-controls
        if (options.controls) {
            element.setAttribute('aria-controls', options.controls);
        }

        // 添加 aria-describedby
        if (options.describedBy) {
            element.setAttribute('aria-describedby', options.describedBy);
        }

        // 添加 aria-live
        if (options.live) {
            element.setAttribute('aria-live', options.live);
        }
    }

    /**
     * 更新加载状态
     * @param {boolean} isLoading - 是否正在加载
     * @param {string} message - 加载消息
     */
    function setLoadingState(isLoading, message) {
        const lang = document.documentElement.lang || 'en';
        const isZh = lang.startsWith('zh');
        const defaultLoadingMsg = isZh ? '加载中...' : 'Loading...';
        
        if (isLoading) {
            announce(message || defaultLoadingMsg, 'status');
            document.body.setAttribute('aria-busy', 'true');
        } else {
            document.body.removeAttribute('aria-busy');
        }
    }

    // 公开 API
    return {
        init,
        announce,
        enhanceElement,
        setLoadingState,
        enhanceIconButtons,
        enhanceFormElements,
    };
})();

// 导出模块
if (typeof module !== 'undefined' && module.exports) {
    module.exports = AccessibilityEnhancer;
}
