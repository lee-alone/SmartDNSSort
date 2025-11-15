package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DNS      DNSConfig      `yaml:"dns"`
	Upstream UpstreamConfig `yaml:"upstream"`
	Ping     PingConfig     `yaml:"ping"`
	Cache    CacheConfig    `yaml:"cache"`
	WebUI    WebUIConfig    `yaml:"webui"`
	AdBlock  AdBlockConfig  `yaml:"adblock"`
}

type DNSConfig struct {
	ListenPort int  `yaml:"listen_port"`
	EnableTCP  bool `yaml:"enable_tcp"`
	EnableIPv6 bool `yaml:"enable_ipv6"`
}

type UpstreamConfig struct {
	Servers     []string `yaml:"servers"`
	Strategy    string   `yaml:"strategy"` // parallel, random
	TimeoutMs   int      `yaml:"timeout_ms"`
	Concurrency int      `yaml:"concurrency"`
}

type PingConfig struct {
	Count       int    `yaml:"count"`
	TimeoutMs   int    `yaml:"timeout_ms"`
	Concurrency int    `yaml:"concurrency"`
	Strategy    string `yaml:"strategy"` // min, avg
}

type CacheConfig struct {
	MinTTLSeconds int `yaml:"min_ttl_seconds"`
	MaxTTLSeconds int `yaml:"max_ttl_seconds"`
}

type WebUIConfig struct {
	Enabled    bool `yaml:"enabled"`
	ListenPort int  `yaml:"listen_port"`
}

type AdBlockConfig struct {
	Enabled  bool   `yaml:"enabled"`
	RuleFile string `yaml:"rule_file"`
}

// LoadConfig 从 YAML 文件加载配置
func LoadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// 设置默认值
	if cfg.DNS.ListenPort == 0 {
		cfg.DNS.ListenPort = 53
	}
	if cfg.Upstream.TimeoutMs == 0 {
		cfg.Upstream.TimeoutMs = 300
	}
	if cfg.Upstream.Concurrency == 0 {
		cfg.Upstream.Concurrency = 4
	}
	if cfg.Ping.Count == 0 {
		cfg.Ping.Count = 3
	}
	if cfg.Ping.TimeoutMs == 0 {
		cfg.Ping.TimeoutMs = 500
	}
	if cfg.Ping.Concurrency == 0 {
		cfg.Ping.Concurrency = 16
	}
	if cfg.Cache.MinTTLSeconds == 0 {
		cfg.Cache.MinTTLSeconds = 60
	}
	if cfg.Cache.MaxTTLSeconds == 0 {
		cfg.Cache.MaxTTLSeconds = 600
	}

	return &cfg, nil
}
