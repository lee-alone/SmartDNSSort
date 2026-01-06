// Resolver 模块 - 处理递归解析器的前端逻辑

class ResolverModule {
  constructor() {
    this.statusRefreshInterval = null;
    this.statsRefreshInterval = null;
    this.isInitialized = false;
  }

  // 初始化模块
  async init() {
    if (this.isInitialized) return;

    this.attachEventListeners();
    await this.refreshStatus();
    await this.refreshStats();

    // 定时刷新状态和统计信息
    this.statusRefreshInterval = setInterval(() => this.refreshStatus(), 5000);
    this.statsRefreshInterval = setInterval(() => this.refreshStats(), 5000);

    // 检查是否需要显示自动提示
    this.checkAutoHint();

    this.isInitialized = true;
  }

  // 清理资源
  cleanup() {
    if (this.statusRefreshInterval) {
      clearInterval(this.statusRefreshInterval);
      this.statusRefreshInterval = null;
    }
    if (this.statsRefreshInterval) {
      clearInterval(this.statsRefreshInterval);
      this.statsRefreshInterval = null;
    }
    this.isInitialized = false;
  }

  // 绑定事件监听器
  attachEventListeners() {
    const startBtn = document.getElementById('resolver-start-btn');
    const stopBtn = document.getElementById('resolver-stop-btn');
    const restartBtn = document.getElementById('resolver-restart-btn');
    const clearStatsBtn = document.getElementById('resolver-clear-stats-btn');
    const traceBtn = document.getElementById('resolver-trace-btn');
    const hintActionBtn = document.getElementById('resolver-hint-action-btn');

    if (startBtn) startBtn.addEventListener('click', () => this.controlResolver('start'));
    if (stopBtn) stopBtn.addEventListener('click', () => this.controlResolver('stop'));
    if (restartBtn) restartBtn.addEventListener('click', () => this.controlResolver('restart'));
    if (clearStatsBtn) clearStatsBtn.addEventListener('click', () => this.clearStats());
    if (traceBtn) traceBtn.addEventListener('click', () => this.traceResolve());
    if (hintActionBtn) hintActionBtn.addEventListener('click', () => this.handleHintAction());
  }

  // 刷新状态
  async refreshStatus() {
    try {
      const response = await fetch('/api/resolver/status');
      const data = await response.json();

      if (data.success && data.data) {
        this.updateStatusUI(data.data);
      }
    } catch (error) {
      console.error('Failed to refresh resolver status:', error);
    }
  }

  // 刷新统计信息
  async refreshStats() {
    try {
      const response = await fetch('/api/resolver/stats');
      const data = await response.json();

      if (data.success && data.data) {
        this.updateStatsUI(data.data);
      }
    } catch (error) {
      console.error('Failed to refresh resolver stats:', error);
    }
  }

  // 更新状态 UI
  updateStatusUI(status) {
    const statusBadge = document.getElementById('resolver-status-badge');
    const statusText = document.getElementById('resolver-status-text');
    const uptime = document.getElementById('resolver-uptime');
    const port = document.getElementById('resolver-port');
    const queryTimeout = document.getElementById('resolver-query-timeout');

    // 更新状态徽章
    if (statusBadge) {
      statusBadge.textContent = status.status === 'running' ? '运行中' : '已停止';
      statusBadge.className = status.status === 'running' 
        ? 'px-4 py-2 rounded-full text-white text-sm font-semibold bg-green-500'
        : 'px-4 py-2 rounded-full text-white text-sm font-semibold bg-red-500';
    }

    // 更新状态文本
    if (statusText) {
      statusText.textContent = status.status === 'running' ? '运行中' : '已停止';
    }

    // 更新运行时间
    if (uptime) {
      uptime.textContent = status.uptime || '-';
    }

    // 更新端口
    if (port) {
      port.textContent = status.port || '-';
    }

    // 更新查询超时
    if (queryTimeout) {
      queryTimeout.textContent = (status.query_timeout || 0) + 'ms';
    }
  }

  // 更新统计 UI
  updateStatsUI(stats) {
    const totalQueries = document.getElementById('resolver-total-queries');
    const successQueries = document.getElementById('resolver-success-queries');
    const failedQueries = document.getElementById('resolver-failed-queries');
    const successRate = document.getElementById('resolver-success-rate');
    const avgLatency = document.getElementById('resolver-avg-latency');
    const cacheHitRate = document.getElementById('resolver-cache-hit-rate');

    // 更新查询统计
    if (totalQueries) totalQueries.textContent = stats.total_queries || 0;
    if (successQueries) successQueries.textContent = stats.success_queries || 0;
    if (failedQueries) failedQueries.textContent = stats.failed_queries || 0;

    // 更新成功率
    if (successRate) {
      const rate = stats.success_rate || 0;
      successRate.textContent = rate.toFixed(1) + '%';
    }

    // 更新平均延迟
    if (avgLatency) {
      const latency = stats.avg_latency_ms || 0;
      avgLatency.textContent = latency.toFixed(1) + 'ms';
    }

    // 更新缓存命中率
    if (cacheHitRate) {
      const rate = stats.cache_hit_rate || 0;
      cacheHitRate.textContent = rate.toFixed(1) + '%';
    }

    // 更新性能对比
    this.updatePerformanceComparison(stats);
  }

  // 更新性能对比
  updatePerformanceComparison(stats) {
    const localAvgLatency = document.getElementById('local-avg-latency');
    const localCacheHitRate = document.getElementById('local-cache-hit-rate');

    if (localAvgLatency) {
      const latency = stats.avg_latency_ms || 0;
      localAvgLatency.textContent = latency.toFixed(1) + 'ms';
    }

    if (localCacheHitRate) {
      const rate = stats.cache_hit_rate || 0;
      localCacheHitRate.textContent = rate.toFixed(1) + '%';
    }

    // 外部 DNS 的数据可以从其他 API 获取
    // 这里暂时使用占位符
    const externalAvgLatency = document.getElementById('external-avg-latency');
    const externalCacheHitRate = document.getElementById('external-cache-hit-rate');

    if (externalAvgLatency) {
      externalAvgLatency.textContent = '120ms'; // 示例值
    }

    if (externalCacheHitRate) {
      externalCacheHitRate.textContent = '45%'; // 示例值
    }
  }

  // 控制递归解析器
  async controlResolver(action) {
    try {
      const response = await fetch('/api/resolver/control', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ action }),
      });

      const data = await response.json();

      if (data.success) {
        this.showNotification(`递归解析器${action === 'start' ? '已启动' : action === 'stop' ? '已停止' : '已重启'}`, 'success');
        await this.refreshStatus();
        await this.refreshStats();
      } else {
        this.showNotification(`操作失败: ${data.message}`, 'error');
      }
    } catch (error) {
      console.error('Failed to control resolver:', error);
      this.showNotification('操作失败，请检查网络连接', 'error');
    }
  }

  // 清空统计信息
  async clearStats() {
    if (!confirm('确定要清空统计信息吗？')) {
      return;
    }

    try {
      const response = await fetch('/api/resolver/stats/clear', {
        method: 'POST',
      });

      const data = await response.json();

      if (data.success) {
        this.showNotification('统计信息已清空', 'success');
        await this.refreshStats();
      } else {
        this.showNotification(`清空失败: ${data.message}`, 'error');
      }
    } catch (error) {
      console.error('Failed to clear stats:', error);
      this.showNotification('清空失败，请检查网络连接', 'error');
    }
  }

  // 迭代路径跟踪
  async traceResolve() {
    const domain = document.getElementById('resolver-trace-domain').value.trim();
    const type = document.getElementById('resolver-trace-type').value;

    if (!domain) {
      this.showNotification('请输入要查询的域名', 'warning');
      return;
    }

    try {
      const response = await fetch('/api/resolver/trace', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ domain, type }),
      });

      const data = await response.json();

      if (data.success) {
        this.displayTraceResult(data.data);
      } else {
        this.showNotification(`查询失败: ${data.message}`, 'error');
      }
    } catch (error) {
      console.error('Failed to trace resolve:', error);
      this.showNotification('查询失败，请检查网络连接', 'error');
    }
  }

  // 显示迭代路径结果
  displayTraceResult(trace) {
    const resultContainer = document.getElementById('resolver-trace-result');
    const output = document.getElementById('resolver-trace-output');

    if (!resultContainer || !output) return;

    // 格式化输出
    let text = '';
    if (typeof trace === 'string') {
      text = trace;
    } else if (typeof trace === 'object') {
      text = JSON.stringify(trace, null, 2);
    }

    output.textContent = text;
    resultContainer.classList.remove('hidden');
  }

  // 检查自动提示
  async checkAutoHint() {
    try {
      const response = await fetch('/api/resolver/status');
      const data = await response.json();

      if (data.success && data.data) {
        const status = data.data;

        // 如果递归解析器未启用，显示提示
        if (!status.enabled) {
          this.showAutoHint(
            '递归解析器未启用。启用本地递归解析可以提高隐私保护和查询速度。',
            'enable'
          );
        } else if (status.status !== 'running') {
          this.showAutoHint(
            '递归解析器已启用但未运行。点击下方按钮启动它。',
            'start'
          );
        }
      }
    } catch (error) {
      console.error('Failed to check auto hint:', error);
    }
  }

  // 显示自动提示
  showAutoHint(message, action) {
    const hintContainer = document.getElementById('resolver-auto-hint');
    const hintText = document.getElementById('resolver-hint-text');
    const hintActionBtn = document.getElementById('resolver-hint-action-btn');

    if (!hintContainer || !hintText) return;

    hintText.textContent = message;
    hintContainer.dataset.action = action;
    hintContainer.classList.remove('hidden');
  }

  // 处理提示操作
  async handleHintAction() {
    const hintContainer = document.getElementById('resolver-auto-hint');
    const action = hintContainer.dataset.action;

    if (action === 'enable') {
      // 启用递归解析器
      await this.controlResolver('start');
    } else if (action === 'start') {
      // 启动递归解析器
      await this.controlResolver('start');
    }

    hintContainer.classList.add('hidden');
  }

  // 显示通知
  showNotification(message, type = 'info') {
    // 这里可以集成通知系统
    console.log(`[${type.toUpperCase()}] ${message}`);

    // 简单的 alert 实现
    if (type === 'error') {
      alert(`❌ ${message}`);
    } else if (type === 'success') {
      alert(`✅ ${message}`);
    } else if (type === 'warning') {
      alert(`⚠️ ${message}`);
    }
  }
}

// 导出模块
window.ResolverModule = ResolverModule;
