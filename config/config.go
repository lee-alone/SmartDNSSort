package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// 默认配置文件内容（包含详细说明）
const DefaultConfigContent = `# SmartDNSSort 配置文件

# DNS 服务器配置
dns:
  # DNS 监听端口（默认 53）
  listen_port: 53
  # 是否启用 TCP 协议（用于大型 DNS 查询，默认 true）
  enable_tcp: true
  # 是否启用 IPv6 支持（默认 true）
  enable_ipv6: true

# 上游 DNS 服务器配置
upstream:
  # 上游 DNS 服务器地址列表
  servers:
    - "192.168.1.10"
    - "192.168.1.11"
    - "192.168.1.25"
  # 查询策略：parallel（并行查询所有服务器）或 random（随机选择一个服务器）
  strategy: "random"
  # 上游服务器响应超时时间（毫秒）
  timeout_ms: 3000
  # 并发查询数量
  concurrency: 4

# Ping 检测配置（用于选择最优的 DNS 服务器）
ping:
  # 每次 Ping 的数据包个数
  count: 3
  # Ping 响应超时时间（毫秒）
  timeout_ms: 500
  # 并发 Ping 的数量
  concurrency: 16
  # 选择策略：min（选择最低延迟）或 avg（选择平均延迟最低）
  strategy: "min"

# DNS 缓存配置
cache:
  # 首次查询或过期缓存返回时使用的 TTL（快速响应）。默认值：60秒
  fast_response_ttl: 60
  # 缓存最小 TTL（生存时间，秒）
  min_ttl_seconds: 3600
  # 缓存最大 TTL（生存时间，秒）
  max_ttl_seconds: 84600

# Web UI 管理界面配置
webui:
  # 是否启用 Web 管理界面（默认 true）
  enabled: true
  # Web 界面监听端口（默认 8080）
  listen_port: 8080

# 广告拦截配置
adblock:
  # 是否启用广告拦截功能（默认 false）
  enabled: false
  # 广告拦截规则文件路径
  rule_file: "rules.txt"
`

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
	FastResponseTTL int `yaml:"fast_response_ttl"`
	MinTTLSeconds   int `yaml:"min_ttl_seconds"`
	MaxTTLSeconds   int `yaml:"max_ttl_seconds"`
}

type WebUIConfig struct {
	Enabled    bool `yaml:"enabled"`
	ListenPort int  `yaml:"listen_port"`
}

type AdBlockConfig struct {
	Enabled  bool   `yaml:"enabled"`
	RuleFile string `yaml:"rule_file"`
}

// CreateDefaultConfig 创建默认配置文件
func CreateDefaultConfig(filePath string) error {
	return os.WriteFile(filePath, []byte(DefaultConfigContent), 0644)
}

// LoadConfig 从 YAML 文件加载配置
func LoadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		// 如果文件不存在，自动创建默认配置文件
		if os.IsNotExist(err) {
			if err := CreateDefaultConfig(filePath); err != nil {
				return nil, err
			}
			// 读取刚创建的文件
			data, err = os.ReadFile(filePath)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
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
	if cfg.Cache.FastResponseTTL == 0 {
		cfg.Cache.FastResponseTTL = 60
	}
	if cfg.Cache.MinTTLSeconds == 0 {
		cfg.Cache.MinTTLSeconds = 60
	}
	if cfg.Cache.MaxTTLSeconds == 0 {
		cfg.Cache.MaxTTLSeconds = 600
	}

	return &cfg, nil
}
