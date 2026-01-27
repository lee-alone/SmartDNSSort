package config

import (
	"runtime"
	"strings"
)

// setDefaultValues 设置配置文件中缺失字段的默认值
func setDefaultValues(cfg *Config, rawData []byte) {
	// DNS 配置默认值
	if cfg.DNS.ListenPort == 0 {
		cfg.DNS.ListenPort = 53
	}

	// Upstream 配置默认值
	if cfg.Upstream.TimeoutMs == 0 {
		cfg.Upstream.TimeoutMs = 5000
	}
	// Bootstrap DNS 默认值：如果未配置，使用公共 DNS 服务器
	// 用于解析 DoH/DoT 服务器的域名
	// 注意：这里不设置默认值，因为 DefaultConfigContent 中已经定义了
	// 如果用户删除了这个配置，我们才使用公共 DNS
	if len(cfg.Upstream.BootstrapDNS) == 0 {
		cfg.Upstream.BootstrapDNS = []string{"8.8.8.8", "1.1.1.1", "8.8.4.4", "1.0.0.1"}
	}

	// 健康检查默认值
	setHealthCheckDefaults(&cfg.Upstream.HealthCheck)

	// Ping 配置默认值
	setPingDefaults(cfg, rawData)

	// Cache 配置默认值
	setCacheDefaults(cfg)

	// AdBlock 配置默认值
	setAdBlockDefaults(cfg)

	// System 配置默认值
	setSystemDefaults(cfg)

	// Stats 配置默认值
	setStatsDefaults(cfg)
}

// setHealthCheckDefaults 设置健康检查配置的默认值
func setHealthCheckDefaults(hc *HealthCheckConfig) {
	if hc.FailureThreshold == 0 {
		hc.FailureThreshold = 3 // 优化：从 5 改为 3，更快进入降级状态
	}
	if hc.CircuitBreakerThreshold == 0 {
		hc.CircuitBreakerThreshold = 3 // 优化：从 10 改为 3，更快进入熔断状态
	}
	if hc.CircuitBreakerTimeout == 0 {
		hc.CircuitBreakerTimeout = 30 // 保持不变，但使用指数退避
	}
	if hc.SuccessThreshold == 0 {
		hc.SuccessThreshold = 2
	}
}

// setPingDefaults 设置 Ping 配置的默认值
func setPingDefaults(cfg *Config, rawData []byte) {
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
		cfg.Ping.RttCacheTtlSeconds = 600 // 与 DefaultConfigContent 保持一致
	}
	// Set default for Ping.Enabled.
	// If the config file exists but doesn't specify 'ping.enabled',
	// cfg.Ping.Enabled will be false after unmarshalling. We want it to be true.
	// If the config file explicitly sets 'ping.enabled: false', it should remain false.
	// If the config file explicitly sets 'ping.enabled: true', it should remain true.
	//
	// To distinguish between 'omitted' and 'explicitly false', we check the raw YAML data.
	// This allows respecting explicit 'false' from users while defaulting omitted fields to 'true'.
	// Note: This relies on 'data' (raw config bytes) still being available.
	if !cfg.Ping.Enabled && !strings.Contains(string(rawData), "\nping:\n  enabled: false") {
		cfg.Ping.Enabled = true
	}
}

// setCacheDefaults 设置缓存配置的默认值
func setCacheDefaults(cfg *Config) {
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
	if cfg.Cache.MsgCacheSizeMB == 0 {
		// 默认为主缓存的 1/10
		cfg.Cache.MsgCacheSizeMB = cfg.Cache.MaxMemoryMB / 10
		cfg.Cache.MsgCacheSizeMB = max(cfg.Cache.MsgCacheSizeMB, 1) // 最小 1MB
	}
	if cfg.Cache.DNSSECMsgCacheTTLSeconds == 0 {
		cfg.Cache.DNSSECMsgCacheTTLSeconds = 300 // 默认 5 分钟
	}
	if cfg.Cache.SaveToDiskIntervalMinutes == 0 {
		cfg.Cache.SaveToDiskIntervalMinutes = 60
	}
}

// setAdBlockDefaults 设置广告拦截配置的默认值
func setAdBlockDefaults(cfg *Config) {
	if cfg.AdBlock.Engine == "" {
		cfg.AdBlock.Engine = "urlfilter"
	}
	if cfg.AdBlock.CustomRulesFile == "" {
		cfg.AdBlock.CustomRulesFile = "./custom_rules.txt"
	}
	if cfg.AdBlock.CustomResponseFile == "" {
		cfg.AdBlock.CustomResponseFile = "./adblock_cache/custom_response_rules.txt"
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
		cfg.AdBlock.BlockMode = "zero_ip"
	}
	if cfg.AdBlock.BlockedTTL == 0 {
		cfg.AdBlock.BlockedTTL = 3600
	}
}

// setSystemDefaults 设置系统配置的默认值
func setSystemDefaults(cfg *Config) {
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
}

// setStatsDefaults 设置统计配置的默认值
func setStatsDefaults(cfg *Config) {
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
}
