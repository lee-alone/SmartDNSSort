package resolver

import (
	"context"
	"fmt"
	"net"
	"smartdnssort/config"
	"smartdnssort/logger"
	"time"

	"github.com/miekg/dns"
)

// Resolver 递归DNS解析器
type Resolver struct {
	config    *config.RecursiveConfig
	cache     *Cache
	stats     *Stats
	rootHints []string
}

// NewResolver 创建新的递归解析器
func NewResolver(cfg *config.RecursiveConfig, rootHints []string) (*Resolver, error) {
	if cfg == nil {
		logger.Error("resolver config is nil")
		return nil, fmt.Errorf("config is nil")
	}

	// 创建缓存
	// 注意：这里的缓存参数目前写死或使用默认值，后续可从配置中读取
	cache := NewCache(10000, true)
	logger.Debugf("resolver cache initialized")

	// 创建统计模块
	stats := NewStats()
	logger.Debug("resolver stats module initialized")

	if len(rootHints) == 0 {
		logger.Warn("starting resolver with empty root hints, will use built-in defaults")
	}

	logger.Infof("resolver initialized successfully")
	return &Resolver{
		config:    cfg,
		cache:     cache,
		stats:     stats,
		rootHints: rootHints,
	}, nil
}

// Resolve 执行DNS查询
func (r *Resolver) Resolve(ctx context.Context, domain string, qtype uint16) ([]dns.RR, error) {
	if domain == "" {
		logger.Warn("resolve called with empty domain")
		return nil, fmt.Errorf("domain is empty")
	}

	// 确保域名以 . 结尾
	if domain[len(domain)-1] != '.' {
		domain = domain + "."
	}

	logger.Debugf("resolving domain: %s (type=%d)", domain, qtype)

	// 生成缓存键
	key := CacheKey(domain, qtype)

	// 检查缓存
	if rrs, found := r.cache.Get(key); found {
		r.stats.RecordCacheHit()
		logger.Debugf("cache hit for domain: %s", domain)
		return rrs, nil
	}

	r.stats.RecordCacheMiss()
	logger.Debugf("cache miss for domain: %s", domain)

	// 执行查询
	startTime := time.Now()
	rrs, err := r.resolveRecursive(ctx, domain, qtype, 0)
	latency := time.Since(startTime)

	// 记录统计信息
	r.stats.RecordQuery(latency, err == nil)

	if err != nil {
		logger.Warnf("resolve failed for domain %s: %v", domain, err)
		return nil, err
	}

	logger.Debugf("resolve succeeded for domain %s in %v", domain, latency)

	// 缓存结果
	if len(rrs) > 0 {
		// 计算 TTL（使用最小的 TTL）
		minTTL := uint32(3600) // 默认 1 小时
		for _, rr := range rrs {
			if rr.Header().Ttl < minTTL {
				minTTL = rr.Header().Ttl
			}
		}
		r.cache.Set(key, rrs, time.Duration(minTTL)*time.Second)
		logger.Debugf("cached %d records for domain %s with TTL %d", len(rrs), domain, minTTL)
	}

	return rrs, nil
}

// resolveRecursive 递归解析
func (r *Resolver) resolveRecursive(ctx context.Context, domain string, qtype uint16, depth int) ([]dns.RR, error) {
	// 检查递归深度 (默认 15)
	maxDepth := 15
	if depth > maxDepth {
		return nil, fmt.Errorf("max recursion depth exceeded (%d) for %s", depth, domain)
	}

	// 检查上下文是否已取消
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	logger.Debugf("[Resolver] Recursive resolve depth=%d domain=%s type=%s", depth, domain, dns.TypeToString[qtype])

	// 1. 从根服务器开始
	nameservers := r.getRootNameservers()
	if len(nameservers) == 0 {
		return nil, fmt.Errorf("no root nameservers available")
	}

	// 2. 迭代查询
	currentNameservers := nameservers
	currentDomain := domain

	for iteration := 1; iteration <= 15; iteration++ { // 限制单次递归内部迭代次数
		if len(currentNameservers) == 0 {
			return nil, fmt.Errorf("no nameservers available for %s", currentDomain)
		}

		// 查询当前域名
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(currentDomain), qtype)
		msg.RecursionDesired = false

		// 向当前的 nameserver 查询 (尝试列表中的每一个，直到成功或获得确定性结果)
		var reply *dns.Msg
		var err error
		var lastNS string
		var definitiveResult bool

		for _, ns := range currentNameservers {
			lastNS = ns
			reply, err = r.queryNameserver(ctx, ns, msg)
			if err != nil || reply == nil {
				logger.Debugf("[Resolver] Query nameserver %s failed: %v", ns, err)
				continue
			}

			// 检查是否为确定性结果
			if reply.Rcode == dns.RcodeSuccess || reply.Rcode == dns.RcodeNameError {
				definitiveResult = true
				break
			}

			logger.Debugf("[Resolver] Nameserver %s returned non-definitive Rcode: %s", ns, dns.RcodeToString[reply.Rcode])
			// 继续尝试其他 nameserver
		}

		if !definitiveResult {
			if err != nil {
				return nil, fmt.Errorf("all nameservers failed for %s: %w", currentDomain, err)
			}
			if reply != nil {
				return nil, fmt.Errorf("all nameservers returned errors for %s, last error: %s", currentDomain, dns.RcodeToString[reply.Rcode])
			}
			return nil, fmt.Errorf("no response from any nameservers for %s", currentDomain)
		}

		logger.Debugf("[Resolver] Iteration %d: domain=%s ns=%s rcode=%s answers=%d authority=%d extra=%d",
			iteration, currentDomain, lastNS, dns.RcodeToString[reply.Rcode], len(reply.Answer), len(reply.Ns), len(reply.Extra))

		// 3. 处理 RCode
		if reply.Rcode == dns.RcodeNameError {
			logger.Debugf("[Resolver] NXDOMAIN for %s at %s", currentDomain, lastNS)
			return []dns.RR{}, nil // 或者返回特定错误
		}

		if reply.Rcode != dns.RcodeSuccess {
			return nil, fmt.Errorf("dns error: %s", dns.RcodeToString[reply.Rcode])
		}

		// 4. 处理响应内容
		if len(reply.Answer) > 0 {
			// 检查是否有我们需要的记录类型
			var finalAnswers []dns.RR
			var hasCNAME bool
			var cnameTarget string

			for _, rr := range reply.Answer {
				if rr.Header().Rrtype == qtype {
					finalAnswers = append(finalAnswers, rr)
				} else if rr.Header().Rrtype == dns.TypeCNAME {
					hasCNAME = true
					if cname, ok := rr.(*dns.CNAME); ok {
						cnameTarget = cname.Target
					}
				}
			}

			if len(finalAnswers) > 0 {
				logger.Debugf("[Resolver] Found %d answers for %s", len(finalAnswers), currentDomain)
				return reply.Answer, nil
			}

			if hasCNAME && cnameTarget != "" {
				logger.Debugf("[Resolver] Following CNAME: %s -> %s", currentDomain, cnameTarget)
				// 重新从根开始解析 CNAME 目标，或者在这里继续迭代？
				// 为了简单和稳健，递归调用 resolveRecursive
				return r.Resolve(ctx, cnameTarget, qtype)
			}
		}

		// 检查 Authority section 中的 NS 记录 (Referral)
		if len(reply.Ns) > 0 {
			var nextNSNames []string
			var isNODATA bool

			for _, rr := range reply.Ns {
				if ns, ok := rr.(*dns.NS); ok {
					nextNSNames = append(nextNSNames, ns.Ns)
				} else if _, ok := rr.(*dns.SOA); ok {
					// 如果 Authority 区有 SOA，通常表示 NODATA (或者 NXDOMAIN，但上面已经检查过 Rcode)
					isNODATA = true
				}
			}

			if isNODATA && len(nextNSNames) == 0 {
				logger.Debugf("[Resolver] NODATA for %s", currentDomain)
				return []dns.RR{}, nil
			}

			if len(nextNSNames) > 0 {
				// 解析 NS 记录的 IP 地址
				// 尝试在 Additional section 中查找 Glue records (A 记录和 AAAA 记录)
				var ips []string
				for _, rr := range reply.Extra {
					header := rr.Header()
					// 检查是否是 NS 列表中某个域名的记录
					isMatch := false
					for _, nsName := range nextNSNames {
						if dns.Fqdn(nsName) == dns.Fqdn(header.Name) {
							isMatch = true
							break
						}
					}

					if isMatch {
						if a, ok := rr.(*dns.A); ok {
							ips = append(ips, a.A.String())
						} else if aaaa, ok := rr.(*dns.AAAA); ok {
							ips = append(ips, aaaa.AAAA.String())
						}
					}
				}

				if len(ips) > 0 {
					logger.Debugf("[Resolver] Using glue records for %s: %v", currentDomain, ips)
					currentNameservers = ips
					continue
				}

				// 如果没有 Glue records，且我们不是在查询 NS 自己，则需要解析 NS 域名
				logger.Debugf("[Resolver] No glue for NS %v, resolving...", nextNSNames)
				ips, err = r.resolveNameserverIPs(ctx, nextNSNames, depth+1)
				if err != nil {
					logger.Debugf("[Resolver] Failed to resolve NS IPs: %v", err)
					return nil, err
				}

				if len(ips) > 0 {
					currentNameservers = ips
					continue
				}
			}
		}

		// 如果到这里还没有跳出，说明无法继续
		return nil, fmt.Errorf("no answer and no further nameservers for %s", currentDomain)
	}

	return nil, fmt.Errorf("max iterations reached for %s", domain)
}

// getRootNameservers 获取根 nameservers
func (r *Resolver) getRootNameservers() []string {
	if len(r.rootHints) > 0 {
		return r.rootHints
	}

	// 2. 返回根 nameserver 的 IP 地址 (默认兜底)
	return []string{
		"198.41.0.4",     // a.root-servers.net
		"199.9.14.201",   // b.root-servers.net
		"192.33.4.12",    // c.root-servers.net
		"199.7.91.13",    // d.root-servers.net
		"192.203.230.10", // e.root-servers.net
		"192.5.5.241",    // f.root-servers.net
		"192.112.36.4",   // g.root-servers.net
		"198.97.190.53",  // h.root-servers.net
		"192.36.148.17",  // i.root-servers.net
		"192.58.128.30",  // j.root-servers.net
		"193.0.14.129",   // k.root-servers.net
		"199.7.83.42",    // l.root-servers.net
		"202.12.27.33",   // m.root-servers.net
	}
}

// queryNameserver 向指定的 nameserver 发送查询
func (r *Resolver) queryNameserver(ctx context.Context, nameserver string, msg *dns.Msg) (*dns.Msg, error) {
	client := &dns.Client{
		Net:     "udp",
		Timeout: 2 * time.Second,
	}

	target := nameserver
	if !hasPort(target) {
		target = target + ":53"
	}

	reply, _, err := client.ExchangeContext(ctx, msg, target)
	if err != nil {
		// 尝试使用 TCP
		client.Net = "tcp"
		reply, _, err = client.ExchangeContext(ctx, msg, target)
	}

	return reply, err
}

// resolveNameserverIPs 解析 nameserver 域名的 IP
func (r *Resolver) resolveNameserverIPs(ctx context.Context, nameservers []string, depth int) ([]string, error) {
	var ips []string

	for _, ns := range nameservers {
		// 如果已经是 IP 地址，直接使用
		if net.ParseIP(ns) != nil {
			ips = append(ips, ns)
			continue
		}

		// 否则递归解析 (同时尝试 A 和 AAAA 记录)
		// 解析 A 记录
		rrsA, err := r.resolveRecursive(ctx, ns, dns.TypeA, depth)
		if err == nil {
			for _, rr := range rrsA {
				if a, ok := rr.(*dns.A); ok {
					ips = append(ips, a.A.String())
				}
			}
		}

		// 解析 AAAA 记录
		rrsAAAA, err := r.resolveRecursive(ctx, ns, dns.TypeAAAA, depth)
		if err == nil {
			for _, rr := range rrsAAAA {
				if aaaa, ok := rr.(*dns.AAAA); ok {
					ips = append(ips, aaaa.AAAA.String())
				}
			}
		}
	}

	return ips, nil
}

// hasPort 检查地址是否包含端口号
func hasPort(s string) bool {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return true
		}
		if s[i] == ']' { // IPv6 address without port
			return false
		}
	}
	return false
}

// GetStats 获取统计信息
func (r *Resolver) GetStats() map[string]interface{} {
	stats := r.stats.GetStats()
	stats["cache"] = r.cache.GetStats()
	return stats
}

// Close 关闭解析器
func (r *Resolver) Close() error {
	// 清理缓存
	r.cache.Clear()
	return nil
}

// ClearCache 清空缓存
func (r *Resolver) ClearCache() {
	r.cache.Clear()
}

// CleanupExpiredCache 清理过期缓存
func (r *Resolver) CleanupExpiredCache() {
	r.cache.CleanupExpired()
}

// ResetStats 重置统计信息
func (r *Resolver) ResetStats() {
	r.stats.Reset()
}
