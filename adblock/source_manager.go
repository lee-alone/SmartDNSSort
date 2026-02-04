package adblock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"smartdnssort/config"
	"strings"
	"sync"
	"time"
)

type SourceStatus struct {
	URL        string    `json:"url"`
	Status     string    `json:"status"` // "active", "failed", "bad"
	Enabled    bool      `json:"enabled"`
	RuleCount  int       `json:"rule_count"`
	LastUpdate time.Time `json:"last_update"`
	LastError  string    `json:"last_error"`
}

type SourceInfo struct {
	URL          string    `json:"url"`
	ETag         string    `json:"etag"`
	LastModified string    `json:"last_modified"`
	CacheFile    string    `json:"cache_file"`
	RuleCount    int       `json:"rule_count"`
	LastUpdate   time.Time `json:"last_update"`
	LastError    string    `json:"last_error"`
	FailCount    int       `json:"fail_count"`
	Status       string    `json:"status"` // active | failed | bad
	Enabled      bool      `json:"enabled"`
}

type SourceManager struct {
	sources  map[string]*SourceInfo
	metaFile string
	mu       sync.RWMutex
}

func NewSourceManager(cfg *config.AdBlockConfig) (*SourceManager, error) {
	if err := os.MkdirAll(cfg.CacheDir, 0755); err != nil {
		return nil, err
	}

	metaFile := filepath.Join(cfg.CacheDir, "rules_meta.json")
	sm := &SourceManager{
		sources:  make(map[string]*SourceInfo),
		metaFile: metaFile,
	}

	if err := sm.loadMeta(); err != nil {
		// If meta file doesn't exist or is corrupted, just start with a clean slate
		// But log the error for debugging
	}

	// Add sources from config, overriding any loaded from meta
	for _, url := range cfg.RuleURLs {
		sm.AddSource(url)
	}
	if cfg.CustomRulesFile != "" {
		// Ensure custom rules file exists and is properly initialized
		if err := sm.ensureCustomRulesFile(cfg.CustomRulesFile); err != nil {
			// Log error but don't fail - we can still use other sources
			// The file will be created on first write
		}
		sm.AddSource(cfg.CustomRulesFile)
	}

	return sm, nil
}

// ensureCustomRulesFile creates the custom rules file if it doesn't exist
func (sm *SourceManager) ensureCustomRulesFile(path string) error {
	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		// File exists, nothing to do
		return nil
	} else if !os.IsNotExist(err) {
		// Some other error occurred
		return err
	}

	// File doesn't exist, create it with a helpful template
	template := `# SmartDNSSort 自定义广告屏蔽规则文件
# 
# 在此文件中添加您自己的广告屏蔽规则
# 每行一条规则，支持以下格式：
#
# 1. 域名匹配（推荐）：
#    ||example.com^         - 屏蔽 example.com 及其所有子域名
#    ||ads.example.com^     - 仅屏蔽 ads.example.com
#
# 2. 通配符匹配：
#    *ads.*                 - 屏蔽包含 'ads.' 的所有域名
#
# 3. 正则表达式（高级）：
#    /^ad[s]?\./            - 使用正则表达式匹配
#
# 以 # 开头的行为注释，将被忽略
# 空行也会被忽略
#
# 示例规则（取消注释以启用）：
# ||doubleclick.net^
# ||googleadservices.com^
# ||googlesyndication.com^
# ||advertising.com^

`
	return os.WriteFile(path, []byte(template), 0644)
}

func (sm *SourceManager) loadMeta() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	data, err := os.ReadFile(sm.metaFile)
	if err != nil {
		return err
	}

	var sources []*SourceInfo
	if err := json.Unmarshal(data, &sources); err != nil {
		return err
	}

	hasEnabled := strings.Contains(string(data), "\"enabled\"")

	for _, s := range sources {
		if !hasEnabled {
			s.Enabled = true
		}
		sm.sources[s.URL] = s
	}
	return nil
}

func (sm *SourceManager) saveMeta() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var sources []*SourceInfo
	for _, s := range sm.sources {
		sources = append(sources, s)
	}

	data, err := json.MarshalIndent(sources, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(sm.metaFile, data, 0644)
}

func (sm *SourceManager) AddSource(url string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if source, exists := sm.sources[url]; exists {
		// Source already exists (loaded from meta), ensure it's enabled
		// since it's specified in the config
		source.Enabled = true
	} else {
		// Create new source
		h := sha256.Sum256([]byte(url))
		cacheFile := "rules_" + hex.EncodeToString(h[:16]) + ".txt"

		sm.sources[url] = &SourceInfo{
			URL:       url,
			Status:    "active",
			CacheFile: cacheFile,
			Enabled:   true,
		}
	}
}

func (sm *SourceManager) RemoveSource(url string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	// also remove the cache file
	if source, exists := sm.sources[url]; exists {
		os.Remove(filepath.Join(filepath.Dir(sm.metaFile), source.CacheFile))
		delete(sm.sources, url)
	}
}

func (sm *SourceManager) GetSource(url string) *SourceInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sources[url]
}

func (sm *SourceManager) GetAllSources() []*SourceInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var sources []*SourceInfo
	for _, s := range sm.sources {
		sources = append(sources, s)
	}
	return sources
}

func (sm *SourceManager) UpdateSourceStatus(url string, ruleCount int, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if source, exists := sm.sources[url]; exists {
		source.LastUpdate = time.Now()
		source.RuleCount = ruleCount
		if err != nil {
			source.LastError = err.Error()
			source.FailCount++
			// Determine if this is an initial load failure or an update failure
			// Initial load: FailCount == 1 (first attempt) AND RuleCount == 0 (no previous success)
			isInitialLoadFailure := source.FailCount == 1 && ruleCount == 0

			if isInitialLoadFailure {
				// First attempt to load rules failed, mark as initializing
				source.Status = "initializing"
			} else {
				// Either: previous attempts failed, or we had rules before
				// This is an update failure, mark as failed
				source.Status = "failed"
				if source.FailCount >= 3 {
					source.Status = "bad"
				}
			}
		} else {
			source.LastError = ""
			source.FailCount = 0
			source.Status = "active"
		}
	}
}

func (sm *SourceManager) GetStatuses() []SourceStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var statuses []SourceStatus
	for _, s := range sm.sources {
		statuses = append(statuses, SourceStatus{
			URL:        s.URL,
			Status:     s.Status,
			Enabled:    s.Enabled,
			RuleCount:  s.RuleCount,
			LastUpdate: s.LastUpdate,
			LastError:  s.LastError,
		})
	}
	return statuses
}

func (sm *SourceManager) SetEnabled(url string, enabled bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sources[url]; ok {
		s.Enabled = enabled
	}
}
