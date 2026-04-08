// Main Application Entry Point
// This file loads all modular components

// Modules are loaded in order of dependency:
// 1. utils.js - Utility functions used by other modules
// 2. navigation.js - Navigation and view management
// 3. config.js - Configuration management
// 4. dashboard.js - Dashboard and statistics
// 5. adblock.js - AdBlock functionality
// 6. custom-settings.js - Custom settings management

// 认证状态管理
async function checkAuthStatus() {
    try {
        const response = await fetch('/api/auth-status', {
            method: 'GET',
            credentials: 'same-origin'
        });
        
        if (!response.ok) {
            console.error('Failed to fetch auth status:', response.status);
            showDashboard();
            return;
        }
        
        const data = await response.json();
        
        if (data.need_setup) {
            showSetupView();
        } else if (data.need_login) {
            showLoginView();
        } else {
            showDashboard();
        }
    } catch (error) {
        console.error('Error checking auth status:', error);
        showDashboard();
    }
}

// 显示初始化设置视图
function showSetupView() {
    // 隐藏侧边栏和页眉，让设置界面更纯净
    const sidebar = document.getElementById('sidebar-container');
    const header = document.getElementById('header-container');
    if (sidebar) sidebar.style.display = 'none';
    if (header) header.style.display = 'none';

    // 切换核心视图
    document.getElementById('view-setup').style.display = 'flex';
    document.getElementById('view-login').style.display = 'none';
    document.getElementById('view-dashboard').style.display = 'none';
    document.getElementById('view-config').style.display = 'none';
    document.getElementById('view-rules').style.display = 'none';

    // 显式触发翻译
    if (window.i18n && typeof window.i18n.translatePage === 'function') {
        window.i18n.translatePage();
    }

    // 设置默认端口
    const portInput = document.getElementById('setup-port');
    if (!portInput.value) {
        portInput.value = window.location.port || 8080;
    }

    // 绑定表单提交事件
    const form = document.getElementById('setup-form');
    form.onsubmit = handleSetup;
}

// 处理初始化设置
async function handleSetup(event) {
    event.preventDefault();
    
    const username = document.getElementById('setup-username').value.trim();
    const password = document.getElementById('setup-password').value;
    const confirmPassword = document.getElementById('setup-confirm-password').value;
    const port = parseInt(document.getElementById('setup-port').value);
    
    const errorDiv = document.getElementById('setup-error');
    const submitBtn = document.getElementById('setup-submit-btn');
    
    // 验证
    if (password !== confirmPassword) {
        errorDiv.textContent = '两次输入的密码不一致';
        errorDiv.classList.remove('hidden');
        return;
    }
    
    // 禁用按钮
    submitBtn.disabled = true;
    submitBtn.textContent = '处理中...';
    errorDiv.classList.add('hidden');

    try {
        // 在提交 setup 之前，主动拉取一次 CSRF Token
        const csrfResp = await fetch('/api/csrf-token', {
            method: 'GET',
            credentials: 'same-origin'
        });
        const csrfData = await csrfResp.json();
        const token = csrfData.data.csrf_token || '';

        const response = await fetch('/api/setup', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': token
            },
            credentials: 'same-origin',
            body: JSON.stringify({
                username: username,
                password: password,
                port: port
            })
        });
        
        const data = await response.json();
        
        if (!response.ok || !data.success) {
            throw new Error(data.message || '设置失败');
        }
        
        // 检查端口是否变化
        const oldPort = window.location.port ? parseInt(window.location.port) : 80;
        const newPort = port;
        const portChanged = newPort !== oldPort && newPort > 0;
        
        if (portChanged && data.data && data.data.restarting) {
            // 显示重启等待蒙层
            showSetupRestartOverlay(newPort);
        } else {
            // 端口未变化，直接刷新
            alert('初始设置完成！页面将刷新以应用新配置。');
            window.location.reload();
        }
    } catch (error) {
        errorDiv.textContent = error.message || '设置失败，请重试';
        errorDiv.classList.remove('hidden');
        submitBtn.disabled = false;
        submitBtn.textContent = '完成设置';
    }
}

// 显示设置重启等待蒙层并倒计时跳转
function showSetupRestartOverlay(newPort) {
    const overlay = document.getElementById('setup-restart-overlay');
    const countdownEl = document.getElementById('setup-countdown');
    overlay.style.display = 'flex';
    
    let seconds = 3;
    countdownEl.textContent = seconds;
    
    const interval = setInterval(() => {
        seconds--;
        if (seconds <= 0) {
            clearInterval(interval);
            // 构造新端口的 URL 并跳转
            const protocol = window.location.protocol;
            const hostname = window.location.hostname;
            const newPath = window.location.pathname;
            const newUrl = `${protocol}//${hostname}:${newPort}${newPath}`;
            window.location.href = newUrl;
        } else {
            countdownEl.textContent = seconds;
        }
    }, 1000);
}

// 显示登录视图
function showLoginView() {
    // 隐藏侧边栏和页眉，让登录界面更纯净
    const sidebar = document.getElementById('sidebar-container');
    const header = document.getElementById('header-container');
    if (sidebar) sidebar.style.display = 'none';
    if (header) header.style.display = 'none';

    // 切换核心视图
    document.getElementById('view-setup').style.display = 'none';
    document.getElementById('view-login').style.display = 'flex';
    document.getElementById('view-dashboard').style.display = 'none';
    document.getElementById('view-config').style.display = 'none';
    document.getElementById('view-rules').style.display = 'none';

    // 显式触发翻译
    if (window.i18n && typeof window.i18n.translatePage === 'function') {
        window.i18n.translatePage();
    }

    // 绑定表单提交事件
    const form = document.getElementById('login-form');
    form.onsubmit = handleLogin;
}

// 处理登录
async function handleLogin(event) {
    event.preventDefault();
    
    const username = document.getElementById('login-username').value.trim();
    const password = document.getElementById('login-password').value;
    
    const errorDiv = document.getElementById('login-error');
    const submitBtn = document.getElementById('login-submit-btn');
    
    // 禁用按钮
    submitBtn.disabled = true;
    submitBtn.textContent = '登录中...';
    errorDiv.classList.add('hidden');
    
    try {
        // 先获取 CSRF token
        const csrfResponse = await fetch('/api/csrf-token', {
            method: 'GET',
            credentials: 'same-origin'
        });
        const csrfData = await csrfResponse.json();
        
        // 登录请求
        const response = await fetch('/api/login', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': csrfData.data.csrf_token || ''
            },
            credentials: 'same-origin',
            body: JSON.stringify({
                username: username,
                password: password
            })
        });
        
        const data = await response.json();

        if (!response.ok || !data.success) {
            throw new Error(data.message || '登录失败');
        }

        // [最稳妥修复] 登录成功后，使用温和刷新
        // 这能清除所有模块由于 401 报错导致的内部"脏状态"
        // 刷新后 checkAuthStatus() 会重新执行，此时 Cookie 已生效，所有组件以"已认证"身份加载
        window.location.reload();
    } catch (error) {
        errorDiv.textContent = error.message || '登录失败，请重试';
        errorDiv.classList.remove('hidden');
    } finally {
        submitBtn.disabled = false;
        submitBtn.textContent = '登录';
    }
}

// 显示 Dashboard
function showDashboard() {
    // 显示全局布局组件
    const sidebar = document.getElementById('sidebar-container');
    const header = document.getElementById('header-container');
    if (sidebar) sidebar.style.display = 'block';
    if (header) header.style.display = 'block';

    // 切换核心视图
    document.getElementById('view-setup').style.display = 'none';
    document.getElementById('view-login').style.display = 'none';
    document.getElementById('view-dashboard').style.display = 'block';
    document.getElementById('view-config').style.display = 'none';
    document.getElementById('view-rules').style.display = 'none';

    // 设置全局认证状态标旗
    window.isAuthenticated = true;

    // [新增] 重新检查组件是否加载完整（防止之前因 401 导致的加载失败）
    if (window.ComponentLoader && typeof window.ComponentLoader.reloadMissing === 'function') {
        window.ComponentLoader.reloadMissing();
    }

    // 显式触发全局翻译（重要！）
    if (window.i18n && typeof window.i18n.translatePage === 'function') {
        window.i18n.translatePage();
    }

    // 登录成功后，统一调度各模块的数据加载
    if (typeof loadConfig === 'function') {
        loadConfig();
    }
    if (typeof loadCustomSettings === 'function') {
        loadCustomSettings();
    }
    if (typeof loadStats === 'function') {
        loadStats();
    }
    if (typeof loadRules === 'function') {
        loadRules();
    }
}

// 处理登出
async function handleLogout() {
    try {
        await fetch('/api/logout', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': window.csrfToken || ''
            },
            credentials: 'same-origin'
        });
        
        // 刷新页面以显示登录视图
        window.location.reload();
    } catch (error) {
        console.error('Logout error:', error);
        window.location.reload();
    }
}

// 等待组件加载完成后初始化认证状态
document.addEventListener('componentsLoaded', () => {
    // 首先检查认证状态，只有登录成功后才允许加载业务数据
    checkAuthStatus();
});
