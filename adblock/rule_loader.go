package adblock

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"smartdnssort/config"
	"smartdnssort/logger"
	"strings"
	"time"
)

const (
	defaultMaxConcurrentDownloads = 5
	defaultDownloadTimeout        = 60 * time.Second
	maxFileSizeLimit              = 50 * 1024 * 1024 // 50MB
)

type RuleLoader struct {
	client        *http.Client
	maxConcurrent int
	sem           chan struct{}
	cacheDir      string
}

func NewRuleLoader(cfg *config.AdBlockConfig) *RuleLoader {
	maxConcurrent := cfg.MaxConcurrentDownloads
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrentDownloads
	}

	timeoutSeconds := cfg.DownloadTimeoutSeconds
	timeout := defaultDownloadTimeout
	if timeoutSeconds > 0 {
		timeout = time.Duration(timeoutSeconds) * time.Second
	}

	return &RuleLoader{
		client: &http.Client{
			Timeout: timeout,
		},
		maxConcurrent: maxConcurrent,
		sem:           make(chan struct{}, maxConcurrent),
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
	if IsLocalFile(source.URL) {
		// Handle local file
		filePath := GetLocalFilePath(source.URL)
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

	cachePath := filepath.Join(rl.cacheDir, source.CacheFile)

	// Download to temporary file first
	tempPath := cachePath + ".tmp"
	file, err := os.Create(tempPath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	// Write response body to file and track bytes written
	// Note: We don't require Content-Length header as many servers use chunked encoding
	bytesReceived, err := io.Copy(file, resp.Body)
	if err != nil {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("failed to write file: %w", err)
	}

	// Close file before counting lines to flush and release on some OS
	if err := file.Close(); err != nil {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("failed to close temp file: %w", err)
	}

	// Check if received zero bytes (this is still an error)
	if bytesReceived == 0 {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("downloaded file is empty (0 bytes)")
	}

	// Check if file exceeds limit
	if bytesReceived > maxFileSizeLimit {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("file exceeds 50MB limit (size: %d bytes)", bytesReceived)
	}

	// Count lines in the downloaded file
	ruleCount, err := countLinesFromFile(tempPath)
	if err != nil {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("failed to count rules: %w", err)
	}

	// Validate that we have rules
	if ruleCount == 0 {
		os.Remove(tempPath)
		return "", 0, fmt.Errorf("downloaded file contains no valid rules (received %d bytes)", bytesReceived)
	}

	// Move temp file to final location
	if err := os.Rename(tempPath, cachePath); err != nil {
		if rmErr := os.Remove(tempPath); rmErr != nil {
			logger.Warnf("[AdBlock] Failed to remove temp file %s: %v", tempPath, rmErr)
		}
		return "", 0, fmt.Errorf("failed to finalize cache file: %w", err)
	}

	// Update source info with new cache headers
	source.ETag = resp.Header.Get("ETag")
	source.LastModified = resp.Header.Get("Last-Modified")

	// Log success info
	logger.Debugf("[AdBlock] Successfully downloaded %d bytes and %d rules from %s", bytesReceived, ruleCount, source.URL)

	return cachePath, ruleCount, nil
}

func (rl *RuleLoader) loadLocalFile(path string) (string, int, error) {
	ruleCount, err := countLinesFromFile(path)
	return path, ruleCount, err
}

// countLinesFromFile is deprecated. Use CountValidRules in utils.go instead.
func countLinesFromFile(path string) (int, error) {
	return CountValidRules(path)
}

// LoadAllRules reads all rules from a list of cache files.
// Custom rules (local files) are loaded first to ensure higher priority.
func (rl *RuleLoader) LoadAllRules(sources []*SourceInfo) ([]string, error) {
	var allRules []string
	var loadErrors []string

	// Load rules sequentially to maintain priority order
	// Custom rules (local files) are already sorted first by GetAllSources()
	for _, source := range sources {
		if !source.Enabled {
			continue
		}

		// Check if cache file exists
		cachePath := filepath.Join(rl.cacheDir, source.CacheFile)
		if _, err := os.Stat(cachePath); os.IsNotExist(err) {
			// if a local file, the path is the URL
			if IsLocalFile(source.URL) {
				cachePath = GetLocalFilePath(source.URL)
			} else {
				// Cache file doesn't exist and it's not a local file
				// This is a critical error - the source should have been downloaded first
				loadErrors = append(loadErrors, fmt.Sprintf("cache file missing for source %s: %s", source.URL, cachePath))
				continue
			}
		}

		rules, err := ReadValidRules(cachePath)
		if err != nil {
			// Log error but continue with other sources
			loadErrors = append(loadErrors, fmt.Sprintf("failed to read rules from %s: %v", source.URL, err))
			continue
		}
		allRules = append(allRules, rules...)
	}

	// Log any errors that occurred during loading
	if len(loadErrors) > 0 {
		for _, errStr := range loadErrors {
			logger.Warnf("[AdBlock] %s", errStr)
		}
	}

	return allRules, nil
}
