package upstream

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"smartdnssort/stats"
	"time"

	"github.com/miekg/dns"
)

// QueryResult 查询结果
type QueryResult struct {
	IPs    []string
	CNAME  string // 添加 CNAME 字段
	TTL    uint32 // 上游 DNS 返回的 TTL（对所有 IP 取最小值）
	Error  error
	Server string // 添加服务器字段
}

// QueryResultWithTTL 带 TTL 信息的查询结果
type QueryResultWithTTL struct {
	IPs   []string
	CNAME string // 添加 CNAME 字段
	TTL   uint32 // 上游 DNS 返回的 TTL
}

// Upstream 上游 DNS 查询模块
type Upstream struct {
	servers   []string
	strategy  string // parallel, random
	timeoutMs int
	stats     *stats.Stats
}

// NewUpstream 创建上游 DNS 查询器
func NewUpstream(servers []string, strategy string, timeoutMs int, s *stats.Stats) *Upstream {
	if len(servers) == 0 {
		servers = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}
	// 默认为 random 策略，避免并发查询带来的高负载
	if strategy == "" || strategy == "parallel" {
		strategy = "random"
	}
	if timeoutMs <= 0 {
		timeoutMs = 300
	}

	return &Upstream{
		servers:   servers,
		strategy:  strategy,
		timeoutMs: timeoutMs,
		stats:     s,
	}
}

// Query 查询域名，返回 IP 列表和 TTL
func (u *Upstream) Query(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	// 目前仅支持 random 策略，简化逻辑并降低上游压力
	return u.queryRandom(ctx, domain, qtype)
}

// queryRandom 随机选择一个上游 DNS 服务器进行查询
func (u *Upstream) queryRandom(ctx context.Context, domain string, qtype uint16) (*QueryResultWithTTL, error) {
	if len(u.servers) == 0 {
		return nil, fmt.Errorf("no upstream servers configured")
	}

	// 随机选择一个服务器
	server := u.servers[rand.Intn(len(u.servers))]

	log.Printf("[queryRandom] 随机选择服务器 %s 查询 %s (type=%s)\n", server, domain, dns.TypeToString[qtype])

	result := u.querySingleServer(ctx, server, domain, qtype)
	if result.Error != nil {
		if u.stats != nil {
			u.stats.IncUpstreamFailure(server)
		}
		return nil, result.Error
	}

	if u.stats != nil {
		u.stats.IncUpstreamSuccess(server)
	}
	log.Printf("[queryRandom] 查询成功，返回 %d 个IP, CNAME=%s (TTL=%d秒): %v\n", len(result.IPs), result.CNAME, result.TTL, result.IPs)
	return &QueryResultWithTTL{IPs: result.IPs, CNAME: result.CNAME, TTL: result.TTL}, nil
}

// querySingleServer 查询单个上游 DNS 服务器
func (u *Upstream) querySingleServer(ctx context.Context, server, domain string, qtype uint16) *QueryResult {
	// 构造 DNS 请求
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), qtype)

	log.Printf("[querySingleServer] 向 %s 查询 %s (type=%s)\n", server, domain, dns.TypeToString[qtype])

	// 执行查询
	reply, _, err := u.doExchange(ctx, server, msg)
	if err != nil {
		log.Printf("[querySingleServer] 查询 %s 失败: %v\n", server, err)
		return &QueryResult{Error: err, Server: server}
	}

	if reply == nil || reply.Rcode != dns.RcodeSuccess {
		log.Printf("[querySingleServer] %s 返回错误代码: %d\n", server, reply.Rcode)
		return &QueryResult{Error: fmt.Errorf("dns query failed: rcode=%d", reply.Rcode), Server: server}
	}

	// 提取 IP 地址和 TTL
	ips, cname, ttl := extractIPs(reply)
	log.Printf("[querySingleServer] %s 返回 %d 个IP, CNAME=%s (TTL=%d秒): %v\n", server, len(ips), cname, ttl, ips)
	return &QueryResult{IPs: ips, CNAME: cname, TTL: ttl, Server: server}
}

// doExchange 执行底层的 DNS 交换
func (u *Upstream) doExchange(ctx context.Context, server string, m *dns.Msg) (*dns.Msg, time.Duration, error) {
	// 确保有端口
	if _, _, err := net.SplitHostPort(server); err != nil {
		server = net.JoinHostPort(server, "53")
	}

	client := &dns.Client{
		Net:     "udp",
		Timeout: time.Duration(u.timeoutMs) * time.Millisecond,
	}

	return client.ExchangeContext(ctx, m, server)
}

// extractIPs 从 DNS 响应中提取 IP 地址、CNAME 和最小 TTL
// 返回值：IP 列表、CNAME、最小 TTL（秒）
func extractIPs(msg *dns.Msg) ([]string, string, uint32) {
	var ips []string
	var cname string
	var minTTL uint32 = 0 // 0 表示未设置

	for _, answer := range msg.Answer {
		switch rr := answer.(type) {
		case *dns.A:
			ips = append(ips, rr.A.String())
			// 取最小 TTL
			if minTTL == 0 || rr.Hdr.Ttl < minTTL {
				minTTL = rr.Hdr.Ttl
			}
		case *dns.AAAA:
			ips = append(ips, rr.AAAA.String())
			// 取最小 TTL
			if minTTL == 0 || rr.Hdr.Ttl < minTTL {
				minTTL = rr.Hdr.Ttl
			}
		case *dns.CNAME:
			if cname == "" {
				cname = rr.Target
			}
			if minTTL == 0 || rr.Hdr.Ttl < minTTL {
				minTTL = rr.Hdr.Ttl
			}
		}
	}

	// 如果没有找到任何记录，使用默认 TTL（60 秒）
	if minTTL == 0 {
		minTTL = 60
	}

	return ips, cname, minTTL
}
