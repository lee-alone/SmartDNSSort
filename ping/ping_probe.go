package ping

import (
	"context"
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

// smartPing 纯 ICMP 探测
// 简化逻辑：只使用 ICMP echo request/reply 测试 IP 可达性
// domain 参数保留用于未来可能的扩展，但当前不使用
func (p *Pinger) smartPing(ctx context.Context, ip, domain string) int {
	rtt, _, _ := p.smartPingWithMethod(ctx, ip, domain)
	return rtt
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
				Seq:  int(id), // 修复：在 Linux UDP 模式下 ID 会被内核修改，需使用 Seq 追踪
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
				Seq:  int(id), // 修复：在 Linux UDP 模式下 ID 会被内核修改，需使用 Seq 追踪
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
func (p *Pinger) getDestAddr(_ *icmp.PacketConn, ip string, isIPv6 bool) (net.Addr, error) {
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

// smartPingWithMethod 纯 ICMP 探测
// 简化逻辑：只使用 ICMP echo request/reply 测试 IP 可达性
// 一旦 ICMP 返回 -1（超时），直接将该 IP 判定为不可达
// 不再尝试 TCP/TLS/UDP 探测
//
// 纯 ICMP 模式优势：
// - 逻辑极简且透明
// - 不受应用层（TLS、DNS）影响
// - 避免 CDN 证书校验导致的"特定域名成块 DEAD"问题
// - 在 Debian 环境下配合 setcap cap_net_raw+ep 权限，识别率 100%
//
// 软容错改造：3次探测均值逻辑
// - 运行 p.count 次循环（默认 3 次）
// - 如果成功，累加真实 RTT；如果失败，累加 LogicDeadRTT
// - 取平均值作为最终 RTT
// - 只要有任意一次成功，就标记 icmpErr 为 nil，用于后续触发"权重平反"
func (p *Pinger) smartPingWithMethod(_ context.Context, ip, _ string) (int, string, *ICMPError) {
	// 确定探测次数，默认为 3 次
	probeCount := p.count
	if probeCount <= 0 {
		probeCount = 3
	}

	totalRTT := 0
	var icmpErr *ICMPError
	hasSuccess := false

	// 执行多次探测
	for i := 0; i < probeCount; i++ {
		rtt, err := p.icmpPingWithError(ip)
		if rtt >= 0 {
			// 探测成功，累加真实 RTT
			totalRTT += rtt
			hasSuccess = true
			// 只要有任意一次成功，就清除错误标记
			icmpErr = nil
		} else {
			// 探测失败，累加惩罚值
			totalRTT += LogicDeadRTT
			// 保留第一次的错误信息（如果有）
			if icmpErr == nil && err != nil {
				icmpErr = err
			}
		}
	}

	// 计算平均值
	avgRTT := totalRTT / probeCount

	// 如果至少有一次成功，清除错误标记，用于触发"权重平反"
	if hasSuccess {
		icmpErr = nil
	}

	return avgRTT, "icmp", icmpErr
}
