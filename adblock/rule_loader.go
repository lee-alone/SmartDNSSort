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
	defaultDownloadTimeout        = 60 * time.Second // 增加到60秒，给大文件足够的时间
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
// It will retry up to 3 times on failure.
func (rl *RuleLoader) UpdateFromSource(ctx context.Context, source *SourceInfo) (string, int, error) {
	if strings.HasPrefix(source.URL, "file://") || !strings.HasPrefix(source.URL, "http") {
		// Handle local file
		filePath := strings.TrimPrefix(source.URL, "file://")
		return rl.loadLocalFile(filePath)
	}

	// Retry logic for remote files
	const maxRetries = 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		cachePath, ruleCount, err := rl.downloadRemoteFile(ctx, source)
		if err == nil {
			return cachePath, ruleCount, nil
		}

		lastErr = err

		// Don't retry on certain errors
		if strings.Contains(err.Error(), "bad status: 404") ||
			strings.Contains(err.Error(), "bad status: 403") ||
			strings.Contains(err.Error(), "exceeds 50MB limit") {
			return "", 0, err
		}

		// Wait before retrying (exponential backoff)
		if attempt < maxRetries {
			waitTime := time.Duration(attempt*attempt) * time.Second
			select {
			case <-time.After(waitTime):
				// Continue to next attempt
			case <-ctx.Done():
				return "", 0, ctx.Err()
			}
		}
	}

	return "", 0, fmt.Errorf("failed to download after %d attempts: %w", maxRetries, lastErr)
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

	// Get Content-Length for validation
	contentLength := resp.ContentLength
	if contentLength <= 0 {
		return "", 0, fmt.Errorf("invalid or missing Content-Length header")
	}

	// Check if file exceeds 50MB limit
	const maxFileSize = 50 * 1024 * 1024
	if contentLength > maxFileSize {
		return "", 0, fmt.Errorf("file exceeds 50MB limit (size: %d bytes)", contentLength)
	}

	cachePath := filepath.Join(rl.cacheDir, source.CacheFile)

	// Download to temporary file first
	tempPath := cachePath + ".tmp"
	file, err := os.Create(tempPath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	// Write response body to file and track bytes written
	bytesWritten, err := io.Copy(file, resp.Body)
	if err != nil {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("failed to write file: %w", err)
	}

	// Verify that we received the complete file
	if bytesWritten != contentLength {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("incomplete download: expected %d bytes, got %d bytes", contentLength, bytesWritten)
	}

	// Close file before counting lines
	file.Close()

	// Count lines in the downloaded file
	ruleCount, err := countLinesFromFile(tempPath)
	if err != nil {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("failed to count rules: %w", err)
	}

	// Validate that we have rules
	if ruleCount == 0 {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("downloaded file contains no valid rules")
	}

	// Move temp file to final location
	if err := os.Rename(tempPath, cachePath); err != nil {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("failed to finalize cache file: %w", err)
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

// countLinesFromFile counts non-empty lines in a file
// This is more accurate than just counting newlines
func countLinesFromFile(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line != "" && !strings.HasPrefix(line, "#") {
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return count, nil
}

// LoadAllRules reads all rules from a list of cache files.
func (rl *RuleLoader) LoadAllRules(sources []*SourceInfo) ([]string, error) {
	var allRules []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	var loadErrors []string
	var errMu sync.Mutex

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
					// Cache file doesn't exist and it's not a local file
					// This is a critical error - the source should have been downloaded first
					errMu.Lock()
					loadErrors = append(loadErrors, fmt.Sprintf("cache file missing for source %s: %s", s.URL, cachePath))
					errMu.Unlock()
					return
				}
			}

			rules, err := readLines(cachePath)
			if err != nil {
				// Log error but continue with other sources
				errMu.Lock()
				loadErrors = append(loadErrors, fmt.Sprintf("failed to read rules from %s: %v", s.URL, err))
				errMu.Unlock()
				return
			}
			mu.Lock()
			allRules = append(allRules, rules...)
			mu.Unlock()
		}(source)
	}

	wg.Wait()

	// Log any errors that occurred during loading
	if len(loadErrors) > 0 {
		// In production, these should be logged properly
		// For now, we'll just continue with the rules we managed to load
		_ = loadErrors
	}

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
