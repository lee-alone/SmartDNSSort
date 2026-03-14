package ping

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"smartdnssort/logger"
	"strings"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// ICMPError ICMP 探测错误类型
type ICMPError struct {
	IsPermissionError bool  // 是否为权限错误
	IsProtocolError   bool  // 是否为协议不支持错误
	Err               error // 原始错误
}

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
	rtt, _, _ := p.smartPingWithMethod(ctx, ip, domain)
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

// icmpPingWithError 使用 ICMP echo request/reply 测试 IP 可达性
// 返回 RTT 和详细的错误信息
//
// 第三阶段优化：区分"路不通"与"层不通"
//   - 如果返回的 ICMPError.IsPermissionError 或 IsProtocolError 为 true，
//     说明是权限或协议不支持问题，不应该触发 FastFail
//   - 只有真正的网络超时才应该触发 FastFail
func (p *Pinger) icmpPingWithError(ip string) (int, *ICMPError) {
	// 检查 ICMP 调度器是否就绪
	select {
	case <-p.icmpReady:
		// ICMP 调度器已就绪，继续
	default:
		// ICMP 调度器未就绪，返回错误
		logger.Debugf("[Pinger] ICMP dispatcher not ready, skipping ICMP ping for %s", ip)
		return -1, &ICMPError{IsProtocolError: true, Err: fmt.Errorf("ICMP dispatcher not ready")}
	}

	// 判断 IP 类型
	isIPv6 := p.isIPv6(ip)
	var conn *icmp.PacketConn
	if isIPv6 {
		conn = p.v6Conn
	} else {
		conn = p.v4Conn
	}

	// 检查对应的连接是否可用
	if conn == nil {
		logger.Debugf("[Pinger] ICMP connection not available for %s (IPv6: %v)", ip, isIPv6)
		return -1, &ICMPError{IsProtocolError: true, Err: fmt.Errorf("ICMP connection not available")}
	}

	// 1. 获取唯一 ID 并注册到全局 Map
	id := p.getNextID()
	ch := make(chan time.Time, 1)
	p.pendingProbes.Store(id, ch)
	defer p.pendingProbes.Delete(id)

	// 2. 根据地址类型发送正确的 ICMP
	var msg icmp.Message

	if isIPv6 {
		// IPv6 ICMP
		msg = icmp.Message{
			Type: ipv6.ICMPTypeEchoRequest,
			Code: 0,
			Body: &icmp.Echo{
				ID:   int(id),
				Seq:  1,
				Data: []byte("ping"),
			},
		}
	} else {
		// IPv4 ICMP
		msg = icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   int(id),
				Seq:  1,
				Data: []byte("ping"),
			},
		}
	}

	b, err := msg.Marshal(nil)
	if err != nil {
		logger.Debugf("[Pinger] Failed to marshal ICMP message for %s: %v", ip, err)
		return -1, &ICMPError{Err: err}
	}

	// 修复：使用 getDestAddr 动态构造目标地址，确保地址类型与连接协议匹配
	// 自动处理 IPv6 Link-Local 地址的 zone ID
	dest, err := p.getDestAddr(conn, ip, isIPv6)
	if err != nil {
		logger.Debugf("[Pinger] Failed to resolve destination address %s: %v", ip, err)
		return -1, &ICMPError{Err: err}
	}

	start := time.Now()
	if _, err = conn.WriteTo(b, dest); err != nil {
		logger.Debugf("[Pinger] Failed to send ICMP packet to %s: %v", ip, err)
		// 检查是否为权限错误
		if isPermissionError(err) {
			return -1, &ICMPError{IsPermissionError: true, Err: err}
		}
		return -1, &ICMPError{Err: err}
	}

	// 3. 等待全局接收协程的回调信号
	select {
	case recvTime := <-ch:
		return int(recvTime.Sub(start).Milliseconds()), nil
	case <-time.After(time.Duration(p.timeoutMs) * time.Millisecond):
		return -1, nil // 超时，不是权限或协议错误
	}
}

// getDestAddr 根据连接类型动态构造目标地址
// 修复：地址类型必须与 ListenPacket 的协议严格对应
// - UDP 模式：使用 net.UDPAddr
// - RAW 模式：使用 net.IPAddr
// - 自动处理 IPv6 Link-Local 地址的 zone ID
func (p *Pinger) getDestAddr(conn *icmp.PacketConn, ip string, isIPv6 bool) (net.Addr, error) {
	// 判断是否为 UDP 模式
	isUDP := false
	if isIPv6 {
		isUDP = p.v6IsUDP
	} else {
		isUDP = p.v4IsUDP
	}

	// 使用 net.ResolveIPAddr 自动处理 IPv6 Link-Local 地址的 zone ID
	network := "ip"
	if isIPv6 {
		network = "ip6"
	}
	ipAddr, err := net.ResolveIPAddr(network, ip)
	if err != nil {
		return nil, err
	}

	// 根据连接类型构造正确的地址类型
	if isUDP {
		return &net.UDPAddr{
			IP:   ipAddr.IP,
			Port: 0, // ICMP 不使用端口
			Zone: ipAddr.Zone,
		}, nil
	}
	return ipAddr, nil
}

// isPermissionError 检查错误是否为权限错误
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// 常见的权限错误字符串
	permissionErrors := []string{
		"permission denied",
		"operation not permitted",
		"access denied",
		"socket: permission denied",
	}
	for _, pe := range permissionErrors {
		if strings.Contains(strings.ToLower(errStr), pe) {
			return true
		}
	}
	return false
}

// icmpPing 使用 ICMP echo request/reply 测试 IP 可达性
// 这是最直接的 IP 可达性测试，不受端口和应用层限制
// 如果 ICMP 不通，说明 IP 根本不可达或被 ISP 拦截
//
// 第一阶段重构：使用全局 ICMP 调度器
// - 获取唯一 ID 并注册到全局 Map
// - 根据地址类型发送正确的 ICMP（IPv4 或 IPv6）
// - 等待全局接收协程的回调信号
func (p *Pinger) icmpPing(ip string) int {
	rtt, _ := p.icmpPingWithError(ip)
	return rtt
}

// smartPingWithMethod 智能混合探测，同时返回探测方法和ICMP错误
// 用于标记每个 IP 使用的探测方法，便于调试和监控
//
// 第三阶段优化：SNI 感知 + 精细化 FastFail
// - 使用 getSNIDomain 获取最佳 SNI 域名
// - 确保 TLS 握手使用正确的 ServerName
// - 探测成功后更新代表性域名
// - 区分"路不通"与"层不通"，避免因权限问题触发 FastFail
// - 修复：返回ICMP错误信息，避免重复探测
func (p *Pinger) smartPingWithMethod(ctx context.Context, ip, domain string) (int, string, *ICMPError) {
	// 获取用于 SNI 的域名（第三阶段优化）
	sniDomain := p.getSNIDomain(ip, domain)

	// 第1步：ICMP ping (0ms 权重)
	// 第三阶段优化：使用 icmpPingWithError 区分错误类型
	rtt, icmpErr := p.icmpPingWithError(ip)
	if rtt >= 0 {
		// ICMP 成功，不需要更新 SNI（ICMP 不使用 SNI）
		return rtt, "icmp", nil
	}

	// 如果 ICMP 失败是因为权限或协议不支持，不应该触发 FastFail
	// 继续尝试 TCP 探测
	if icmpErr != nil && (icmpErr.IsPermissionError || icmpErr.IsProtocolError) {
		logger.Debugf("[Pinger] ICMP failed for %s due to permission/protocol error, trying TCP: %v", ip, icmpErr.Err)
		// 继续执行 TCP 探测，不触发 FastFail
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
			return rtt2, "tls", nil // 只返回原始 RTT，惩罚分由排序逻辑统一加
		}
		// TLS 握手失败，但 TCP 连接成功
		// 可能是 SNI 域名不正确，尝试更新代表性域名
		if domain != "" && p.ipPool != nil && sniDomain != domain {
			// 用户传入的域名与 SNI 域名不同，且 TLS 失败
			// 标记当前代表性域名可能有问题
			p.ipPool.CheckAndUpdateRepDomain(ip, sniDomain, domain)
		}
		return -1, "", nil
	}

	// 第3步：UDP 53 (500ms 权重)
	if rtt := p.udpDnsPing(ip); rtt >= 0 {
		return rtt, "udp53", nil
	}

	// 第4步：TCP 80 (300ms 权重)
	if p.enableHttpFallback {
		if rtt := p.tcpPingPort(ctx, ip, "80"); rtt >= 0 {
			return rtt, "tcp80", nil
		}
	}

	return -1, "", nil
}
