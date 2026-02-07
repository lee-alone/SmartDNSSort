package adblock

import (
	"context"
	"fmt"
	"smartdnssort/config"
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
		if err := m.LoadRulesFromCache(); err != nil {
			// log error
		}
		if _, err := m.UpdateRules(false); err != nil {
			// log error
		}
	}()

	// Ticker for periodic updates
	if m.cfg.UpdateIntervalHours > 0 {
		ticker := time.NewTicker(time.Duration(m.cfg.UpdateIntervalHours) * time.Hour)
		go func() {
			for {
				select {
				case <-ticker.C:
					if _, err := m.UpdateRules(false); err != nil {
						// log error
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

			// In force mode, we don't care about the age of the cache.
			if !force {
				// if cache is not too old, skip
				if time.Since(s.LastUpdate) < time.Duration(m.cfg.UpdateIntervalHours)*time.Hour {
					return
				}
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
	allRules, err := m.loader.LoadAllRules(sources)
	if err != nil {
		return err
	}

	// Only load rules if we have any
	if len(allRules) > 0 {
		return m.engine.LoadRules(allRules)
	}

	return nil
}

func (m *AdBlockManager) CheckHost(domain string) (bool, string) {
	if !m.cfg.Enable {
		return false, ""
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
	Domain  string `json:"domain"`
	Blocked bool   `json:"blocked"`
	Rule    string `json:"rule"`
	Source  string `json:"source"` // This would be hard to implement without more info
}

func (m *AdBlockManager) TestDomain(domain string) TestResult {
	blocked, rule := m.CheckHost(domain)
	return TestResult{
		Domain:  domain,
		Blocked: blocked,
		Rule:    rule,
	}
}
