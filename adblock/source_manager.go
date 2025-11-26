package adblock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"smartdnssort/config"
	"sync"
	"time"
)

type SourceStatus struct {
	URL        string    `json:"url"`
	Status     string    `json:"status"` // "active", "failed", "bad"
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
		// Create custom rules file if it doesn't exist
		if err := sm.ensureCustomRulesFile(cfg.CustomRulesFile); err != nil {
			return nil, err
		}
		sm.AddSource(cfg.CustomRulesFile)
	}

	return sm, nil
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

	for _, s := range sources {
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

	if _, exists := sm.sources[url]; !exists {
		h := sha256.Sum256([]byte(url))
		cacheFile := "rules_" + hex.EncodeToString(h[:16]) + ".txt"

		sm.sources[url] = &SourceInfo{
			URL:       url,
			Status:    "active",
			CacheFile: cacheFile,
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
			source.Status = "failed"
			if source.FailCount >= 3 {
				source.Status = "bad"
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
			RuleCount:  s.RuleCount,
			LastUpdate: s.LastUpdate,
			LastError:  s.LastError,
		})
	}
	return statuses
}

// ensureCustomRulesFile creates the custom rules file if it doesn't exist
func (sm *SourceManager) ensureCustomRulesFile(filePath string) error {
	// Check if file already exists
	if _, err := os.Stat(filePath); err == nil {
		return nil // File exists, nothing to do
	} else if !os.IsNotExist(err) {
		return err // Some other error occurred
	}

	// Ensure parent directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Create file with helpful comments
	defaultContent := `# SmartDNSSort 自定义广告屏蔽规则文件
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
	return os.WriteFile(filePath, []byte(defaultContent), 0644)
}
