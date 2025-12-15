/**
 * Lightweight i18n library for SmartDNSSort
 * Based on JSON + data-i18n attribute
 */

const resources = {
    "en": {
        "app": {
            "title": "SmartDNSSort"
        },
        "tabs": {
            "dashboard": "Dashboard",
            "config": "Configuration",
            "custom": "Custom Rules",
            "adblock": "AdBlock"
        },
        "actions": {
            "clearCache": "Clear DNS Cache",
            "clearStats": "Clear All Stats",
            "refresh": "Refresh",
            "restart": "Restart Service"
        },
        "status": {
            "connecting": "Connecting...",
            "connected": "Connected",
            "disconnected": "Disconnected",
            "error": "Error: Could not fetch stats."
        },
        "dashboard": {
            "generalStats": "General Stats",
            "totalQueries": "Total Queries",
            "cacheHits": "Cache Hits",
            "cacheMisses": "Cache Misses",
            "cacheHitRate": "Cache Hit Rate",
            "upstreamFailures": "Upstream Failures",
            "systemStatus": "System Status",
            "cpuUsage": "CPU Usage",
            "memoryUsage": "Memory Usage",
            "goroutines": "Goroutines",
            "adblockStatus": "AdBlock Status",
            "updateRules": "Update All Rules Now",
            "enableAdblock": "Enable AdBlock",
            "engine": "Engine",
            "totalRules": "Total Rules",
            "blockedToday": "Blocked Today",
            "blockedTotal": "Blocked Total",
            "lastUpdate": "Last Update",
            "memoryUsageTitle": "Memory Usage",
            "cacheEntries": "Cache Entries",
            "expiredEntries": "Expired Entries",
            "protectedEntries": "Protected Entries",
            "upstreamServers": "Upstream Servers",
            "server": "Server",
            "success": "Success",
            "failure": "Failure",
            "hotDomains": "Hot Domains (Top 10)",
            "domain": "Domain",
            "count": "Count",
            "recentQueries": "Recent Queries",
            "noDomainData": "No domain data yet.",
            "noRecentQueries": "No recent queries.",
            "errorLoadingData": "Error loading data."
        },
        "config": {
            "nav": {
                "dns": "DNS Service",
                "upstream": "Upstream",
                "ping": "Ping",
                "cache": "Cache",
                "prefetch": "Prefetch",
                "webui": "Web UI",
                "system": "System"
            },
            "dns": {
                "legend": "DNS Service",
                "listenPort": "DNS Listen Port",
                "enableTcp": "Enable TCP",
                "enableIpv6": "Enable IPv6"
            },
            "upstream": {
                "legend": "Upstream",
                "servers": "Upstream Servers",
                "serversHelp": "One server per line.",
                "bootstrapDns": "Bootstrap DNS",
                "bootstrapDnsHelp": "Bootstrap DNS servers (must be IP addresses) for resolving DoH/DoT domain names. One server per line.",
                "strategy": {
                    "_label": "Strategy",
                    "random": "Random",
                    "parallel": "Parallel",
                    "sequential": "Sequential",
                    "racing": "Racing"
                },
                "timeout": "Timeout (ms)",
                "concurrency": "Concurrency",
                "sequentialTimeout": "Sequential Timeout (ms)",
                "sequentialTimeoutHelp": "Single attempt timeout for sequential strategy (100-2000ms).",
                "racingDelay": "Racing Delay (ms)",
                "racingDelayHelp": "Initial delay for racing strategy (50-500ms).",
                "racingMaxConcurrent": "Racing Max Concurrent",
                "racingMaxConcurrentHelp": "Maximum concurrent requests for racing strategy (2-5).",
                "nxdomainForErrors": "Return NXDOMAIN for Upstream Errors",
                "healthCheck": {
                    "legend": "Health Check & Circuit Breaker",
                    "enabled": "Enable Health Checks",
                    "failureThreshold": "Failure Threshold",
                    "failureThresholdHelp": "Number of consecutive failures before degrading upstream server.",
                    "circuitBreakerThreshold": "Circuit Breaker Threshold",
                    "circuitBreakerThresholdHelp": "Number of consecutive failures before stopping use of upstream server.",
                    "circuitBreakerTimeout": "Circuit Breaker Timeout (s)",
                    "circuitBreakerTimeoutHelp": "Duration in seconds before retrying a broken circuit.",
                    "successThreshold": "Success Threshold to Restore",
                    "successThresholdHelp": "Number of consecutive successes to recover from degraded/broken state."
                }
            },
            "ping": {
                "legend": "Ping",
                "enabled": "Enable IP Optimization",
                "enabledHelp": "Whether to test and sort IPs from DNS resolution results.",
                "count": "Count",
                "timeout": "Timeout (ms)",
                "concurrency": "Concurrency",
                "strategy": "Strategy",
                "maxTestIps": "Max Test IPs",
                "maxTestIpsHelp": "Maximum number of IPs to test per sort (0 = unlimited).",
                "rttCacheTtl": "RTT Cache TTL (s)",
                "rttCacheTtlHelp": "Cache duration for RTT results (0 = disabled).",
                "enableHttpFallback": "Enable HTTP Fallback",
                "enableHttpFallbackHelp": "Fall back to HTTP-based ping if ICMP fails."
            },
            "cache": {
                "legend": "Cache",
                "fastResponseTtl": "Fast Response TTL (s)",
                "userReturnTtl": "User Return TTL (s)",
                "minTtl": "Min TTL (s)",
                "maxTtl": "Max TTL (s)",
                "negativeTtl": "Negative Cache TTL (s)",
                "negativeTtlHelp": "TTL for NXDOMAIN/NODATA responses.",
                "errorCacheTtl": "Error Cache TTL (s)",
                "errorCacheTtlHelp": "TTL for SERVFAIL/REFUSED responses.",
                "memoryLegend": "Memory Cache Management",
                "maxMemory": "Max Memory (MB)",
                "maxMemoryHelp": "Max memory usage for the cache. Eviction is triggered beyond this limit. 0 for unlimited.",
                "evictionThreshold": "Eviction Threshold",
                "evictionThresholdHelp": "Memory usage percentage (0.7-0.95) that triggers eviction.",
                "evictionBatchPercent": "Eviction Batch Percent",
                "evictionBatchPercentHelp": "Percentage of total cache to evict in a single batch (0.05-0.2).",
                "keepExpired": "Keep Expired Entries",
                "keepExpiredHelp": "Keep expired entries in memory if space is available to speed up subsequent queries.",
                "protectPrefetch": "Protect Prefetched Domains",
                "protectPrefetchHelp": "Prevent domains in the prefetch list from being evicted during LRU cleanup.",
                "saveToDiskInterval": "Save to Disk Interval (Minutes)",
                "saveToDiskIntervalHelp": "How often to persist cache to disk (0 to disable). Set to 0 to disable periodic saves."
            },
            "prefetch": {
                "legend": "Prefetch",
                "enable": "Enable Prefetch",
                "help": "Prefetch uses an advanced mathematical model to automatically refresh popular domains before their cache expires. Suggested to enable only in high-traffic environments."
            },
            "webui": {
                "legend": "Web UI",
                "enable": "Enable Web UI",
                "listenPort": "Web Listen Port"
            },
            "system": {
                "legend": "System",
                "maxCpuCores": "Max CPU Cores",
                "maxCpuCoresHelp": "Set to 0 to use all available cores.",
                "sortQueueWorkers": "Sort Queue Workers",
                "sortQueueWorkersHelp": "Number of parallel sorting tasks.",
                "refreshWorkers": "Refresh Workers",
                "refreshWorkersHelp": "Number of async cache refresh workers."
            },
            "other": {
                "legend": "System & Advanced Settings"
            },
            "save": "Save & Apply"
        },
        "adblock": {
            "ruleSources": "Rule Sources",
            "url": "URL",
            "status": "Status",
            "rules": "Rules",
            "lastUpdate": "Last Update",
            "enabled": "Enabled",
            "actions": "Actions",
            "addNew": "Add New Source",
            "add": "Add",
            "placeholderUrl": "https://example.com/rules.txt",
            "blockMode": "Block Mode",
            "saveBlockMode": "Save",
            "blockModeHelp": "Select how blocked domains should be handled",
            "testDomain": "Test Domain",
            "test": "Test",
            "placeholderDomain": "example.com",
            "settings": "AdBlock Settings",
            "updateInterval": "Update Interval (Hours)",
            "updateIntervalHelp": "How often to automatically update rule lists. 0 to disable.",
            "maxCacheAge": "Max Cache Age (Hours)",
            "maxCacheAgeHelp": "Maximum age of cached rule files before forcing a re-download. 0 to disable.",
            "maxCacheSize": "Max Cache Size (MB)",
            "maxCacheSizeHelp": "Total size limit for all cached rule files.",
            "blockedTtl": "Blocked Response TTL (Seconds)",
            "blockedTtlHelp": "TTL for the response to a blocked domain query.",
            "saveSettings": "Save Settings",
            "statusDisabled": "Disabled",
            "statusEnabled": "Enabled",
            "statusError": "Error",
            "noSources": "No rule sources configured.",
            "errorLoadingSources": "Error loading sources."
        },
        "footer": {
            "copyright": "© 2025 SmartDNSSort."
        },
        "messages": {
            "confirmClearCache": "Are you sure you want to clear the DNS cache?",
            "cacheCleared": "DNS cache cleared successfully.",
            "cacheClearFailed": "Failed to clear DNS cache.",
            "cacheClearError": "An error occurred while trying to clear the DNS cache.",
            "confirmClearStats": "Are you sure you want to clear all statistics?",
            "statsCleared": "All statistics cleared successfully.",
            "statsClearFailed": "Failed to clear statistics.",
            "statsClearError": "An error occurred while trying to clear statistics.",
            "configSaved": "Configuration saved and applied successfully.",
            "configSaveError": "Error saving configuration: {error}",
            "configSaveErrorGeneric": "An error occurred while saving the configuration.",
            "restartConfirm": "Are you sure you want to restart the service?",
            "restarting": "Restarting service...",
            "restartSuccess": "Service restarted successfully.",
            "restartError": "Error restarting service: {error}",
            "restartFailed": "Failed to restart service.",
            "adblockRulesUpdated": "AdBlock rules updated successfully.",
            "adblockRulesUpdateError": "Error updating AdBlock rules: {error}",
            "adblockSourceAdded": "AdBlock source added successfully.",
            "adblockSourceAddError": "Error adding AdBlock source: {error}",
            "adblockSourceDeleted": "AdBlock source deleted successfully.",
            "adblockSourceDeleteError": "Error deleting AdBlock source: {error}",
            "adblockBlockModeSaved": "Block mode saved successfully.",
            "adblockBlockModeSaveError": "Error saving block mode: {error}",
            "adblockSettingsSaved": "AdBlock settings saved successfully.",
            "adblockSettingsSaveError": "Error saving AdBlock settings: {error}",
            "adblockSettingsSaveErrorGeneric": "An error occurred while saving AdBlock settings.",
            "adblockTestResult": "Result: {result}",
            "deleteConfirm": "Are you sure you want to delete this source?",
            "adblockToggleError": "Error toggling AdBlock: {error}",
            "adblockUpdateConfirm": "Are you sure you want to force an update of all adblock rules? This may take a moment.",
            "adblockUpdateStarted": "AdBlock rule update started in the background.",
            "adblockUpdateFailed": "Failed to start update: {error}",
            "adblockUpdateError": "An error occurred: {error}",
            "enterUrl": "Please enter a URL for the new rule source.",
            "enterDomain": "Please enter a domain to test.",
            "testing": "Testing...",
            "blocked": "Blocked!",
            "rule": "Rule",
            "notBlocked": "Not Blocked.",
            "testError": "An error occurred during the test.",
            "selectBlockMode": "Please select a block mode.",
            "customBlockedSaved": "Custom blocked domains saved successfully.",
            "customBlockedSaveError": "Error saving blocked domains: {error}",
            "customResponseSaved": "Custom response rules saved successfully.",
            "customResponseSaveError": "Error saving response rules: {error}"
        },
        "custom": {
            "title": "Custom Rules Management",
            "description": "Manage your blocked domains and custom DNS reply rules.",
            "blockedDomains": "Blocked Domains",
            "blockedDomainsHelp": "One domain per line. These domains will be blocked by AdBlock.",
            "saveBlocked": "Save Block List",
            "customResponses": "Custom Responses",
            "customResponsesHelp": "Format: domain type value ttl. Rules here take precedence over block lists and upstream.",
            "customResponsesExample": "Example: example.com A 1.2.3.4 300",
            "saveResponse": "Save Response Rules"
        }
    },
    "zh-CN": {
        "app": {
            "title": "SmartDNSSort"
        },
        "tabs": {
            "dashboard": "仪表盘",
            "config": "配置",
            "custom": "自定义设置",
            "adblock": "广告拦截"
        },
        "actions": {
            "clearCache": "清除 DNS 缓存",
            "clearStats": "清除所有统计",
            "refresh": "刷新",
            "restart": "重启服务"
        },
        "status": {
            "connecting": "连接中...",
            "connected": "已连接",
            "disconnected": "已断开",
            "error": "错误：无法获取统计信息。"
        },
        "dashboard": {
            "generalStats": "常规统计",
            "totalQueries": "总查询数",
            "cacheHits": "缓存命中",
            "cacheMisses": "缓存未命中",
            "cacheHitRate": "缓存命中率",
            "upstreamFailures": "上游失败",
            "systemStatus": "系统状态",
            "cpuUsage": "CPU 使用率",
            "memoryUsage": "内存使用率",
            "goroutines": "协程数",
            "adblockStatus": "广告拦截状态",
            "updateRules": "立即更新所有规则",
            "enableAdblock": "启用广告拦截",
            "engine": "引擎",
            "totalRules": "总规则数",
            "blockedToday": "今日拦截",
            "blockedTotal": "总拦截",
            "lastUpdate": "最后更新",
            "memoryUsageTitle": "内存使用",
            "cacheEntries": "缓存条目",
            "expiredEntries": "过期条目",
            "protectedEntries": "受保护条目",
            "upstreamServers": "上游服务器",
            "server": "服务器",
            "success": "成功",
            "failure": "失败",
            "hotDomains": "热门域名 (Top 10)",
            "domain": "域名",
            "count": "次数",
            "recentQueries": "最近查询",
            "noDomainData": "暂无域名数据。",
            "noRecentQueries": "暂无最近查询。",
            "errorLoadingData": "加载数据出错。"
        },
        "config": {
            "nav": {
                "dns": "DNS 服务",
                "upstream": "上游",
                "ping": "Ping",
                "cache": "缓存",
                "prefetch": "预取",
                "webui": "Web 界面",
                "system": "系统"
            },
            "dns": {
                "legend": "DNS 服务",
                "listenPort": "DNS 监听端口",
                "enableTcp": "启用 TCP",
                "enableIpv6": "启用 IPv6"
            },
            "upstream": {
                "legend": "上游",
                "servers": "上游服务器",
                "serversHelp": "每行一个服务器。",
                "bootstrapDns": "引导 DNS",
                "bootstrapDnsHelp": "用于解析 DoH/DoT 域名的引导 DNS 服务器（必须是 IP 地址）。每行一个服务器。",
                "strategy": {
                    "_label": "策略",
                    "random": "随机",
                    "parallel": "并行",
                    "sequential": "顺序",
                    "racing": "竞争"
                },
                "timeout": "超时 (ms)",
                "concurrency": "并发数",
                "sequentialTimeout": "顺序超时 (ms)",
                "sequentialTimeoutHelp": "顺序策略的单次尝试超时时间 (100-2000ms)。",
                "racingDelay": "竞争延迟 (ms)",
                "racingDelayHelp": "竞争策略的初始延迟 (50-500ms)。",
                "racingMaxConcurrent": "竞争最大并发数",
                "racingMaxConcurrentHelp": "竞争策略的最大并发请求数 (2-5)。",
                "nxdomainForErrors": "上游错误返回 NXDOMAIN",
                "healthCheck": {
                    "legend": "健康检查与熔断器",
                    "enabled": "启用健康检查",
                    "failureThreshold": "失败阈值",
                    "failureThresholdHelp": "连续失败次数达到此值后，将上游服务器标记为降级。",
                    "circuitBreakerThreshold": "熔断器阈值",
                    "circuitBreakerThresholdHelp": "连续失败次数达到此值后，停止使用该上游服务器。",
                    "circuitBreakerTimeout": "熔断器超时 (秒)",
                    "circuitBreakerTimeoutHelp": "熔断器在此秒数后允许重试已损坏的电路。",
                    "successThreshold": "恢复成功阈值",
                    "successThresholdHelp": "连续成功次数达到此值后，从降级/损坏状态恢复。"
                }
            },
            "ping": {
                "legend": "Ping",
                "enabled": "启用 IP 优选",
                "enabledHelp": "是否对 DNS 解析结果中的 IP 进行 Ping 测试和排序。",
                "count": "次数",
                "timeout": "超时 (ms)",
                "concurrency": "并发数",
                "strategy": "策略",
                "maxTestIps": "最大测试 IP 数",
                "maxTestIpsHelp": "每次排序测试的最大 IP 数 (0 = 不限制)。",
                "rttCacheTtl": "RTT 缓存 TTL (s)",
                "rttCacheTtlHelp": "RTT 结果的缓存时间 (0 = 禁用)。",
                "enableHttpFallback": "启用 HTTP 回退",
                "enableHttpFallbackHelp": "当 ICMP 失败时回退到基于 HTTP 的 ping。"
            },
            "cache": {
                "legend": "缓存",
                "fastResponseTtl": "快速响应 TTL (s)",
                "userReturnTtl": "用户返回 TTL (s)",
                "minTtl": "最小 TTL (s)",
                "maxTtl": "最大 TTL (s)",
                "negativeTtl": "否定缓存 TTL (s)",
                "negativeTtlHelp": "NXDOMAIN/NODATA 响应的 TTL。",
                "errorCacheTtl": "错误缓存 TTL (s)",
                "errorCacheTtlHelp": "SERVFAIL/REFUSED 响应的 TTL。",
                "memoryLegend": "内存缓存管理",
                "maxMemory": "最大内存 (MB)",
                "maxMemoryHelp": "缓存的最大内存使用量。超过此限制将触发驱逐。0 表示不限制。",
                "evictionThreshold": "驱逐阈值",
                "evictionThresholdHelp": "触发驱逐的内存使用百分比 (0.7-0.95)。",
                "evictionBatchPercent": "驱逐批次百分比",
                "evictionBatchPercentHelp": "单次批次驱逐的缓存总量的百分比 (0.05-0.2)。",
                "keepExpired": "保留过期条目",
                "keepExpiredHelp": "如果空间允许，将过期条目保留在内存中以加速后续查询。",
                "protectPrefetch": "保护预取域名",
                "protectPrefetchHelp": "防止预取列表中的域名在 LRU 清理期间被驱逐。",
                "saveToDiskInterval": "落盘间隔 (分钟)",
                "saveToDiskIntervalHelp": "缓存持久化落盘的频率（0 表示禁用）。设置为 0 以禁用定期保存。"
            },
            "prefetch": {
                "legend": "预取",
                "enable": "启用预取",
                "help": "预取功能使用先进的数学模型,在热门域名缓存过期前自动刷新。建议仅在高流量环境下启用此功能。"
            },
            "webui": {
                "legend": "Web 界面",
                "enable": "启用 Web 界面",
                "listenPort": "Web 监听端口"
            },
            "system": {
                "legend": "系统",
                "maxCpuCores": "最大 CPU 核心数",
                "maxCpuCoresHelp": "设置为 0 以使用所有可用核心。",
                "sortQueueWorkers": "排序队列工作者",
                "sortQueueWorkersHelp": "并行排序任务的数量。",
                "refreshWorkers": "刷新工作者",
                "refreshWorkersHelp": "异步缓存刷新工作者的数量。"
            },
            "other": {
                "legend": "系统与高级设置"
            },
            "save": "保存并应用"
        },
        "adblock": {
            "ruleSources": "规则源",
            "url": "URL",
            "status": "状态",
            "rules": "规则数",
            "lastUpdate": "最后更新",
            "enabled": "启用",
            "actions": "操作",
            "addNew": "添加新源",
            "add": "添加",
            "placeholderUrl": "https://example.com/rules.txt",
            "blockMode": "拦截模式",
            "saveBlockMode": "保存",
            "blockModeHelp": "选择如何处理被拦截的域名",
            "testDomain": "测试域名",
            "test": "测试",
            "placeholderDomain": "example.com",
            "settings": "广告拦截设置",
            "updateInterval": "更新间隔 (小时)",
            "updateIntervalHelp": "自动更新规则列表的频率。0 表示禁用。",
            "maxCacheAge": "最大缓存时间 (小时)",
            "maxCacheAgeHelp": "强制重新下载之前缓存规则文件的最大时间。0 表示禁用。",
            "maxCacheSize": "最大缓存大小 (MB)",
            "maxCacheSizeHelp": "所有缓存规则文件的总大小限制。",
            "blockedTtl": "被拦截响应 TTL (秒)",
            "blockedTtlHelp": "被拦截域名查询的响应 TTL。",
            "saveSettings": "保存设置",
            "statusDisabled": "已禁用",
            "statusEnabled": "已启用",
            "statusError": "错误",
            "noSources": "未配置规则源。",
            "errorLoadingSources": "加载源出错。"
        },
        "footer": {
            "copyright": "© 2025 SmartDNSSort."
        },
        "messages": {
            "confirmClearCache": "确定要清除 DNS 缓存吗？",
            "cacheCleared": "DNS 缓存已成功清除。",
            "cacheClearFailed": "清除 DNS 缓存失败。",
            "cacheClearError": "清除 DNS 缓存时发生错误。",
            "confirmClearStats": "确定要清除所有统计信息吗？",
            "statsCleared": "所有统计信息已成功清除。",
            "statsClearFailed": "清除统计信息失败。",
            "statsClearError": "清除统计信息时发生错误。",
            "configSaved": "配置已成功保存并应用。",
            "configSaveError": "保存配置时出错：{error}",
            "configSaveErrorGeneric": "保存配置时发生错误。",
            "restartConfirm": "确定要重启服务吗？",
            "restarting": "正在重启服务...",
            "restartSuccess": "服务已成功重启。",
            "restartError": "重启服务时出错：{error}",
            "restartFailed": "重启服务失败。",
            "adblockRulesUpdated": "广告拦截规则已成功更新。",
            "adblockRulesUpdateError": "更新广告拦截规则时出错：{error}",
            "adblockSourceAdded": "广告拦截源已成功添加。",
            "adblockSourceAddError": "添加广告拦截源时出错：{error}",
            "adblockSourceDeleted": "广告拦截源已成功删除。",
            "adblockSourceDeleteError": "删除广告拦截源时出错：{error}",
            "adblockBlockModeSaved": "拦截模式已成功保存。",
            "adblockBlockModeSaveError": "保存拦截模式时出错：{error}",
            "adblockSettingsSaved": "广告拦截设置已成功保存。",
            "adblockSettingsSaveError": "保存广告拦截设置时出错：{error}",
            "adblockSettingsSaveErrorGeneric": "保存广告拦截设置时发生错误。",
            "adblockTestResult": "结果：{result}",
            "deleteConfirm": "确定要删除此源吗？",
            "adblockToggleError": "切换广告拦截状态时出错：{error}",
            "adblockUpdateConfirm": "确定要强制更新所有广告拦截规则吗？这可能需要一些时间。",
            "adblockUpdateStarted": "广告拦截规则更新已在后台开始。",
            "adblockUpdateFailed": "无法开始更新：{error}",
            "adblockUpdateError": "发生错误：{error}",
            "enterUrl": "请输入新规则源的 URL。",
            "enterDomain": "请输入要测试的域名。",
            "testing": "测试中...",
            "blocked": "已拦截！",
            "rule": "规则",
            "notBlocked": "未拦截。",
            "testError": "测试期间发生错误。",
            "selectBlockMode": "请选择拦截模式。",
            "customBlockedSaved": "自定义拦截域名已成功保存。",
            "customBlockedSaveError": "保存拦截域名时出错：{error}",
            "customResponseSaved": "自定义回复规则已成功保存。",
            "customResponseSaveError": "保存回复规则时出错：{error}"
        },
        "custom": {
            "title": "自定义规则管理",
            "description": "管理您的拦截域名和自定义 DNS 回复规则。",
            "blockedDomains": "拦截域名管理",
            "blockedDomainsHelp": "每行一个域名。这些域名将被广告拦截功能拦截。",
            "saveBlocked": "保存拦截列表",
            "customResponses": "自定义回复管理",
            "customResponsesHelp": "格式：域名 类型 值 TTL。此处的规则优先级高于拦截列表和上游查询。",
            "customResponsesExample": "示例：example.com A 1.2.3.4 300",
            "saveResponse": "保存回复规则"
        }
    }
};

const i18n = {
    locale: 'en',
    translations: {},
    availableLocales: ['en', 'zh-CN'],

    async init() {
        // 1. Determine language
        const savedLang = localStorage.getItem('smartdns_lang');
        const browserLang = navigator.language;

        if (savedLang && this.availableLocales.includes(savedLang)) {
            this.locale = savedLang;
        } else if (browserLang.startsWith('zh')) {
            this.locale = 'zh-CN';
        } else {
            this.locale = 'en';
        }

        // 2. Load translations
        await this.loadTranslations(this.locale);

        // 3. Apply to page
        this.applyTranslations();

        // 4. Update select if exists
        const langSelect = document.getElementById('langSwitch');
        if (langSelect) {
            langSelect.value = this.locale;
            langSelect.addEventListener('change', (e) => {
                this.setLanguage(e.target.value);
            });
        }

        // 5. Set html lang attribute
        document.documentElement.lang = this.locale;

        // 6. Dispatch ready event
        window.dispatchEvent(new CustomEvent('languageChanged', { detail: this.locale }));
    },

    async loadTranslations(lang) {
        if (resources[lang]) {
            this.translations = resources[lang];
        } else {
            console.warn(`Language ${lang} not found in resources, falling back to en`);
            this.translations = resources['en'];
            this.locale = 'en';
        }
    },

    async setLanguage(lang) {
        if (!this.availableLocales.includes(lang)) return;

        this.locale = lang;
        localStorage.setItem('smartdns_lang', lang);
        document.documentElement.lang = lang;

        await this.loadTranslations(lang);
        this.applyTranslations();

        // Dispatch event for other components
        window.dispatchEvent(new CustomEvent('languageChanged', { detail: lang }));
    },

    t(key, params = {}) {
        const keys = key.split('.');
        let value = this.translations;

        for (const k of keys) {
            if (value && value[k]) {
                value = value[k];
            } else {
                console.warn(`Missing translation: ${key}`);
                return key;
            }
        }

        // If value is an object, try to get the _label property
        if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
            if (value._label && typeof value._label === 'string') {
                value = value._label;
            } else {
                console.warn(`Missing translation or _label for: ${key}`);
                return key;
            }
        }

        if (typeof value !== 'string') {
            return key;
        }

        // Replace placeholders
        return value.replace(/{(\w+)}/g, (match, p1) => {
            return params[p1] !== undefined ? params[p1] : match;
        });
    },

    applyTranslations() {
        // 1. Text content
        document.querySelectorAll('[data-i18n]').forEach(el => {
            const key = el.getAttribute('data-i18n');
            el.textContent = this.t(key);
        });

        // 2. Placeholders
        document.querySelectorAll('[data-i18n-ph]').forEach(el => {
            const key = el.getAttribute('data-i18n-ph');
            el.placeholder = this.t(key);
        });

        // 3. Titles
        document.querySelectorAll('[data-i18n-title]').forEach(el => {
            const key = el.getAttribute('data-i18n-title');
            el.title = this.t(key);
        });
    }
};

// Expose to window
window.i18n = i18n;

// Auto init
document.addEventListener('DOMContentLoaded', () => {
    i18n.init();
});
