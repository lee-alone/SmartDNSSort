package adblock

import (
	"context"
	"fmt"
	"os"
	"smartdnssort/config"
	"smartdnssort/logger"
	"strings"
	"sync"
	"time"
)

type AdBlockManager struct {
	cfg        *config.AdBlockConfig
	engine     FilterEngine
	sourcesMgr *SourceManager
	loader     *RuleLoader
	stats      *Stats
	mu         sync.RWMutex
	lastUpdate time.Time
}

func NewManager(cfg *config.AdBlockConfig) (*AdBlockManager, error) {
	var engine FilterEngine
	var err error

	switch strings.ToLower(cfg.Engine) {
	case "urlfilter":
		engine, err = NewURLFilterEngine()
		if err != nil {
			return nil, fmt.Errorf("error creating urlfilter engine: %w", err)
		}
	case "simple":
		engine = NewSimpleFilter()
	default:
		return nil, fmt.Errorf("unknown adblock engine: %s", cfg.Engine)
	}

	sourcesMgr, err := NewSourceManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("error creating source manager: %w", err)
	}

	loader := NewRuleLoader(cfg)
	stats := NewStats()

	return &AdBlockManager{
		cfg:        cfg,
		engine:     engine,
		sourcesMgr: sourcesMgr,
		loader:     loader,
		stats:      stats,
	}, nil
}

func (m *AdBlockManager) Start(ctx context.Context) {

	// Initial rule load
	go func() {
		logger.Info("[AdBlock] Loading existing rules from cache...")
		if err := m.LoadRulesFromCache(); err != nil {
			logger.Warnf("[AdBlock] Failed to load rules from cache: %v", err)
			// If no cached rules exist, force download on first start
			logger.Info("[AdBlock] No cached rules found, performing initial download...")
			result, err := m.UpdateRules(true)
			if err != nil {
				logger.Errorf("[AdBlock] Initial rules update failed: %v", err)
			} else {
				logger.Infof("[AdBlock] Initial update completed: %d total rules, %d sources updated", result.TotalRules, result.Sources)
				if len(result.FailedSources) > 0 {
					logger.Warnf("[AdBlock] Failed to update %d sources: %v", len(result.FailedSources), result.FailedSources)
				}
			}
		} else {
			logger.Info("[AdBlock] Successfully loaded rules from cache")
		}
	}()

	// Ticker for periodic updates
	if m.cfg.UpdateIntervalHours > 0 {
		ticker := time.NewTicker(time.Duration(m.cfg.UpdateIntervalHours) * time.Hour)
		go func() {
			for {
				select {
				case <-ticker.C:
					logger.Info("[AdBlock] Periodic rules update triggered")
					if _, err := m.UpdateRules(false); err != nil {
						logger.Errorf("[AdBlock] Periodic update failed: %v", err)
					}
				case <-ctx.Done():
					ticker.Stop()
					return
				}
			}
		}()
	}
}

func (m *AdBlockManager) UpdateRules(force bool) (UpdateResult, error) {
	startTime := time.Now()

	// Phase 1: Prepare - Download and parse rules WITHOUT holding the lock
	sources := m.sourcesMgr.GetAllSources()

	// Create a semaphore to limit concurrent downloads
	sem := make(chan struct{}, m.loader.maxConcurrent)

	var wg sync.WaitGroup
	failedSources := []string{}
	var mu sync.Mutex
	var totalRules, newRules, removedRules int

	for _, source := range sources {
		wg.Add(1)
		go func(s *SourceInfo) {
			defer wg.Done()

			if !s.Enabled {
				return
			}

			// Force mode: always download regardless of cache age
			// Non-force mode: only download if cache is stale
			if !force {
				// if cache is not too old, skip
				if time.Since(s.LastUpdate) < time.Duration(m.cfg.UpdateIntervalHours)*time.Hour {
					logger.Debugf("[AdBlock] Skipping source %s (cache is fresh)", s.URL)
					return
				}
			} else {
				logger.Debugf("[AdBlock] Force mode: scheduling download for %s", s.URL)
			}

			// Acquire semaphore slot
			sem <- struct{}{}
			defer func() { <-sem }()

			_, ruleCount, err := m.loader.UpdateFromSource(context.Background(), s)
			m.sourcesMgr.UpdateSourceStatus(s.URL, ruleCount, err)

			mu.Lock()
			if err != nil {
				failedSources = append(failedSources, s.URL)
			} else {
				// This is a simplification. A real diff would be more complex.
				if ruleCount > s.RuleCount {
					newRules += ruleCount - s.RuleCount
				} else {
					removedRules += s.RuleCount - ruleCount
				}
			}
			mu.Unlock()
		}(source)
	}
	wg.Wait()

	if err := m.sourcesMgr.saveMeta(); err != nil {
		// log error
	}

	// Phase 2: Load - Parse all rules into a new engine instance WITHOUT holding the lock
	// Get fresh source list after updates
	updatedSources := m.sourcesMgr.GetAllSources()
	allRules, err := m.loader.LoadAllRules(updatedSources)
	if err != nil {
		return UpdateResult{}, err
	}

	// Create a new engine instance with the updated rules
	var newEngine FilterEngine
	switch strings.ToLower(m.cfg.Engine) {
	case "urlfilter":
		var err error
		newEngine, err = NewURLFilterEngine()
		if err != nil {
			return UpdateResult{}, fmt.Errorf("error creating new urlfilter engine: %w", err)
		}
	case "simple":
		newEngine = NewSimpleFilter()
	default:
		return UpdateResult{}, fmt.Errorf("unknown adblock engine: %s", m.cfg.Engine)
	}

	if err := newEngine.LoadRules(allRules); err != nil {
		return UpdateResult{}, err
	}

	// Phase 3: Swap - Replace the engine with minimal lock holding time
	m.mu.Lock()
	m.engine = newEngine
	m.lastUpdate = time.Now()
	m.mu.Unlock()

	totalRules = m.engine.Count()
	if totalRules == 0 {
		totalRules = len(allRules)
	}

	return UpdateResult{
		TotalRules:      totalRules,
		NewRules:        newRules,
		RemovedRules:    removedRules,
		Sources:         len(sources),
		FailedSources:   failedSources,
		DurationSeconds: time.Since(startTime).Seconds(),
	}, nil
}

func (m *AdBlockManager) LoadRulesFromCache() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sources := m.sourcesMgr.GetAllSources()

	// If no sources configured, return error to trigger download
	if len(sources) == 0 {
		logger.Warn("[AdBlock] No sources configured, cannot load from cache")
		return fmt.Errorf("no sources configured")
	}

	// Check if there are any cached files
	var hasCachedFiles bool
	for _, source := range sources {
		if !source.Enabled {
			continue
		}

		// For local files, check if file exists
		if strings.HasPrefix(source.URL, "file://") || !strings.HasPrefix(source.URL, "http") {
			filePath := strings.TrimPrefix(source.URL, "file://")
			if _, err := os.Stat(filePath); err == nil {
				hasCachedFiles = true
				break
			}
		} else {
			// For remote files, check if cache file exists
			cachePath := fmt.Sprintf("%s/%s", m.loader.cacheDir, source.CacheFile)
			if _, err := os.Stat(cachePath); err == nil {
				hasCachedFiles = true
				break
			}
		}
	}

	if !hasCachedFiles {
		logger.Info("[AdBlock] No cached files found, will download rules on first update")
		return fmt.Errorf("no cached files found")
	}

	allRules, err := m.loader.LoadAllRules(sources)
	if err != nil {
		return err
	}

	// Only load rules if we have a reasonable number
	// If cache has very few rules (< 100), it's likely incomplete or corrupted
	// Trigger a fresh download to get complete rules
	if len(allRules) < 100 {
		logger.Warnf("[AdBlock] Cache has too few rules (%d), likely incomplete. Will trigger fresh download.", len(allRules))
		return fmt.Errorf("cache has insufficient rules: %d", len(allRules))
	}

	logger.Infof("[AdBlock] Loaded %d rules from cache", len(allRules))

	// Load rules into the engine
	if err := m.engine.LoadRules(allRules); err != nil {
		return err
	}

	// Update m.lastUpdate with the latest LastUpdate time from sources
	// This ensures the correct last update time is shown even when loading from cache
	var latestUpdate time.Time
	for _, source := range sources {
		if source.Enabled && source.LastUpdate.After(latestUpdate) {
			latestUpdate = source.LastUpdate
		}
	}
	m.lastUpdate = latestUpdate

	return nil
}

func (m *AdBlockManager) CheckHost(domain string) (MatchResult, string) {
	if !m.cfg.Enable {
		return MatchNeutral, ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.engine.CheckHost(domain)
}

// SetEnabled dynamically enables or disables AdBlock filtering
func (m *AdBlockManager) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.Enable = enabled
}

func (m *AdBlockManager) RecordBlock(domain, rule string) {
	m.stats.RecordBlock(domain, rule)
}

func (m *AdBlockManager) GetStats() AdBlockStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalRules := m.engine.Count()
	sources := m.sourcesMgr.GetAllSources()

	// Calculate total rules from all enabled sources
	// This is more accurate than relying on engine.Count() alone
	var calculatedTotal int
	for _, s := range sources {
		if s.Enabled && s.Status == "active" {
			calculatedTotal += s.RuleCount
		}
	}

	// Use the calculated total if engine count is 0 or if calculated is higher
	if totalRules == 0 || calculatedTotal > totalRules {
		totalRules = calculatedTotal
	}

	var failedSources []string
	for _, s := range sources {
		// Only count as failed if it's actually failed or bad, not initializing
		if (s.Status == "failed" || s.Status == "bad") && s.Enabled {
			failedSources = append(failedSources, s.URL)
		}
	}

	return m.stats.GetStats(m.cfg.Enable, m.cfg.Engine, totalRules, len(sources), failedSources, m.lastUpdate)
}

func (m *AdBlockManager) GetSources() []SourceStatus {
	return m.sourcesMgr.GetStatuses()
}

func (m *AdBlockManager) AddSource(url string) error {
	m.sourcesMgr.AddSource(url)
	return m.sourcesMgr.saveMeta()
}

func (m *AdBlockManager) RemoveSource(url string) error {
	m.sourcesMgr.RemoveSource(url)
	return m.sourcesMgr.saveMeta()
}

func (m *AdBlockManager) SetSourceEnabled(url string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sourcesMgr.SetEnabled(url, enabled)
	if err := m.sourcesMgr.saveMeta(); err != nil {
		return err
	}

	// Reload rules after enabling/disabling a source
	sources := m.sourcesMgr.GetAllSources()
	allRules, err := m.loader.LoadAllRules(sources)
	if err != nil {
		return err
	}

	// Create a new engine with updated rules
	var newEngine FilterEngine
	switch strings.ToLower(m.cfg.Engine) {
	case "urlfilter":
		var err error
		newEngine, err = NewURLFilterEngine()
		if err != nil {
			return fmt.Errorf("error creating new urlfilter engine: %w", err)
		}
	case "simple":
		newEngine = NewSimpleFilter()
	default:
		return fmt.Errorf("unknown adblock engine: %s", m.cfg.Engine)
	}

	if err := newEngine.LoadRules(allRules); err != nil {
		return err
	}

	// Replace the engine
	m.engine = newEngine
	return nil
}

type TestResult struct {
	Domain string `json:"domain"`
	Status string `json:"status"` // "blocked", "allowed", "neutral"
	Rule   string `json:"rule"`
	Source string `json:"source"`
}

func (m *AdBlockManager) TestDomain(domain string) TestResult {
	result, rule := m.CheckHost(domain)
	status := "neutral"
	switch result {
	case MatchBlocked:
		status = "blocked"
	case MatchAllowed:
		status = "allowed"
	}

	return TestResult{
		Domain: domain,
		Status: status,
		Rule:   rule,
	}
}
