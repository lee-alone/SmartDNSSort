package config

import (
	"os"
	"testing"
)

// TestBootstrapDNSDefaultValues 测试 bootstrap_dns 的默认值设置
func TestBootstrapDNSDefaultValues(t *testing.T) {
	// 创建一个临时配置文件，不包含 bootstrap_dns 配置
	tempConfig := `
dns:
  listen_port: 53
  enable_tcp: true
  enable_ipv6: true

upstream:
  servers:
    - "https://dns.google/dns-query"
  strategy: "random"
  timeout_ms: 5000
  concurrency: 3
`
	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "test_config_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(tempConfig); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	// 加载配置
	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 验证 bootstrap_dns 是否有默认值
	if len(cfg.Upstream.BootstrapDNS) == 0 {
		t.Error("Expected bootstrap_dns to have default values, but got empty slice")
	}

	// 验证默认值是否正确
	expectedDNS := []string{"8.8.8.8", "1.1.1.1", "8.8.4.4", "1.0.0.1"}
	if len(cfg.Upstream.BootstrapDNS) != len(expectedDNS) {
		t.Errorf("Expected %d bootstrap DNS servers, got %d", len(expectedDNS), len(cfg.Upstream.BootstrapDNS))
	}

	for i, dns := range expectedDNS {
		if i >= len(cfg.Upstream.BootstrapDNS) {
			break
		}
		if cfg.Upstream.BootstrapDNS[i] != dns {
			t.Errorf("Expected bootstrap_dns[%d] = %s, got %s", i, dns, cfg.Upstream.BootstrapDNS[i])
		}
	}

	t.Logf("Bootstrap DNS default values: %v", cfg.Upstream.BootstrapDNS)
}

// TestBootstrapDNSCustomValues 测试自定义 bootstrap_dns 配置不会被覆盖
func TestBootstrapDNSCustomValues(t *testing.T) {
	// 创建一个临时配置文件，包含自定义 bootstrap_dns 配置
	tempConfig := `
dns:
  listen_port: 53

upstream:
  servers:
    - "https://dns.google/dns-query"
  bootstrap_dns:
    - "192.168.1.1"
    - "192.168.1.2"
  strategy: "random"
  timeout_ms: 5000
`
	// 创建临时文件
	tmpFile, err := os.CreateTemp("", "test_config_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(tempConfig); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	tmpFile.Close()

	// 加载配置
	cfg, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// 验证自定义值没有被覆盖
	expectedDNS := []string{"192.168.1.1", "192.168.1.2"}
	if len(cfg.Upstream.BootstrapDNS) != len(expectedDNS) {
		t.Errorf("Expected %d bootstrap DNS servers, got %d", len(expectedDNS), len(cfg.Upstream.BootstrapDNS))
	}

	for i, dns := range expectedDNS {
		if cfg.Upstream.BootstrapDNS[i] != dns {
			t.Errorf("Expected bootstrap_dns[%d] = %s, got %s", i, dns, cfg.Upstream.BootstrapDNS[i])
		}
	}

	t.Logf("Custom Bootstrap DNS values preserved: %v", cfg.Upstream.BootstrapDNS)
}

func TestPingEnabledDefaultAndCustomValues(t *testing.T) {
	tests := []struct {
		name           string
		configContent  string
		expectedStatus bool
	}{
		{
			name: "Default value when omitted within ping section",
			configContent: `
ping:
  count: 1
`,
			expectedStatus: true, // Should default to true if omitted
		},
		{
			name: "Explicitly set to true",
			configContent: `
ping:
  enabled: true
  count: 1
`,
			expectedStatus: true,
		},
		{
			name: "Explicitly set to false",
			configContent: `
ping:
  enabled: false
  count: 1
`,
			expectedStatus: false,
		},
		{
			name: "Ping section completely omitted", // This should also default to true
			configContent: `
dns:
  listen_port: 53
`,
			expectedStatus: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "test_config_ping_*.yaml")
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(tt.configContent); err != nil {
				t.Fatalf("Failed to write temp file: %v", err)
			}
			tmpFile.Close()

			cfg, err := LoadConfig(tmpFile.Name())
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			if cfg.Ping.Enabled != tt.expectedStatus {
				t.Errorf("Expected Ping.Enabled to be %v, got %v for config:\n%s",
					tt.expectedStatus, cfg.Ping.Enabled, tt.configContent)
			}
		})
	}
}

