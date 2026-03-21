package adblock

import (
	"os"
	"path/filepath"
	"testing"

	"smartdnssort/config"
)

// TestCountValidRules tests the CountValidRules function
func TestCountValidRules(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_rules.txt")

	// Write test content with various line types
	content := `! This is a comment
# Another comment

||example.com^
||ads.com^
! Yet another comment
||tracker.com^
||malware.com^
# Final comment
||spam.com^
`

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test counting valid rules
	count, err := CountValidRules(testFile)
	if err != nil {
		t.Fatalf("CountValidRules failed: %v", err)
	}

	// Expected: 5 valid rules (example.com, ads.com, tracker.com, malware.com, spam.com)
	expected := 5
	if count != expected {
		t.Errorf("Expected %d valid rules, got %d", expected, count)
	}
}

// TestCountValidRules_EmptyFile tests counting rules in an empty file
func TestCountValidRules_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "empty.txt")

	if err := os.WriteFile(testFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	count, err := CountValidRules(testFile)
	if err != nil {
		t.Fatalf("CountValidRules failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 valid rules in empty file, got %d", count)
	}
}

// TestCountValidRules_OnlyComments tests counting rules in a file with only comments
func TestCountValidRules_OnlyComments(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "comments.txt")

	content := `! Comment 1
# Comment 2
! Comment 3
`

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	count, err := CountValidRules(testFile)
	if err != nil {
		t.Fatalf("CountValidRules failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 valid rules in comment-only file, got %d", count)
	}
}

// TestCountValidRules_NonExistentFile tests error handling for non-existent file
func TestCountValidRules_NonExistentFile(t *testing.T) {
	_, err := CountValidRules("/non/existent/file.txt")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

// TestIsLocalFile tests the IsLocalFile function
func TestIsLocalFile(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{
			name:     "HTTP URL",
			url:      "http://example.com/rules.txt",
			expected: false,
		},
		{
			name:     "HTTPS URL",
			url:      "https://example.com/rules.txt",
			expected: false,
		},
		{
			name:     "File protocol URL",
			url:      "file:///path/to/rules.txt",
			expected: true,
		},
		{
			name:     "Relative path",
			url:      "./rules.txt",
			expected: true,
		},
		{
			name:     "Absolute path",
			url:      "/etc/adblock/rules.txt",
			expected: true,
		},
		{
			name:     "Windows path",
			url:      "C:\\rules.txt",
			expected: true,
		},
		{
			name:     "Empty string",
			url:      "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsLocalFile(tt.url)
			if result != tt.expected {
				t.Errorf("IsLocalFile(%q) = %v, expected %v", tt.url, result, tt.expected)
			}
		})
	}
}

// TestGetLocalFilePath tests the GetLocalFilePath function
func TestGetLocalFilePath(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "File protocol URL",
			url:      "file:///path/to/rules.txt",
			expected: "/path/to/rules.txt",
		},
		{
			name:     "File protocol with double slash",
			url:      "file://path/to/rules.txt",
			expected: "path/to/rules.txt",
		},
		{
			name:     "Relative path unchanged",
			url:      "./rules.txt",
			expected: "./rules.txt",
		},
		{
			name:     "Absolute path unchanged",
			url:      "/etc/adblock/rules.txt",
			expected: "/etc/adblock/rules.txt",
		},
		{
			name:     "Empty string",
			url:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetLocalFilePath(tt.url)
			if result != tt.expected {
				t.Errorf("GetLocalFilePath(%q) = %q, expected %q", tt.url, result, tt.expected)
			}
		})
	}
}

// TestCreateEngine tests the CreateEngine function
func TestCreateEngine(t *testing.T) {
	tests := []struct {
		name        string
		engineType  string
		expectError bool
	}{
		{
			name:        "Simple filter engine",
			engineType:  "simple",
			expectError: false,
		},
		{
			name:        "URL filter engine",
			engineType:  "urlfilter",
			expectError: false,
		},
		{
			name:        "Unknown engine type",
			engineType:  "unknown",
			expectError: true,
		},
		{
			name:        "Empty engine type",
			engineType:  "",
			expectError: true,
		},
		{
			name:        "Case insensitive - SIMPLE",
			engineType:  "SIMPLE",
			expectError: false,
		},
		{
			name:        "Case insensitive - URLFILTER",
			engineType:  "URLFILTER",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.AdBlockConfig{
				Engine: tt.engineType,
			}

			engine, err := CreateEngine(cfg)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for engine type %q, got nil", tt.engineType)
				}
				if engine != nil {
					t.Errorf("Expected nil engine for error case, got %v", engine)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for engine type %q: %v", tt.engineType, err)
				}
				if engine == nil {
					t.Errorf("Expected non-nil engine for type %q", tt.engineType)
				}
				// Test that the engine implements the interface
				if engine != nil {
					_, _ = engine.CheckHost("example.com")
					_ = engine.Count()
					_ = engine.Close()
				}
			}
		})
	}
}

// TestCreateEngine_NilConfig tests error handling for nil config
func TestCreateEngine_NilConfig(t *testing.T) {
	_, err := CreateEngine(nil)
	if err == nil {
		t.Error("Expected error for nil config, got nil")
	}
}
