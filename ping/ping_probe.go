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
func (p *Pinger) smartPing(ctx context.Context, ip, domain string) int {
	// 第1步：ICMP ping（最直接，最能代表 IP 可达性）
	// 如果 ICMP 不通，说明 IP 根本不可达或被 ISP 拦截
	if rtt := p.icmpPing(ip); rtt >= 0 {
		return rtt
	}

	// 第2步：先测 443 TCP（几乎所有现代服务都支持）
	if rtt := p.tcpPingPort(ctx, ip, "443"); rtt >= 0 {
		// 第2.1步：关键！TLS ClientHello 带 SNI，能干掉 163.com 那种"TCP通但实际不可用"的节点
		if rtt2 := p.tlsHandshakeWithSNI(ip, domain); rtt2 >= 0 {
			// 用 TLS 握手时间更准（包含加密协商延迟）
			// TCP 成功增加 100ms 惩罚（相比 ICMP）
			return rtt2 + 100
		}
		// TLS 失败直接判死刑（防止假阳性）
		return -1
	}

	// 第3步：443 完全不通的，尝试 53 UDP（公共 DNS 场景）
	// 注意：UDP DNS 只能代表 DNS 服务可用，不代表 IP 真正可用
	// 对 UDP 结果增加 500ms 惩罚，降低其优先级
	if rtt := p.udpDnsPing(ip); rtt >= 0 {
		return rtt + 500
	}

	// 第4步（可选）：用户打开开关才测 80
	if p.enableHttpFallback {
		if rtt := p.tcpPingPort(ctx, ip, "80"); rtt >= 0 {
			// HTTP 增加 300ms 惩罚
			return rtt + 300
		}
	}

	return -1
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
func (p *Pinger) tlsHandshakeWithSNI(ip, domain string) int {
	conf := &tls.Config{
		ServerName:         domain,
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
func (p *Pinger) smartPingWithMethod(ctx context.Context, ip, domain string) (int, string) {
	// 第1步：ICMP ping（最直接，最能代表 IP 可达性）
	if rtt := p.icmpPing(ip); rtt >= 0 {
		return rtt, "icmp"
	}

	// 第2步：先测 443 TCP（几乎所有现代服务都支持）
	if rtt := p.tcpPingPort(ctx, ip, "443"); rtt >= 0 {
		// 第2.1步：关键！TLS ClientHello 带 SNI
		if rtt2 := p.tlsHandshakeWithSNI(ip, domain); rtt2 >= 0 {
			// TCP 成功增加 100ms 惩罚（相比 ICMP）
			return rtt2 + 100, "tls"
		}
		// TLS 失败直接判死刑
		return -1, ""
	}

	// 第3步：443 完全不通的，尝试 53 UDP（公共 DNS 场景）
	if rtt := p.udpDnsPing(ip); rtt >= 0 {
		// UDP 增加 500ms 惩罚
		return rtt + 500, "udp53"
	}

	// 第4步（可选）：用户打开开关才测 80
	if p.enableHttpFallback {
		if rtt := p.tcpPingPort(ctx, ip, "80"); rtt >= 0 {
			// HTTP 增加 300ms 惩罚
			return rtt + 300, "tcp80"
		}
	}

	return -1, ""
}
