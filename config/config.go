package config

import (
	"os"
	"runtime"

	"gopkg.in/yaml.v3"
)

// 默认配置文件内容，包含详细说明
const DefaultConfigContent = `# SmartDNSSort 配置文件

# DNS 服务器配置
dns:
  # DNS 监听端口，默认 53
  listen_port: 53
  # 是否启用 TCP 协议（用于大型 DNS 查询），默认 true
  enable_tcp: true
  # 是否启用 IPv6 支持，默认 true
  enable_ipv6: true

# 上游 DNS 服务器配置
upstream:
  # 上游 DNS 服务器地址列表
  # 支持多种协议格式:
  # - UDP: "8.8.8.8:53" 或 "8.8.8.8" (默认端口53)
  # - TCP: "tcp://8.8.8.8:53"
  # - DoH: "https://dns.google/dns-query" 或 "https://1.1.1.1/dns-query"
  # - DoT: "tls://dns.google:853" 或 "tls://1.1.1.1:853"
  servers:
    - "192.168.1.10"
    # UDP 示例
#    - "8.8.8.8:53"
    # TCP 示例
#    - "tcp://8.8.8.8:53"
    # DoH 示例
    - "https://doh.pub/dns-query"
    - "https://dns.google/dns-query"
    - "https://cloudflare-dns.com/dns-query"
    # DoT 示例
#    - "tls://dot.pub:853"
#    - "tls://dns.google:853"
  
  # [新增] 引导 DNS
  # 必须是纯 IP。用于解析 DoH/DoT URL 中的域名 (如 dns.google)
  bootstrap_dns:
    - "192.168.1.11"
    - "8.8.8.8:53"

  # 查询策略：parallel（并行查询所有服务器），random（随机选择一个服务器）
  strategy: "random"
  # 上游服务器响应超时时间（毫秒）
  timeout_ms: 5000
  # 并行查询时的并发数（仅在 strategy 为 parallel 时有效）
  concurrency: 3

  # 是否将未处理的 SERVFAIL, timeout 转换为 NXDOMAIN 响应给客户端，默认 true
  # 这可以减少客户端的失败重试行为，但可能会隐藏上游服务器的真实错误
  nxdomain_for_errors: true

  # 健康检查和熔断器配置
  health_check:
    # 是否启用健康检查，默认 true
    enabled: true
    # 连续失败多少次后进入降级状态，默认 3
    failure_threshold: 3
    # 连续失败多少次后进入熔断状态（停止使用该服务器），默认 5
    circuit_breaker_threshold: 5
    # 熔断后多久尝试恢复（秒），默认 30
    circuit_breaker_timeout: 30
    # 连续成功多少次后从降级/熔断状态恢复，默认 2
    success_threshold: 2


# Ping 检测配置，用于选择最优的 DNS 服务器
ping:
  # 每次 Ping 的数据包数量
  count: 3
  # Ping 响应超时时间（毫秒）
  timeout_ms: 1000
  # 并发 Ping 数量
  concurrency: 16
  # 选择策略：min（选择最小延迟），avg（选择平均延迟）
  strategy: "min"
  # 每个域名测试的 IP 数量，0 表示不限制
  max_test_ips: 0
  # 缓存 IP 的 RTT (延迟) 结果的时间（秒）
  rtt_cache_ttl_seconds: 60

# DNS 缓存配置
cache:
  # 首次查询或未在缓存中时使用的 TTL（快速响应），默认值 15
  fast_response_ttl: 15
  # 正常返回给客户端的 TTL，默认值 600
  user_return_ttl: 600
  # 最小 TTL（秒）
  # 设置为 0 表示不限制。如果 min 和 max 都为 0，不修改原始 TTL。当 min > 0 时只增加过小的 TTL
  min_ttl_seconds: 3600
  # 最大 TTL（秒）
  # 设置为 0 表示不限制。如果 min 和 max 都为 0，不修改原始 TTL。当 max > 0 时只减小过大的 TTL
  max_ttl_seconds: 84600
  # 否定缓存（NXDOMAIN/无记录）的 TTL（秒），默认值 300
  negative_ttl_seconds: 300
  # 错误响应缓存（SERVFAIL/REFUSED等）的 TTL（秒），默认值 30
  error_cache_ttl_seconds: 30

  # 内存缓存管理 (高级)
  # 最大内存使用量 (MB)。超过此限制将触发LRU淘汰。0表示不限制。
  max_memory_mb: 128
  # 是否保留已过期的缓存条目。当内存充足时，可设为 true 以加速后续查询。
  keep_expired_entries: true
  # 内存使用达到此百分比阈值时，触发淘汰机制 (0.7-0.95)。
  eviction_threshold: 0.9
  # 每次淘汰时，清理缓存总量的百分比 (0.05-0.2)。
  eviction_batch_percent: 0.1
  # 在LRU淘汰期间，是否保护预取列表中的域名不被清除。
  protect_prefetch_domains: true

# 预取配置（提前刷新缓存）
prefetch:
  # 是否启用预取功能
  enabled: false
  # 记录访问频率最高的 N 个域名
  top_domains_limit: 100
  # 在缓存即将过期前指定的时间进行后台异步刷新
  refresh_before_expire_seconds: 10

# Web UI 管理界面配置
webui:
  # 是否启用 Web 管理界面，默认 true
  enabled: true
  # Web 管理界面端口，默认 8080
  listen_port: 8080

# 广告拦截配置
adblock:
  enable: true
  engine: urlfilter
  rule_urls:
    - https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt
  custom_rules_file: ./adblock_cache/custom_rules.txt
  cache_dir: ./adblock_cache
  update_interval_hours: 168
  max_cache_age_hours: 168
  max_cache_size_mb: 30
  block_mode: nxdomain
  blocked_ttl: 3600

# 系统资源配置
system:
  # 最大 CPU 核心数，0 表示不限制（使用全部可用核心）
  max_cpu_cores: 0
  # IP 排序队列的工作线程数，0 表示根据 CPU 核心数自动调整
  sort_queue_workers: 0
  # 异步缓存刷新工作线程数，0 表示根据 CPU 核心数自动调整
  refresh_workers: 0
  # 日志级别: debug, info, warn, error. 默认 info
  log_level: "info"
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
	Stats    StatsConfig    `yaml:"stats" json:"stats"`
}

type DNSConfig struct {
	ListenPort int  `yaml:"listen_port" json:"listen_port"`
	EnableTCP  bool `yaml:"enable_tcp" json:"enable_tcp"`
	EnableIPv6 bool `yaml:"enable_ipv6" json:"enable_ipv6"`
}

type UpstreamConfig struct {
	Servers []string `yaml:"servers" json:"servers"`
	// [新增] 引导 DNS，用于解析 DoH/DoT 的域名
	// 必须是纯 IP，如 "223.5.5.5:53"
	BootstrapDNS []string `yaml:"bootstrap_dns" json:"bootstrap_dns"`

	Strategy    string `yaml:"strategy" json:"strategy"`
	TimeoutMs   int    `yaml:"timeout_ms" json:"timeout_ms"`
	Concurrency int    `yaml:"concurrency" json:"concurrency"` // 并行查询时的并发数

	NxdomainForErrors bool `yaml:"nxdomain_for_errors" json:"nxdomain_for_errors"`

	// 健康检查配置
	HealthCheck HealthCheckConfig `yaml:"health_check" json:"health_check"`
}

// HealthCheckConfig 健康检查配置
type HealthCheckConfig struct {
	Enabled                 bool `yaml:"enabled" json:"enabled"`
	FailureThreshold        int  `yaml:"failure_threshold" json:"failure_threshold"`
	CircuitBreakerThreshold int  `yaml:"circuit_breaker_threshold" json:"circuit_breaker_threshold"`
	CircuitBreakerTimeout   int  `yaml:"circuit_breaker_timeout" json:"circuit_breaker_timeout"`
	SuccessThreshold        int  `yaml:"success_threshold" json:"success_threshold"`
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
	NegativeTTLSeconds int `yaml:"negative_ttl_seconds" json:"negative_ttl_seconds"`       // 否定缓存(NXDOMAIN/NODATA)的TTL
	ErrorCacheTTL      int `yaml:"error_cache_ttl_seconds" json:"error_cache_ttl_seconds"` // 错误响应缓存的TTL

	// 内存缓存管理 (高级)
	MaxMemoryMB            int     `yaml:"max_memory_mb" json:"max_memory_mb"`
	KeepExpiredEntries     bool    `yaml:"keep_expired_entries" json:"keep_expired_entries"`
	EvictionThreshold      float64 `yaml:"eviction_threshold" json:"eviction_threshold"`
	EvictionBatchPercent   float64 `yaml:"eviction_batch_percent" json:"eviction_batch_percent"`
	ProtectPrefetchDomains bool    `yaml:"protect_prefetch_domains" json:"protect_prefetch_domains"`
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
	Enable              bool     `yaml:"enable" json:"enable"`
	Engine              string   `yaml:"engine" json:"engine"`
	RuleURLs            []string `yaml:"rule_urls" json:"rule_urls"`
	CustomRulesFile     string   `yaml:"custom_rules_file" json:"custom_rules_file"`
	CacheDir            string   `yaml:"cache_dir" json:"cache_dir"`
	UpdateIntervalHours int      `yaml:"update_interval_hours" json:"update_interval_hours"`
	MaxCacheAgeHours    int      `yaml:"max_cache_age_hours" json:"max_cache_age_hours"`
	MaxCacheSizeMB      int      `yaml:"max_cache_size_mb" json:"max_cache_size_mb"`
	BlockMode           string   `yaml:"block_mode" json:"block_mode"`
	BlockedResponseIP   string   `yaml:"blocked_response_ip" json:"blocked_response_ip"`
	BlockedTTL          int      `yaml:"blocked_ttl" json:"blocked_ttl"`
	LastUpdate          int64    `yaml:"last_update" json:"last_update"`
	FailedSources       []string `yaml:"failed_sources" json:"failed_sources"`
}

type SystemConfig struct {
	MaxCPUCores      int    `yaml:"max_cpu_cores" json:"max_cpu_cores"`
	SortQueueWorkers int    `yaml:"sort_queue_workers" json:"sort_queue_workers"`
	RefreshWorkers   int    `yaml:"refresh_workers" json:"refresh_workers"`
	LogLevel         string `yaml:"log_level" json:"log_level"`
}

type StatsConfig struct {
	HotDomainsWindowHours   int `yaml:"hot_domains_window_hours" json:"hot_domains_window_hours"`
	HotDomainsBucketMinutes int `yaml:"hot_domains_bucket_minutes" json:"hot_domains_bucket_minutes"`
	HotDomainsShardCount    int `yaml:"hot_domains_shard_count" json:"hot_domains_shard_count"`
	HotDomainsMaxPerBucket  int `yaml:"hot_domains_max_per_bucket" json:"hot_domains_max_per_bucket"`
}

const (
	// AvgBytesPerDomain 估算每个域名在缓存中占用的平均字节数（包含A和AAAA记录及辅助结构）
	// 基于以下假设：RawCacheEntry、SortedCacheEntry、map开销、域名/IP字符串存储
	// 估算细节 (字节):
	// - RawCacheEntry (含IP): ~80
	// - SortedCacheEntry (含IP): ~40
	// - 域名字符串 (e.g., "www.example.com"): ~20
	// - IPs (4个IPv4, 4个IPv6): (4 * 16) + (4 * 46) = 64 + 184 = 248
	// - Map 开销 (rawCache, sortedCache 等): ~100
	// - 预取列表条目: ~50
	// - 其他运行时开销: 282
	// 总计: 80+40+20+248+100+50+282 = 820
	AvgBytesPerDomain = 820
)

// CalculateMaxEntries 根据最大内存限制计算最大缓存条目数
func (c *CacheConfig) CalculateMaxEntries() int {
	if c.MaxMemoryMB <= 0 {
		// 如果未设置最大内存限制，返回 0 表示不限制
		return 0
	}
	// 最大内存 (字节) / 每个域名的平均字节数
	return (c.MaxMemoryMB * 1024 * 1024) / AvgBytesPerDomain
}

// CreateDefaultConfig 创建默认配置文件
func CreateDefaultConfig(filePath string) error {
	return os.WriteFile(filePath, []byte(DefaultConfigContent), 0644)
}

// ValidateAndRepairConfig 验证并修复配置文件中缺失的字段
// 该函数只在配置文件不存在时创建默认配置
// 如果配置文件已存在，不做任何修改，以保留用户的注释和自定义配置
func ValidateAndRepairConfig(filePath string) error {
	// 如果文件不存在, 直接创建默认配置
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return CreateDefaultConfig(filePath)
	}

	// 配置文件已存在，不做任何修改
	// 这样可以保留用户的注释和零值配置（如 max_cpu_cores: 0）
	// LoadConfig 函数会负责设置缺失字段的默认值
	return nil
}

// LoadConfig 从YAML文件加载配置
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
		cfg.Upstream.TimeoutMs = 5000
	}
	if cfg.Upstream.Concurrency == 0 {
		cfg.Upstream.Concurrency = 3
	}
	// Bootstrap DNS 默认值：如果未配置，使用公共 DNS 服务器
	// 用于解析 DoH/DoT 服务器的域名
	// 注意：这里不设置默认值，因为 DefaultConfigContent 中已经定义了
	// 如果用户删除了这个配置，我们才使用公共 DNS
	if len(cfg.Upstream.BootstrapDNS) == 0 {
		cfg.Upstream.BootstrapDNS = []string{"8.8.8.8", "1.1.1.1", "8.8.4.4", "1.0.0.1"}
	}

	// 健康检查默认值
	if cfg.Upstream.HealthCheck.FailureThreshold == 0 {
		cfg.Upstream.HealthCheck.FailureThreshold = 3
	}
	if cfg.Upstream.HealthCheck.CircuitBreakerThreshold == 0 {
		cfg.Upstream.HealthCheck.CircuitBreakerThreshold = 5
	}
	if cfg.Upstream.HealthCheck.CircuitBreakerTimeout == 0 {
		cfg.Upstream.HealthCheck.CircuitBreakerTimeout = 30
	}
	if cfg.Upstream.HealthCheck.SuccessThreshold == 0 {
		cfg.Upstream.HealthCheck.SuccessThreshold = 2
	}

	if cfg.Ping.Count == 0 {
		cfg.Ping.Count = 3
	}
	if cfg.Ping.TimeoutMs == 0 {
		cfg.Ping.TimeoutMs = 1000
	}
	if cfg.Ping.Concurrency == 0 {
		cfg.Ping.Concurrency = 16
	}
	// MaxTestIPs: 0 means unlimited, so we don't need to set a default if it's 0.
	if cfg.Ping.RttCacheTtlSeconds == 0 {
		cfg.Ping.RttCacheTtlSeconds = 60 // 与 DefaultConfigContent 保持一致
	}
	if cfg.Cache.FastResponseTTL == 0 {
		cfg.Cache.FastResponseTTL = 15 // 与 DefaultConfigContent 保持一致
	}
	// Note: MinTTLSeconds 和 MaxTTLSeconds 默认为 0
	// 0 表示不限制
	//   - 都设置为 0: 不修改原始 TTL
	//   - 仅 min > 0: 只增加过小的 TTL
	//   - 仅 max > 0: 只减小过大的 TTL
	if cfg.Cache.UserReturnTTL == 0 {
		cfg.Cache.UserReturnTTL = 600 // 与 DefaultConfigContent 保持一致
	}
	if cfg.Cache.NegativeTTLSeconds == 0 {
		cfg.Cache.NegativeTTLSeconds = 300
	}
	if cfg.Cache.ErrorCacheTTL == 0 {
		cfg.Cache.ErrorCacheTTL = 30
	}

	// 新增内存管理配置的默认值
	if cfg.Cache.MaxMemoryMB == 0 {
		cfg.Cache.MaxMemoryMB = 128
	}
	if cfg.Cache.EvictionThreshold == 0 {
		cfg.Cache.EvictionThreshold = 0.9
	}
	if cfg.Cache.EvictionBatchPercent == 0 {
		cfg.Cache.EvictionBatchPercent = 0.1
	}

	if cfg.Prefetch.TopDomainsLimit == 0 {
		cfg.Prefetch.TopDomainsLimit = 100 // 与 DefaultConfigContent 保持一致
	}
	if cfg.Prefetch.RefreshBeforeExpireSeconds == 0 {
		cfg.Prefetch.RefreshBeforeExpireSeconds = 10
	}

	// AdBlock defaults
	if cfg.AdBlock.Engine == "" {
		cfg.AdBlock.Engine = "urlfilter"
	}
	if cfg.AdBlock.CustomRulesFile == "" {
		cfg.AdBlock.CustomRulesFile = "./custom_rules.txt"
	}
	if cfg.AdBlock.CacheDir == "" {
		cfg.AdBlock.CacheDir = "./adblock_cache"
	}
	if cfg.AdBlock.UpdateIntervalHours == 0 {
		cfg.AdBlock.UpdateIntervalHours = 168 // 与 DefaultConfigContent 保持一致
	}
	if cfg.AdBlock.MaxCacheAgeHours == 0 {
		cfg.AdBlock.MaxCacheAgeHours = 168
	}
	if cfg.AdBlock.MaxCacheSizeMB == 0 {
		cfg.AdBlock.MaxCacheSizeMB = 30
	}
	if cfg.AdBlock.BlockMode == "" {
		cfg.AdBlock.BlockMode = "nxdomain"
	}
	if cfg.AdBlock.BlockedTTL == 0 {
		cfg.AdBlock.BlockedTTL = 3600
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
	if cfg.System.LogLevel == "" {
		cfg.System.LogLevel = "info"
	}

	// Stats defaults
	if cfg.Stats.HotDomainsWindowHours == 0 {
		cfg.Stats.HotDomainsWindowHours = 24
	}
	if cfg.Stats.HotDomainsBucketMinutes == 0 {
		cfg.Stats.HotDomainsBucketMinutes = 60
	}
	if cfg.Stats.HotDomainsShardCount == 0 {
		cfg.Stats.HotDomainsShardCount = 16
	}
	if cfg.Stats.HotDomainsMaxPerBucket == 0 {
		cfg.Stats.HotDomainsMaxPerBucket = 5000
	}

	return &cfg, nil
}
