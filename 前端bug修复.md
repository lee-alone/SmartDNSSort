Bug 1：广告拦截状态关闭后刷新又显示已启用
问题原因
在 webapi/api_adblock.go 的 handleAdBlockToggle 函数中，只调用了 s.dnsServer.SetAdBlockEnabled() 方法，该方法只更新了 DNS 服务器内部的 s.cfg.AdBlock.Enable，但没有同步更新 AdBlockManager 内部的配置。

关键代码差异：

AdBlockManager 有自己的 m.cfg 引用（在 adblock/manager.go）
SetAdBlockEnabled() 只更新了 DNS Server 的 s.cfg
AdBlockManager.CheckHost() 使用的是自己的 m.cfg.Enable 来判断是否启用拦截
所以热更新时内存中的状态不一致了，只有重启服务才会重新初始化 AdBlockManager。

Bug 2：官方订阅源状态显示为 failed 但规则已加载成功
问题原因
在 adblock/source_manager.go 的 UpdateSourceStatus() 方法中，每次更新状态时都会在出错时设置 Status = "failed"。但是这个方法在多个地方被调用，包括初始规则加载失败时。

即使规则后来成功加载，由于状态已经被标记为 "failed"，并且 UpdateSourceStatus 只在更新时才可能重置，导致状态一直显示为 "failed"。而实际上规则文件已经成功下载和解析（从仪表盘可以看到10多万条规则）。

修复方案
修复 Bug 1（状态同步问题）
需要同时修改 DNS Server 和 AdBlockManager 的配置：

api_adblock.go

Apply
// ... existing code ...
	// Update in-memory config
	s.dnsServer.SetAdBlockEnabled(payload.Enabled)

	// Also update AdBlockManager's internal config
	adblockMgr := s.dnsServer.GetAdBlockManager()
	if adblockMgr != nil {
		adblockMgr.SetEnabled(payload.Enabled)
	}
// ... existing code ...
然后在 adblock/manager.go 中添加 SetEnabled 方法：

manager.go

Apply
// SetEnabled dynamically enables or disables AdBlock filtering
func (m *AdBlockManager) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.Enable = enabled
}

func (m *AdBlockManager) CheckHost(domain string) (bool, string) {
// ... existing code ...
修复 Bug 2（状态显示问题）
在 adblock/manager.go 的 UpdateRules 方法中，开始更新前先将所有源的状态重置为 "pending" 或 "updating"：

manager.go

Apply
func (m *AdBlockManager) UpdateRules(force bool) (UpdateResult, error) {
	startTime := time.Now()

	// Reset source statuses before updating
	sources := m.sourcesMgr.GetAllSources()
	for _, source := range sources {
		if source.Enabled {
			m.sourcesMgr.UpdateSourceStatus(source.URL, source.RuleCount, nil) // Reset to active
		}
	}

	// Phase 1: Prepare - Download and parse rules WITHOUT holding the lock
// ... existing code ...
同时在 adblock/source_manager.go 中改进 UpdateSourceStatus 方法，避免在初始加载时错误标记为 failed：

source_manager.go

Apply
func (sm *SourceManager) UpdateSourceStatus(url string, ruleCount int, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if source, exists := sm.sources[url]; exists {
		source.LastUpdate = time.Now()
		source.RuleCount = ruleCount
		if err != nil {
			source.LastError = err.Error()
			source.FailCount++
			// Only mark as failed if this is not the initial load (ruleCount == 0)
			if source.RuleCount > 0 {
				source.Status = "failed"
				if source.FailCount >= 3 {
					source.Status = "bad"
				}
			} else {
				// Initial loading, don't mark as failed yet
				source.Status = "initializing"
			}
		} else {
			source.LastError = ""
			source.FailCount = 0
			source.Status = "active"
		}
	}
}
请修复。