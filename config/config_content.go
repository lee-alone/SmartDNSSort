package config

// DefaultConfigContent 默认配置文件内容，包含详细说明
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

  # 查询策略：parallel（并行查询所有服务器），random（随机选择一个服务器），sequential（顺序查询），racing（竞争查询）
  strategy: "sequential"
  # 上游服务器响应超时时间（毫秒）
  timeout_ms: 5000
  # 并行查询时的并发数（仅在 strategy 为 parallel 时有效）
  concurrency: 3
  # sequential 策略的单次尝试超时时间（毫秒，默认 300）
  sequential_timeout: 300
  # racing 策略的赛跑起始延迟（毫秒，默认 100）
  racing_delay: 100
  # racing 策略中同时发起的最大竞争请求数（默认 2）
  racing_max_concurrent: 2

  # 是否将未处理的 SERVFAIL, timeout 转换为 NXDOMAIN 响应给客户端，默认 true
  # 这可以减少客户端的失败重试行为，但可能会隐藏上游服务器的真实错误
  nxdomain_for_errors: true
  
  # 是否启用 DNSSEC，默认 false
  # 启用后，会向上游请求 DNSSEC 记录 (RRSIG) 并将其返回给客户端
  dnssec: false

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
  # 是否启用 Ping 功能，默认 true
  enabled: true
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
  rtt_cache_ttl_seconds: 300

# DNS 缓存配置
cache:
  # 首次查询或未在缓存中时使用的 TTL（快速响应），默认值 5
  fast_response_ttl: 5
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
  # 缓存持久化落盘间隔（分钟），默认 60 分钟
  save_to_disk_interval_minutes: 60
  # DNSSEC 消息缓存容量 (MB)，用于存储完整的 DNS 响应消息（包含 RRSIG 等）
  # 独立于主缓存，默认为主缓存的 1/10（即 128MB 主缓存对应 12.8MB 消息缓存）
  msg_cache_size_mb: 12

# 预取配置（提前刷新缓存）
prefetch:
  # 是否启用预取功能
  enabled: false

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
