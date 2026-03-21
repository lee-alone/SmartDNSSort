// 简单的 SVG 图表模块

// 绘制缓存命中率环形图
function renderCacheHitRateChart(containerId, hitRate) {
    const container = document.getElementById(containerId);
    if (!container) return;
    
    const size = 120;
    const strokeWidth = 10;
    const radius = (size - strokeWidth) / 2;
    const circumference = 2 * Math.PI * radius;
    const offset = circumference - (hitRate / 100) * circumference;
    
    container.innerHTML = `
        <svg width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">
            <!-- 背景圆 -->
            <circle
                cx="${size/2}"
                cy="${size/2}"
                r="${radius}"
                fill="none"
                stroke="#e5e7eb"
                stroke-width="${strokeWidth}"
            />
            <!-- 进度圆 -->
            <circle
                cx="${size/2}"
                cy="${size/2}"
                r="${radius}"
                fill="none"
                stroke="#22c55e"
                stroke-width="${strokeWidth}"
                stroke-dasharray="${circumference}"
                stroke-dashoffset="${offset}"
                stroke-linecap="round"
                transform="rotate(-90 ${size/2} ${size/2})"
                class="transition-all duration-500"
            />
            <!-- 中心文字 -->
            <text
                x="${size/2}"
                y="${size/2}"
                text-anchor="middle"
                dominant-baseline="middle"
                class="text-lg font-bold fill-text-main-light dark:fill-text-main-dark"
            >
                ${hitRate.toFixed(1)}%
            </text>
        </svg>
    `;
}

// 绘制查询趋势迷你图
function renderQueryTrendChart(containerId, dataPoints) {
    const container = document.getElementById(containerId);
    if (!container || !dataPoints || dataPoints.length === 0) return;
    
    const width = 200;
    const height = 60;
    const padding = 5;
    
    const maxValue = Math.max(...dataPoints);
    const minValue = Math.min(...dataPoints);
    const range = maxValue - minValue || 1;
    
    const points = dataPoints.map((value, index) => {
        const x = padding + (index / (dataPoints.length - 1)) * (width - 2 * padding);
        const y = height - padding - ((value - minValue) / range) * (height - 2 * padding);
        return `${x},${y}`;
    }).join(' ');
    
    container.innerHTML = `
        <svg width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">
            <polyline
                points="${points}"
                fill="none"
                stroke="#3b82f6"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
            />
        </svg>
    `;
}

// 导出函数
window.Charts = {
    renderCacheHitRateChart,
    renderQueryTrendChart
};