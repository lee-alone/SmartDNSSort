package config

import (
	"os"
	"runtime"

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
    - "192.168.1.25"

  # 查询策略：parallel（并行查询所有服务器）或 random（随机选择一个服务器）
  strategy: "random"
  # 上游服务器响应超时时间（毫秒）
  timeout_ms: 3000
  # 并发查询数量
  concurrency: 100
  # 是否将上游错误（如SERVFAIL, timeout）转换为 NXDOMAIN 响应给客户端（默认 true）
  # 这可以加快客户端的失败重试行为，但可能会隐藏上游服务器的真实问题。
  nxdomain_for_errors: true

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
  # 每次排序最多测试的 IP 数量（0 表示不限制）
  max_test_ips: 0
  # 单个 IP 的 RTT (延迟) 结果缓存时间（秒，0 表示禁用）
  rtt_cache_ttl_seconds: 900

# DNS 缓存配置
cache:
  # 首次查询或过期缓存返回时使用的 TTL（快速响应）。默认值：60秒
  fast_response_ttl: 15
  # 缓存命中时返回给客户端的 TTL。默认值：500秒
  user_return_ttl: 500
  # 缓存最小 TTL（生存时间，秒）
  # 设置为 0 有特殊含义：如果 min 和 max 都为 0，不修改上游TTL；仅 min 为 0 时只限制最大值
  min_ttl_seconds: 3600
  # 缓存最大 TTL（生存时间，秒）
  # 设置为 0 有特殊含义：如果 min 和 max 都为 0，不修改上游TTL；仅 max 为 0 时只限制最小值
  max_ttl_seconds: 84600
  # 负向缓存（域名不存在或无记录）的 TTL（秒）。默认值：300秒
  negative_ttl_seconds: 300
  # 错误响应缓存（SERVFAIL/REFUSED等）的 TTL（秒）。默认值：30秒
  error_cache_ttl_seconds: 30

# 热点域名提前刷新机制
prefetch:
  # 是否启用预取功能
  enabled: true
  # 记录最近访问频率最高的前 N 个域名
  top_domains_limit: 1000
  # 在缓存即将过期前指定秒数触发后台异步更新
  refresh_before_expire_seconds: 10

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

# 系统性能配置
system:
  # 最大 CPU 核心数（0 表示不限制，使用全部可用核心）
  max_cpu_cores: 0
  # IP 排序队列的工作线程数（0 表示根据 CPU 核心数自动调整）
  sort_queue_workers: 0
  # 异步缓存刷新工作线程数（0 表示根据 CPU 核心数自动调整）
  refresh_workers: 0
`

type Config struct {
	DNS      DNSConfig      `yaml:"dns" json:"dns"`
	Upstream UpstreamConfig `yaml:"upstream" json:"upstream"`
	Ping     PingConfig     `yaml:"ping" json:"ping"`
	Cache    CacheConfig    `yaml:"cache" json:"cache"`
	Prefetch PrefetchConfig `yaml:"prefetch" json:"prefetch"`
	WebUI    WebUIConfig    `yaml:"webui" json:"webui"`
	AdBlock  AdBlockConfig  `yaml:"adblock" json:"adblock"`
	System   SystemConfig   `yaml:"system" json:"system"`
}

type DNSConfig struct {
	ListenPort int  `yaml:"listen_port" json:"listen_port"`
	EnableTCP  bool `yaml:"enable_tcp" json:"enable_tcp"`
	EnableIPv6 bool `yaml:"enable_ipv6" json:"enable_ipv6"`
}

type UpstreamConfig struct {
	Servers           []string `yaml:"servers" json:"servers"`
	Strategy          string   `yaml:"strategy" json:"strategy"`
	TimeoutMs         int      `yaml:"timeout_ms" json:"timeout_ms"`
	Concurrency       int      `yaml:"concurrency" json:"concurrency"`
	NxdomainForErrors bool     `yaml:"nxdomain_for_errors" json:"nxdomain_for_errors"`
}

type PingConfig struct {
	Count              int    `yaml:"count" json:"count"`
	TimeoutMs          int    `yaml:"timeout_ms" json:"timeout_ms"`
	Concurrency        int    `yaml:"concurrency" json:"concurrency"`
	Strategy           string `yaml:"strategy" json:"strategy"`
	MaxTestIPs         int    `yaml:"max_test_ips" json:"max_test_ips"`
	RttCacheTtlSeconds int    `yaml:"rtt_cache_ttl_seconds" json:"rtt_cache_ttl_seconds"`
}

type CacheConfig struct {
	FastResponseTTL    int `yaml:"fast_response_ttl" json:"fast_response_ttl"`
	UserReturnTTL      int `yaml:"user_return_ttl" json:"user_return_ttl"`
	MinTTLSeconds      int `yaml:"min_ttl_seconds" json:"min_ttl_seconds"`
	MaxTTLSeconds      int `yaml:"max_ttl_seconds" json:"max_ttl_seconds"`
	NegativeTTLSeconds int `yaml:"negative_ttl_seconds" json:"negative_ttl_seconds"`       // 负向缓存(NXDOMAIN/NODATA)的TTL
	ErrorCacheTTL      int `yaml:"error_cache_ttl_seconds" json:"error_cache_ttl_seconds"` // 错误响应缓存的TTL
}

type PrefetchConfig struct {
	Enabled                    bool `yaml:"enabled" json:"enabled"`
	TopDomainsLimit            int  `yaml:"top_domains_limit" json:"top_domains_limit"`
	RefreshBeforeExpireSeconds int  `yaml:"refresh_before_expire_seconds" json:"refresh_before_expire_seconds"`
}

type WebUIConfig struct {
	Enabled    bool `yaml:"enabled" json:"enabled"`
	ListenPort int  `yaml:"listen_port" json:"listen_port"`
}

type AdBlockConfig struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	RuleFile string `yaml:"rule_file" json:"rule_file"`
}

type SystemConfig struct {
	MaxCPUCores      int `yaml:"max_cpu_cores" json:"max_cpu_cores"`
	SortQueueWorkers int `yaml:"sort_queue_workers" json:"sort_queue_workers"`
	RefreshWorkers   int `yaml:"refresh_workers" json:"refresh_workers"`
}

// CreateDefaultConfig 创建默认配置文件
func CreateDefaultConfig(filePath string) error {
	return os.WriteFile(filePath, []byte(DefaultConfigContent), 0644)
}

// ValidateAndRepairConfig 检查并修复配置文件中缺失的字段
// 该函数会读取现有配置,将其与默认配置合并,补充缺失的字段
func ValidateAndRepairConfig(filePath string) error {
	// 如果文件不存在,直接创建默认配置
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return CreateDefaultConfig(filePath)
	}

	// 读取现有配置
	existingData, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// 解析现有配置到map
	var existingConfig map[string]interface{}
	if err := yaml.Unmarshal(existingData, &existingConfig); err != nil {
		return err
	}

	// 解析默认配置到map
	var defaultConfig map[string]interface{}
	if err := yaml.Unmarshal([]byte(DefaultConfigContent), &defaultConfig); err != nil {
		return err
	}

	// 合并配置(保留现有值,只添加缺失的字段)
	mergeConfig(existingConfig, defaultConfig)

	// 将合并后的配置写回文件
	mergedData, err := yaml.Marshal(existingConfig)
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, mergedData, 0644)
}

// mergeConfig 递归合并配置,将default中存在但existing中不存在的字段添加到existing
func mergeConfig(existing, defaultCfg map[string]interface{}) {
	for key, defaultValue := range defaultCfg {
		if existingValue, exists := existing[key]; exists {
			// 如果existing中存在该key,检查是否为嵌套map
			if existingMap, ok := existingValue.(map[string]interface{}); ok {
				if defaultMap, ok := defaultValue.(map[string]interface{}); ok {
					// 递归合并嵌套map
					mergeConfig(existingMap, defaultMap)
				}
			}
			// 如果不是map或值已存在,保留existing中的值
		} else {
			// 如果existing中不存在该key,添加默认值
			existing[key] = defaultValue
		}
	}
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
		cfg.Upstream.TimeoutMs = 3000
	}
	if cfg.Upstream.Concurrency == 0 {
		cfg.Upstream.Concurrency = 100
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
	if cfg.Ping.MaxTestIPs == 0 {
		cfg.Ping.MaxTestIPs = 8
	}
	if cfg.Ping.RttCacheTtlSeconds == 0 {
		cfg.Ping.RttCacheTtlSeconds = 60
	}
	if cfg.Cache.FastResponseTTL == 0 {
		cfg.Cache.FastResponseTTL = 60
	}
	// Note: MinTTLSeconds 和 MaxTTLSeconds 允许为 0
	// 0 有特殊含义:
	//   - 两个都为 0: 不修改上游 TTL
	//   - 仅 min 为 0: 只限制最大值
	//   - 仅 max 为 0: 只限制最小值
	if cfg.Cache.UserReturnTTL == 0 {
		cfg.Cache.UserReturnTTL = 500
	}
	if cfg.Cache.NegativeTTLSeconds == 0 {
		cfg.Cache.NegativeTTLSeconds = 300
	}
	if cfg.Cache.ErrorCacheTTL == 0 {
		cfg.Cache.ErrorCacheTTL = 30
	}
	if cfg.Prefetch.TopDomainsLimit == 0 {
		cfg.Prefetch.TopDomainsLimit = 1000
	}
	if cfg.Prefetch.RefreshBeforeExpireSeconds == 0 {
		cfg.Prefetch.RefreshBeforeExpireSeconds = 30
	}

	// System defaults
	if cfg.System.MaxCPUCores == 0 {
		cfg.System.MaxCPUCores = runtime.NumCPU() // Default to all available cores
	}
	if cfg.System.SortQueueWorkers == 0 {
		// Default to MaxCPUCores for better concurrency performance
		cfg.System.SortQueueWorkers = cfg.System.MaxCPUCores
	}
	if cfg.System.RefreshWorkers == 0 {
		// Default to MaxCPUCores for better concurrency performance
		cfg.System.RefreshWorkers = cfg.System.MaxCPUCores
	}

	return &cfg, nil
}
