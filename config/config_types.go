package config

// Config 主配置结构
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

// DNSConfig DNS 服务器配置
type DNSConfig struct {
	ListenPort int  `yaml:"listen_port,omitempty" json:"listen_port"`
	EnableTCP  bool `yaml:"enable_tcp" json:"enable_tcp"`
	EnableIPv6 bool `yaml:"enable_ipv6" json:"enable_ipv6"`
}

// UpstreamConfig 上游 DNS 服务器配置
type UpstreamConfig struct {
	Servers []string `yaml:"servers,omitempty" json:"servers"`
	// [新增] 引导 DNS，用于解析 DoH/DoT 的域名
	// 必须是纯 IP，如 "223.5.5.5:53"
	BootstrapDNS []string `yaml:"bootstrap_dns,omitempty" json:"bootstrap_dns"`

	Strategy    string `yaml:"strategy,omitempty" json:"strategy"`
	TimeoutMs   int    `yaml:"timeout_ms,omitempty" json:"timeout_ms"`
	Concurrency int    `yaml:"concurrency,omitempty" json:"concurrency"` // 并行查询时的并发数

	// sequential 策略的单次尝试超时时间（默认 300ms）
	SequentialTimeout int `yaml:"sequential_timeout,omitempty" json:"sequential_timeout"`

	// racing 策略的赛跑起始延迟（默认 100ms）
	RacingDelay int `yaml:"racing_delay,omitempty" json:"racing_delay"`

	// racing 策略中同时发起的最大竞争请求数（默认 2）
	RacingMaxConcurrent int `yaml:"racing_max_concurrent,omitempty" json:"racing_max_concurrent"`

	NxdomainForErrors bool `yaml:"nxdomain_for_errors" json:"nxdomain_for_errors"`
	Dnssec            bool `yaml:"dnssec" json:"dnssec"`

	// 健康检查配置
	HealthCheck HealthCheckConfig `yaml:"health_check,omitempty" json:"health_check"`
}

// HealthCheckConfig 健康检查配置
type HealthCheckConfig struct {
	Enabled                 bool `yaml:"enabled" json:"enabled"`
	FailureThreshold        int  `yaml:"failure_threshold,omitempty" json:"failure_threshold"`
	CircuitBreakerThreshold int  `yaml:"circuit_breaker_threshold,omitempty" json:"circuit_breaker_threshold"`
	CircuitBreakerTimeout   int  `yaml:"circuit_breaker_timeout,omitempty" json:"circuit_breaker_timeout"`
	SuccessThreshold        int  `yaml:"success_threshold,omitempty" json:"success_threshold"`
}

// PingConfig Ping 检测配置
type PingConfig struct {
	Enabled            bool   `yaml:"enabled" json:"enabled"`
	Count              int    `yaml:"count,omitempty" json:"count"`
	TimeoutMs          int    `yaml:"timeout_ms,omitempty" json:"timeout_ms"`
	Concurrency        int    `yaml:"concurrency,omitempty" json:"concurrency"`
	Strategy           string `yaml:"strategy,omitempty" json:"strategy"`
	MaxTestIPs         int    `yaml:"max_test_ips,omitempty" json:"max_test_ips"`
	RttCacheTtlSeconds int    `yaml:"rtt_cache_ttl_seconds,omitempty" json:"rtt_cache_ttl_seconds"`
	EnableHttpFallback bool   `yaml:"enable_http_fallback,omitempty" json:"enable_http_fallback"`
}

// CacheConfig DNS 缓存配置
type CacheConfig struct {
	FastResponseTTL    int `yaml:"fast_response_ttl,omitempty" json:"fast_response_ttl"`
	UserReturnTTL      int `yaml:"user_return_ttl,omitempty" json:"user_return_ttl"`
	MinTTLSeconds      int `yaml:"min_ttl_seconds,omitempty" json:"min_ttl_seconds"`
	MaxTTLSeconds      int `yaml:"max_ttl_seconds,omitempty" json:"max_ttl_seconds"`
	NegativeTTLSeconds int `yaml:"negative_ttl_seconds,omitempty" json:"negative_ttl_seconds"`       // 否定缓存(NXDOMAIN/NODATA)的TTL
	ErrorCacheTTL      int `yaml:"error_cache_ttl_seconds,omitempty" json:"error_cache_ttl_seconds"` // 错误响应缓存的TTL

	// 内存缓存管理 (高级)
	MaxMemoryMB               int     `yaml:"max_memory_mb,omitempty" json:"max_memory_mb"`
	KeepExpiredEntries        bool    `yaml:"keep_expired_entries" json:"keep_expired_entries"`
	EvictionThreshold         float64 `yaml:"eviction_threshold,omitempty" json:"eviction_threshold"`
	EvictionBatchPercent      float64 `yaml:"eviction_batch_percent,omitempty" json:"eviction_batch_percent"`
	ProtectPrefetchDomains    bool    `yaml:"protect_prefetch_domains" json:"protect_prefetch_domains"`
	SaveToDiskIntervalMinutes int     `yaml:"save_to_disk_interval_minutes" json:"save_to_disk_interval_minutes"`

	// DNSSEC 消息缓存容量
	MsgCacheSizeMB int `yaml:"msg_cache_size_mb,omitempty" json:"msg_cache_size_mb"`
}

// PrefetchConfig 预取配置
type PrefetchConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// WebUIConfig Web UI 管理界面配置
type WebUIConfig struct {
	Enabled    bool `yaml:"enabled" json:"enabled"`
	ListenPort int  `yaml:"listen_port,omitempty" json:"listen_port"`
}

// AdBlockConfig 广告拦截配置
type AdBlockConfig struct {
	Enable              bool     `yaml:"enable" json:"enable"`
	Engine              string   `yaml:"engine,omitempty" json:"engine"`
	RuleURLs            []string `yaml:"rule_urls,omitempty" json:"rule_urls"`
	CustomRulesFile     string   `yaml:"custom_rules_file,omitempty" json:"custom_rules_file"`
	CustomResponseFile  string   `yaml:"custom_response_file,omitempty" json:"custom_response_file"`
	CacheDir            string   `yaml:"cache_dir,omitempty" json:"cache_dir"`
	UpdateIntervalHours int      `yaml:"update_interval_hours,omitempty" json:"update_interval_hours"`
	MaxCacheAgeHours    int      `yaml:"max_cache_age_hours,omitempty" json:"max_cache_age_hours"`
	MaxCacheSizeMB      int      `yaml:"max_cache_size_mb,omitempty" json:"max_cache_size_mb"`
	BlockMode           string   `yaml:"block_mode,omitempty" json:"block_mode"`
	BlockedResponseIP   string   `yaml:"blocked_response_ip,omitempty" json:"blocked_response_ip"`
	BlockedTTL          int      `yaml:"blocked_ttl,omitempty" json:"blocked_ttl"`
	LastUpdate          int64    `yaml:"last_update,omitempty" json:"last_update"`
	FailedSources       []string `yaml:"failed_sources,omitempty" json:"failed_sources"`
}

// SystemConfig 系统资源配置
type SystemConfig struct {
	MaxCPUCores      int    `yaml:"max_cpu_cores,omitempty" json:"max_cpu_cores"`
	SortQueueWorkers int    `yaml:"sort_queue_workers,omitempty" json:"sort_queue_workers"`
	RefreshWorkers   int    `yaml:"refresh_workers,omitempty" json:"refresh_workers"`
	LogLevel         string `yaml:"log_level,omitempty" json:"log_level"`
}

// StatsConfig 统计配置
type StatsConfig struct {
	HotDomainsWindowHours   int `yaml:"hot_domains_window_hours,omitempty" json:"hot_domains_window_hours"`
	HotDomainsBucketMinutes int `yaml:"hot_domains_bucket_minutes,omitempty" json:"hot_domains_bucket_minutes"`
	HotDomainsShardCount    int `yaml:"hot_domains_shard_count,omitempty" json:"hot_domains_shard_count"`
	HotDomainsMaxPerBucket  int `yaml:"hot_domains_max_per_bucket,omitempty" json:"hot_domains_max_per_bucket"`
}
