package ping

import (
	"context"
	"fmt"
	"net"
	"smartdnssort/logger"
	"strings"
	"sync"
	"sync/atomic"
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
// 修复：地址类型必须与连接协议严格对应
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

// tcpPing 使用 TCP 连接测试 IP 的可达性（ICMP 回退方案）
// 当 ICMP 被限速/丢弃时，通过 TCP 握手延迟来评估网络质量
//
// 参数：
//   - ip: 目标 IP 地址
//   - ports: 要探测的端口列表，并行探测，取最快响应
//
// 返回值：
//   - rtt: TCP 握手延迟（毫秒），-1 表示所有端口都失败
//   - port: 成功连接的端口号，0 表示失败
func (p *Pinger) tcpPing(ip string, ports []int) (int, int) {
	if len(ports) == 0 {
		return -1, 0
	}

	// 并发探测所有端口
	type tcpResult struct {
		rtt  int
		port int
	}
	resultCh := make(chan tcpResult, len(ports))
	var wg sync.WaitGroup

	// 快速返回标记：一旦有结果成功，后续结果直接忽略
	var firstSuccess atomic.Bool

	for _, port := range ports {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()

			// 如果已经有成功结果，跳过探测（节省资源）
			if firstSuccess.Load() {
				return
			}

			address := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
			start := time.Now()

			// 尝试建立 TCP 连接
			conn, err := net.DialTimeout("tcp", address, time.Duration(p.timeoutMs)*time.Millisecond)
			if err != nil {
				logger.Debugf("[Pinger] TCP connection to %s failed: %v", address, err)
				return
			}
			defer conn.Close()

			// 连接成功，计算 RTT
			rtt := int(time.Since(start).Milliseconds())
			logger.Debugf("[Pinger] TCP probe to %s successful, RTT: %dms", address, rtt)

			// 标记已有成功结果（后续 goroutine 会跳过）
			firstSuccess.Store(true)

			select {
			case resultCh <- tcpResult{rtt: rtt, port: port}:
			default:
				// channel 已满，忽略
			}
		}(port)
	}

	// 等待所有 goroutine 完成
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 收集结果，取最快的
	// 注意：由于有 firstSuccess 优化，通常只有一个结果
	var bestResult tcpResult
	bestResult.rtt = -1

	for result := range resultCh {
		if bestResult.rtt < 0 || result.rtt < bestResult.rtt {
			bestResult = result
		}
	}

	return bestResult.rtt, bestResult.port
}

// smartPingWithMethod 执行单次阶梯式探测（首选 ICMP，失败/劣化时触发 TCP 回退）
// 双重验证模式：
//   - 第一阶 (Primary)：进行 1 次 ICMP 探测
//   - 触发条件：如果 ICMP 超时 或 RTT > TCPThresholdMs
//   - 第二阶 (Fallback)：启动 TCP 探测（如果启用）
//
// 注意：此函数只执行单次探测，外层循环由 pingIP 负责（用于计算平均 RTT 和丢包率）
// 返回值说明：
//   - rtt: 最终 RTT（毫秒），-1 表示不可达（外层 pingIP 依靠 rtt >= 0 判断成功）
//   - method: 探测方法 (icmp, tcp:443, tcp:80 等)
//   - icmpErr: ICMP 错误信息（用于判断是否为权限/协议错误）
func (p *Pinger) smartPingWithMethod(_ context.Context, ip, _ string) (int, string, *ICMPError) {
	// 1. 执行 1 次 ICMP 探测
	rtt, icmpErr := p.icmpPingWithError(ip)

	// 2. 判断是否需要 TCP 补全（单次失败或延迟高）
	needTCPFallback := false
	if rtt < 0 || rtt >= p.tcpThresholdMs {
		// ICMP 超时或延迟过高，需要 TCP 补全
		needTCPFallback = true
	}

	if needTCPFallback && p.enableTCPFallback && len(p.tcpFallbackPorts) > 0 {
		// 执行 TCP 探测
		tcpRTT, tcpPort := p.tcpPing(ip, p.tcpFallbackPorts)

		// 如果 TCP 探测成功，返回归一化后的 RTT
		// 修复 #2：TCP RTT 归一化处理
		// TCP 握手通常比 ICMP 慢 2-5 倍（包含 TCP 协议开销、服务端处理延迟等）
		// 使用经验系数 2.5 进行归一化，使 TCP 回退的 IP 与 ICMP 探测的 IP 具有可比性
		// 这样 TCP 回退的 IP 不会被错误地排到后面
		if tcpRTT >= 0 {
			normalizedRTT := int(float64(tcpRTT) / 2.5)
			// 确保归一化后的 RTT 至少为 1ms，避免 0 值导致排序异常
			if normalizedRTT < 1 {
				normalizedRTT = 1
			}
			return normalizedRTT, fmt.Sprintf("tcp:%d", tcpPort), nil
		}
	}

	// 3. 返回 ICMP 结果（如果 TCP 补全也失败，rtt 保持 -1）
	// 注意：不要将 -1 转换为 LogicDeadRTT，否则外层 pingIP 会误判为成功
	return rtt, "icmp", icmpErr
}
