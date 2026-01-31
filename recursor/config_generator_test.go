package recursor

import (
	"strings"
	"testing"
)

// TestGetVersionFeatures 测试获取版本特性
func TestGetVersionFeatures(t *testing.T) {
	tests := []struct {
		version  string
		features VersionFeatures
	}{
		{
			version: "1.6.0",
			features: VersionFeatures{
				ServeExpired:      false,
				QnameMinimisation: true,
				UseCapsForID:      true,
			},
		},
		{
			version: "1.9.0",
			features: VersionFeatures{
				ServeExpired:      true,
				ServeExpiredTTL:   true,
				PrefetchKey:       true,
				QnameMinimisation: true,
			},
		},
		{
			version: "1.19.0",
			features: VersionFeatures{
				ServeExpired:         true,
				ServeExpiredTTL:      true,
				ServeExpiredReplyTTL: true,
				PrefetchKey:          true,
				QnameMinimisation:    true,
				MinimalResponses:     true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			cg := NewConfigGenerator(tt.version, SystemInfo{}, 5353)
			features := cg.GetVersionFeatures()

			if features.ServeExpired != tt.features.ServeExpired {
				t.Errorf("ServeExpired mismatch: expected %v, got %v", tt.features.ServeExpired, features.ServeExpired)
			}
			if features.QnameMinimisation != tt.features.QnameMinimisation {
				t.Errorf("QnameMinimisation mismatch: expected %v, got %v", tt.features.QnameMinimisation, features.QnameMinimisation)
			}
		})
	}
}

// TestCalculateParams 测试计算配置参数
func TestCalculateParams(t *testing.T) {
	tests := []struct {
		name     string
		sysInfo  SystemInfo
		expected ConfigParams
	}{
		{
			name: "4 cores, 8GB",
			sysInfo: SystemInfo{
				CPUCores: 4,
				MemoryGB: 8.0,
			},
			expected: ConfigParams{
				NumThreads:     4,
				MsgCacheSize:   409, // 8192 * 5 / 100 = 409.6
				RRsetCacheSize: 819, // 8192 * 10 / 100 = 819.2
			},
		},
		{
			name: "16 cores, 32GB",
			sysInfo: SystemInfo{
				CPUCores: 16,
				MemoryGB: 32.0,
			},
			expected: ConfigParams{
				NumThreads:     8,    // 最大 8
				MsgCacheSize:   500,  // 最大 500
				RRsetCacheSize: 1000, // 最大 1000
			},
		},
		{
			name: "1 core, 2GB",
			sysInfo: SystemInfo{
				CPUCores: 1,
				MemoryGB: 2.0,
			},
			expected: ConfigParams{
				NumThreads:     1,
				MsgCacheSize:   102, // 2048 * 5 / 100 = 102.4
				RRsetCacheSize: 204, // 2048 * 10 / 100 = 204.8
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cg := NewConfigGenerator("1.19.0", tt.sysInfo, 5353)
			params := cg.CalculateParams()

			if params.NumThreads != tt.expected.NumThreads {
				t.Errorf("NumThreads: expected %d, got %d", tt.expected.NumThreads, params.NumThreads)
			}
			if params.MsgCacheSize != tt.expected.MsgCacheSize {
				t.Errorf("MsgCacheSize: expected %d, got %d", tt.expected.MsgCacheSize, params.MsgCacheSize)
			}
			if params.RRsetCacheSize != tt.expected.RRsetCacheSize {
				t.Errorf("RRsetCacheSize: expected %d, got %d", tt.expected.RRsetCacheSize, params.RRsetCacheSize)
			}
		})
	}
}

// TestGenerateConfig 测试生成配置
func TestGenerateConfig(t *testing.T) {
	sysInfo := SystemInfo{
		CPUCores: 4,
		MemoryGB: 8.0,
	}

	cg := NewConfigGenerator("1.19.0", sysInfo, 5353)
	config, err := cg.GenerateConfig()

	if err != nil {
		t.Fatalf("Failed to generate config: %v", err)
	}

	if config == "" {
		t.Error("Config should not be empty")
	}

	// 验证关键配置项
	checks := []string{
		"interface: 127.0.0.1@5353",
		"num-threads: 4",
		"msg-cache-size: 409m",
		"rrset-cache-size: 819m",
		"serve-expired: yes",
		"prefetch: yes",
	}

	for _, check := range checks {
		if !strings.Contains(config, check) {
			t.Errorf("Config missing: %s", check)
		}
	}
}

// TestValidateConfig 测试验证配置
func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cg      *ConfigGenerator
		wantErr bool
	}{
		{
			name: "Valid config",
			cg: NewConfigGenerator("1.19.0", SystemInfo{
				CPUCores: 4,
				MemoryGB: 8.0,
			}, 5353),
			wantErr: false,
		},
		{
			name: "Invalid port (too low)",
			cg: NewConfigGenerator("1.19.0", SystemInfo{
				CPUCores: 4,
			}, 100),
			wantErr: true,
		},
		{
			name: "Invalid port (too high)",
			cg: NewConfigGenerator("1.19.0", SystemInfo{
				CPUCores: 4,
			}, 70000),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cg.ValidateConfig()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestParseVersion 测试解析版本号
func TestParseVersion(t *testing.T) {
	tests := []struct {
		version string
		major   int
		minor   int
		patch   int
	}{
		{"1.6.0", 1, 6, 0},
		{"1.9.0", 1, 9, 0},
		{"1.19.0", 1, 19, 0},
		{"2.0.0", 2, 0, 0},
		{"1.9", 1, 9, 0},
		{"1", 1, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			cg := NewConfigGenerator(tt.version, SystemInfo{}, 5353)
			ver := cg.parseVersion(tt.version)

			if ver.Major != tt.major {
				t.Errorf("Major: expected %d, got %d", tt.major, ver.Major)
			}
			if ver.Minor != tt.minor {
				t.Errorf("Minor: expected %d, got %d", tt.minor, ver.Minor)
			}
			if ver.Patch != tt.patch {
				t.Errorf("Patch: expected %d, got %d", tt.patch, ver.Patch)
			}
		})
	}
}

// TestConfigGeneratorCreation 测试创建 ConfigGenerator
func TestConfigGeneratorCreation(t *testing.T) {
	sysInfo := SystemInfo{
		CPUCores: 4,
		MemoryGB: 8.0,
	}

	cg := NewConfigGenerator("1.19.0", sysInfo, 5353)

	if cg == nil {
		t.Fatal("NewConfigGenerator returned nil")
	}

	if cg.version != "1.19.0" {
		t.Errorf("Expected version 1.19.0, got %s", cg.version)
	}

	if cg.port != 5353 {
		t.Errorf("Expected port 5353, got %d", cg.port)
	}
}

// BenchmarkGenerateConfig 基准测试：生成配置
func BenchmarkGenerateConfig(b *testing.B) {
	sysInfo := SystemInfo{
		CPUCores: 4,
		MemoryGB: 8.0,
	}
	cg := NewConfigGenerator("1.19.0", sysInfo, 5353)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cg.GenerateConfig()
	}
}

// BenchmarkCalculateParams 基准测试：计算参数
func BenchmarkCalculateParams(b *testing.B) {
	sysInfo := SystemInfo{
		CPUCores: 4,
		MemoryGB: 8.0,
	}
	cg := NewConfigGenerator("1.19.0", sysInfo, 5353)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cg.CalculateParams()
	}
}

// TestConfigContent 测试配置文件内容的完整性
func TestConfigContent(t *testing.T) {
	sysInfo := SystemInfo{
		CPUCores: 4,
		MemoryGB: 8.0,
	}

	cg := NewConfigGenerator("1.19.0", sysInfo, 5353)
	config, err := cg.GenerateConfig()

	if err != nil {
		t.Fatalf("Failed to generate config: %v", err)
	}

	// 验证所有必要的配置项
	requiredConfigs := map[string]string{
		"do-ip4":              "do-ip4: yes",
		"do-ip6":              "do-ip6: no",
		"do-udp":              "do-udp: yes",
		"do-tcp":              "do-tcp: yes",
		"interface":           "interface: 127.0.0.1",
		"hide-identity":       "hide-identity: yes",
		"hide-version":        "hide-version: yes",
		"access-control-127":  "access-control: 127.0.0.1 allow",
		"access-control-deny": "access-control: 0.0.0.0/0 deny",
	}

	for name, configItem := range requiredConfigs {
		if !strings.Contains(config, configItem) {
			t.Errorf("Config missing %s: %s", name, configItem)
		}
	}
}
