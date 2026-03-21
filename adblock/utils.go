package adblock

import (
	"bufio"
	"fmt"
	"os"
	"smartdnssort/config"
	"strings"
)

// ReadValidRules reads valid rules from a file (excluding comments and empty lines)
func ReadValidRules(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments (# and !)
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "!") {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

// CountValidRules counts valid rules in a file (excluding comments and empty lines)
func CountValidRules(path string) (int, error) {
	lines, err := ReadValidRules(path)
	if err != nil {
		return 0, err
	}
	return len(lines), nil
}

// IsLocalFile checks if a URL represents a local file
func IsLocalFile(url string) bool {
	return strings.HasPrefix(url, "file://") || !strings.HasPrefix(url, "http")
}

// GetLocalFilePath returns the absolute path for a local file URL
func GetLocalFilePath(url string) string {
	return strings.TrimPrefix(url, "file://")
}

// CreateEngine creates a new filter engine based on the configuration
func CreateEngine(cfg *config.AdBlockConfig) (FilterEngine, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	switch strings.ToLower(cfg.Engine) {
	case "urlfilter":
		return NewURLFilterEngine()
	case "simple":
		return NewSimpleFilter(), nil
	default:
		return nil, fmt.Errorf("unknown adblock engine: %s", cfg.Engine)
	}
}
