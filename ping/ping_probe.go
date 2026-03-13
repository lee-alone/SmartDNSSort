package ping

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// smartPing 核心：智能混合探测（流量极小，准确率极高）
// 新的探测顺序（基于用户洞察）：
// 1. ICMP ping（最直接，最能代表 IP 可达性）
// 2. TCP 443（HTTPS）+ TLS 握手验证（带 SNI）
// 3. UDP DNS 查询（端口 53，备选方案，增加 500ms 惩罚）
// 4. TCP 80（HTTP，可选）
//
// 第三阶段优化：SNI 感知与混合探测
// - 自动从 IPPool 提取代表性域名作为 SNI
// - 如果传入的 domain 为空，则使用 IPPool 中的代表性域名
func (p *Pinger) smartPing(ctx context.Context, ip, domain string) int {
	rtt, _ := p.smartPingWithMethod(ctx, ip, domain)
	return rtt
}

// getSNIDomain 获取用于 SNI 的域名
// 优先级：传入域名 > IPPool 代表性域名 > 空字符串
func (p *Pinger) getSNIDomain(ip, domain string) string {
	// 如果传入了域名，优先使用
	if domain != "" {
		return domain
	}

	// 从 IP 池获取代表性域名
	if p.ipPool != nil {
		if repDomain, exists := p.ipPool.GetRepDomain(ip); exists {
			return repDomain
		}
	}

	return ""
}

// tcpPingPort 通用 TCP 端口探测（443/80/853 都行）
func (p *Pinger) tcpPingPort(ctx context.Context, ip, port string) int {
	dialer := &net.Dialer{Timeout: time.Duration(p.timeoutMs) * time.Millisecond}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, port))
	if err != nil {
		return -1
	}
	conn.Close()
	return int(time.Since(start).Milliseconds())
}

// tlsHandshakeWithSNI 核心过滤器：TLS ClientHello 带 SNI（≈500 字节）
// 能够识别 TCP 连接成功但 TLS 握手失败的节点
//
// 第三阶段优化：直接信任并使用参数传入的 domain 作为 SNI
// 注意：调用方应该通过 getSNIDomain() 获取正确的 SNI 域名
func (p *Pinger) tlsHandshakeWithSNI(ip, domain string) int {
	// 直接使用传入的 domain 作为 SNI
	// 调用方（smartPingWithMethod）已经通过 getSNIDomain() 选择了最佳域名
	sniDomain := domain

	conf := &tls.Config{
		ServerName:         sniDomain,
		InsecureSkipVerify: true, // 只测速度
		MinVersion:         tls.VersionTLS12,
	}
	dialer := &net.Dialer{Timeout: time.Duration(p.timeoutMs) * time.Millisecond}

	start := time.Now()
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(ip, "443"), conf)
	if err != nil {
		return -1
	}
	conn.Close()
	return int(time.Since(start).Milliseconds())
}

// udpDnsPing 超轻量 UDP DNS 查询（80~200 字节）
func (p *Pinger) udpDnsPing(ip string) int {
	// 固定查询 www.google.com A 记录，30 字节
	query := []byte{
		0x00, 0x00, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x03, 'w', 'w', 'w',
		0x06, 'g', 'o', 'o', 'g', 'l', 'e', 0x03, 'c',
		'o', 'm', 0x00, 0x00, 0x01, 0x00, 0x01,
	}

	pc, err := net.DialTimeout("udp", net.JoinHostPort(ip, "53"), time.Duration(p.timeoutMs)*time.Millisecond)
	if err != nil {
		return -1
	}
	defer pc.Close()
	pc.SetDeadline(time.Now().Add(time.Duration(p.timeoutMs) * time.Millisecond))

	// 从池中获取 buffer
	buf := p.bufferPool.Get().([]byte)
	if cap(buf) < 512 {
		buf = make([]byte, 512)
	}
	buf = buf[:512] // 确保长度足够
	defer p.bufferPool.Put(buf)

	start := time.Now()
	if _, err = pc.Write(query); err != nil {
		return -1
	}
	if _, err = pc.Read(buf); err != nil {
		return -1
	}
	return int(time.Since(start).Milliseconds())
}

// icmpPing 使用 ICMP echo request/reply 测试 IP 可达性
// 这是最直接的 IP 可达性测试，不受端口和应用层限制
// 如果 ICMP 不通，说明 IP 根本不可达或被 ISP 拦截
func (p *Pinger) icmpPing(ip string) int {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return -1
	}
	defer conn.Close()

	destIP := net.ParseIP(ip)
	// 使用随机 ID 区分并发探测
	id := int(time.Now().UnixNano() & 0xffff)

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   id,
			Seq:  1,
			Data: []byte("ping"),
		},
	}

	b, err := msg.Marshal(nil)
	if err != nil {
		return -1
	}

	start := time.Now()
	if _, err = conn.WriteTo(b, &net.IPAddr{IP: destIP}); err != nil {
		return -1
	}

	// 持续读取直到捕获到正确的应答或超时
	reply := make([]byte, 1500)
	for {
		conn.SetReadDeadline(time.Now().Add(time.Duration(p.timeoutMs) * time.Millisecond))
		n, from, err := conn.ReadFrom(reply)
		if err != nil {
			return -1
		}

		// 1. 校验来源 IP
		if from.String() != ip {
			continue
		}

		// 2. 解析 ICMP 报文
		rm, err := icmp.ParseMessage(1, reply[:n])
		if err != nil {
			continue
		}

		// 3. 校验报文类型和 ID
		if rm.Type == ipv4.ICMPTypeEchoReply {
			if echo, ok := rm.Body.(*icmp.Echo); ok {
				if echo.ID == id {
					return int(time.Since(start).Milliseconds())
				}
			}
		}
	}
}

// smartPingWithMethod 智能混合探测，同时返回探测方法
// 用于标记每个 IP 使用的探测方法，便于调试和监控
//
// 第三阶段优化：SNI 感知
// - 使用 getSNIDomain 获取最佳 SNI 域名
// - 确保 TLS 握手使用正确的 ServerName
// - 探测成功后更新代表性域名
func (p *Pinger) smartPingWithMethod(ctx context.Context, ip, domain string) (int, string) {
	// 获取用于 SNI 的域名（第三阶段优化）
	sniDomain := p.getSNIDomain(ip, domain)

	// 第1步：ICMP ping (0ms 权重)
	if rtt := p.icmpPing(ip); rtt >= 0 {
		// ICMP 成功，不需要更新 SNI（ICMP 不使用 SNI）
		return rtt, "icmp"
	}

	// 第2步：TCP 443 + TLS (100ms 权重)
	// 使用 SNI 域名进行 TLS 握手验证
	if rtt := p.tcpPingPort(ctx, ip, "443"); rtt >= 0 {
		if rtt2 := p.tlsHandshakeWithSNI(ip, sniDomain); rtt2 >= 0 {
			// 第三阶段修复：TLS 探测成功，更新代表性域名
			// 如果传入的 domain 不为空，说明用户访问的域名探测成功
			// 应该更新该 IP 的代表性域名，以保证 SNI 的长效准确性
			if domain != "" && p.ipPool != nil {
				p.ipPool.UpdateRepDomainOnSuccess(ip, domain, false)
			}
			return rtt2, "tls" // 只返回原始 RTT，惩罚分由排序逻辑统一加
		}
		// TLS 握手失败，但 TCP 连接成功
		// 可能是 SNI 域名不正确，尝试更新代表性域名
		if domain != "" && p.ipPool != nil && sniDomain != domain {
			// 用户传入的域名与 SNI 域名不同，且 TLS 失败
			// 标记当前代表性域名可能有问题
			p.ipPool.CheckAndUpdateRepDomain(ip, sniDomain, domain)
		}
		return -1, ""
	}

	// 第3步：UDP 53 (500ms 权重)
	if rtt := p.udpDnsPing(ip); rtt >= 0 {
		return rtt, "udp53"
	}

	// 第4步：TCP 80 (300ms 权重)
	if p.enableHttpFallback {
		if rtt := p.tcpPingPort(ctx, ip, "80"); rtt >= 0 {
			return rtt, "tcp80"
		}
	}

	return -1, ""
}
