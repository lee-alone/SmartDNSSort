package adblock

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"smartdnssort/config"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxConcurrentDownloads = 5
	defaultDownloadTimeout        = 15 * time.Second
)

type RuleLoader struct {
	client        *http.Client
	maxConcurrent int
	sem           chan struct{}
	cacheDir      string
}

func NewRuleLoader(cfg *config.AdBlockConfig) *RuleLoader {
	return &RuleLoader{
		client: &http.Client{
			Timeout: defaultDownloadTimeout,
		},
		maxConcurrent: defaultMaxConcurrentDownloads,
		sem:           make(chan struct{}, defaultMaxConcurrentDownloads),
		cacheDir:      cfg.CacheDir,
	}
}

type UpdateResult struct {
	TotalRules      int
	NewRules        int
	RemovedRules    int
	Sources         int
	FailedSources   []string
	DurationSeconds float64
}

// UpdateFromSource downloads rules from a single source URL.
// It handles caching with ETag and Last-Modified headers.
// It returns the path to the cached file, the number of rules, and any error.
func (rl *RuleLoader) UpdateFromSource(ctx context.Context, source *SourceInfo) (string, int, error) {
	if strings.HasPrefix(source.URL, "file://") || !strings.HasPrefix(source.URL, "http") {
		// Handle local file
		filePath := strings.TrimPrefix(source.URL, "file://")
		return rl.loadLocalFile(filePath)
	}

	return rl.downloadRemoteFile(ctx, source)
}

func (rl *RuleLoader) downloadRemoteFile(ctx context.Context, source *SourceInfo) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", source.URL, nil)
	if err != nil {
		return "", 0, err
	}

	// Add cache headers to the request
	if source.ETag != "" {
		req.Header.Set("If-None-Match", source.ETag)
	}
	if source.LastModified != "" {
		req.Header.Set("If-Modified-Since", source.LastModified)
	}

	resp, err := rl.client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		// Rules have not changed, use existing cache
		cachePath := filepath.Join(rl.cacheDir, source.CacheFile)
		return cachePath, source.RuleCount, nil // No change
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("bad status: %s", resp.Status)
	}

	cachePath := filepath.Join(rl.cacheDir, source.CacheFile)
	file, err := os.Create(cachePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	// Use a limited reader to prevent downloading huge files
	// 50MB limit from plan
	limitedReader := &io.LimitedReader{R: resp.Body, N: 50 * 1024 * 1024}
	ruleCount, err := countLines(io.TeeReader(limitedReader, file))
	if err != nil {
		return "", 0, err
	}
	if limitedReader.N == 0 {
		return "", 0, fmt.Errorf("file exceeds 50MB limit")
	}

	// Update source info with new cache headers
	source.ETag = resp.Header.Get("ETag")
	source.LastModified = resp.Header.Get("Last-Modified")

	return cachePath, ruleCount, nil
}

func (rl *RuleLoader) loadLocalFile(path string) (string, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	ruleCount, err := countLines(file)
	return path, ruleCount, err
}

func countLines(r io.Reader) (int, error) {
	buf := make([]byte, 32*1024)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := r.Read(buf)
		count += strings.Count(string(buf[:c]), string(lineSep))

		switch {
		case err == io.EOF:
			return count, nil
		case err != nil:
			return count, err
		}
	}
}

// LoadAllRules reads all rules from a list of cache files.
func (rl *RuleLoader) LoadAllRules(sources []*SourceInfo) ([]string, error) {
	var allRules []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, source := range sources {
		wg.Add(1)
		go func(s *SourceInfo) {
			defer wg.Done()
			if !s.Enabled {
				return
			}
			// Check if cache file exists
			cachePath := filepath.Join(rl.cacheDir, s.CacheFile)
			if _, err := os.Stat(cachePath); os.IsNotExist(err) {
				// if a local file, the path is the URL
				if strings.HasPrefix(s.URL, "file://") || !strings.HasPrefix(s.URL, "http") {
					cachePath = strings.TrimPrefix(s.URL, "file://")
				} else {
					return // Or log error
				}
			}

			rules, err := readLines(cachePath)
			if err != nil {
				// log error
				return
			}
			mu.Lock()
			allRules = append(allRules, rules...)
			mu.Unlock()
		}(source)
	}

	wg.Wait()
	return allRules, nil
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
