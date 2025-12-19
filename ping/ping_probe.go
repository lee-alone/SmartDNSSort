package ping

import (
	"context"
	"crypto/tls"
	"net"
	"time"
)

// smartPing 核心：智能混合探测（流量极小，准确率极高）
// 探测顺序：
// 1. TCP 443（HTTPS）
// 2. TLS 握手验证（带 SNI）
// 3. UDP DNS 查询（端口 53）
// 4. TCP 80（HTTP，可选）
func (p *Pinger) smartPing(ctx context.Context, ip, domain string) int {
	// 第1步：先测 443 TCP（几乎所有现代服务都支持）
	if rtt := p.tcpPingPort(ctx, ip, "443"); rtt >= 0 {
		// 第2步：关键！TLS ClientHello 带 SNI，能干掉 163.com 那种"TCP通但实际不可用"的节点
		if rtt2 := p.tlsHandshakeWithSNI(ip, domain); rtt2 >= 0 {
			// 用 TLS 握手时间更准（包含加密协商延迟）
			return rtt2
		}
		// TLS 失败直接判死刑（防止假阳性）
		return -1
	}

	// 第3步：443 完全不通的，尝试 53 UDP（公共 DNS 场景）
	if rtt := p.udpDnsPing(ip); rtt >= 0 {
		return rtt
	}

	// 第4步（可选）：用户打开开关才测 80
	if p.enableHttpFallback {
		if rtt := p.tcpPingPort(ctx, ip, "80"); rtt >= 0 {
			return rtt
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
	defer func() {
		// 重置 buffer 长度为 0，防止脏数据污染
		p.bufferPool.Put(buf[:0])
	}()

	start := time.Now()
	if _, err = pc.Write(query); err != nil {
		return -1
	}
	if _, err = pc.Read(buf); err != nil {
		return -1
	}
	return int(time.Since(start).Milliseconds())
}
