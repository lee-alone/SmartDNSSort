package adblock

import (
	"os"
	"path/filepath"
	"smartdnssort/config"
	"testing"
)

func TestEnsureCustomRulesFile(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()
	customRulesPath := filepath.Join(tempDir, "custom_rules.txt")

	// Create test config
	cfg := &config.AdBlockConfig{
		Enable:          true,
		CustomRulesFile: customRulesPath,
		CacheDir:        tempDir,
	}

	// Create SourceManager which should create the custom rules file
	sm, err := NewSourceManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create SourceManager: %v", err)
	}

	// Check if custom rules file was created
	if _, statErr := os.Stat(customRulesPath); os.IsNotExist(statErr) {
		t.Errorf("Custom rules file was not created at %s", customRulesPath)
	}

	// Check if file has content
	content, readErr := os.ReadFile(customRulesPath)
	if readErr != nil {
		t.Fatalf("Failed to read custom rules file: %v", readErr)
	}

	if len(content) == 0 {
		t.Error("Custom rules file is empty")
	}

	// Check if file contains expected content
	contentStr := string(content)
	if !contains(contentStr, "SmartDNSSort") {
		t.Error("Custom rules file does not contain expected header")
	}

	// Verify that the file is added as a source
	source := sm.GetSource(customRulesPath)
	if source == nil {
		t.Error("Custom rules file was not added as a source")
	}
}

func TestEnsureCustomRulesFileAlreadyExists(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()
	customRulesPath := filepath.Join(tempDir, "custom_rules.txt")

	// Pre-create the file with custom content
	existingContent := "# My custom rules\n||test.com^\n"
	if err := os.WriteFile(customRulesPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create test config
	cfg := &config.AdBlockConfig{
		Enable:          true,
		CustomRulesFile: customRulesPath,
		CacheDir:        tempDir,
	}

	// Create SourceManager
	_, err := NewSourceManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create SourceManager: %v", err)
	}

	// Check that existing content was preserved
	content, readErr := os.ReadFile(customRulesPath)
	if readErr != nil {
		t.Fatalf("Failed to read custom rules file: %v", readErr)
	}

	if string(content) != existingContent {
		t.Error("Existing custom rules file was overwritten")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
