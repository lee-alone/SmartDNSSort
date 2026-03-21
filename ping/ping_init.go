package ping

import (
	"fmt"
	"net"
	"smartdnssort/logger"
	"sync/atomic"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sync/singleflight"
)

// NewPinger 创建新的 Pinger 实例（纯 ICMP 探测模式）
// 参数：
//   - count: 每个 IP 的测试次数（建议 3-5 次，取平均值）
//   - timeoutMs: 单次测试超时时间（毫秒）
//   - concurrency: 并发测试的 IP 数量（建议 5-10，避免触发 ICMP Flood 保护）
//   - maxTestIPs: 最多测试的 IP 数量（0 表示测试所有）
//   - rttCacheTtlSeconds: RTT 缓存过期时间（秒）
//   - enableHttpFallback: 已弃用，保留用于向后兼容
//   - failureWeightPersistFile: IP失效权重持久化文件路径（空字符串表示不持久化）
//
// Debian 部署建议：
//   - 使用 setcap cap_net_raw+ep SmartDNSSort 赋予二进制文件原始套接字权限
//   - RAW 模式下，ID 和 Seq 字段由程序完全控制，识别率 100%
func NewPinger(count, timeoutMs, concurrency, maxTestIPs, rttCacheTtlSeconds int, enableHttpFallback bool, failureWeightPersistFile string) *Pinger {
	if count <= 0 {
		count = 3
	}
	if timeoutMs <= 0 {
		timeoutMs = 800
	}
	if concurrency <= 0 {
		concurrency = 8
	}

	p := &Pinger{
		count:              count,
		timeoutMs:          timeoutMs,
		concurrency:        concurrency,
		maxTestIPs:         maxTestIPs,
		rttCacheTtlSeconds: rttCacheTtlSeconds,
		rttCache:           newShardedRttCache(32), // 使用 32 个分片
		stopChan:           make(chan struct{}),
		failureWeightMgr:   NewIPFailureWeightManager(failureWeightPersistFile),
		probeFlight:        &singleflight.Group{},
		ipPool:             NewIPPool(), // 初始化全局 IP 资源管理器（用于 IP 监控器）
		staleRevalidating:  make(map[string]bool),
		staleGracePeriod:   30 * time.Second, // 默认 30 秒软过期容忍期
		icmpReady:          make(chan struct{}),
		// TCP 回退探测默认配置
		enableTCPFallback: true,           // 默认启用 TCP 回退
		tcpFallbackPorts:  []int{443, 80}, // 默认探测 443 (HTTPS) 和 80 (HTTP)
		tcpThresholdMs:    1000,           // 默认 ICMP 延迟超过 1000ms 时触发 TCP 回退
		// 修复 #7：TTL 阈值默认配置
		rttThresholdExcellent: 50,  // 默认极优 IP 阈值 50ms
		rttThresholdGood:      100, // 默认优质 IP 阈值 100ms
		// 第四阶段：EWMA 平滑系数配置
		deadThresholdMs: LogicDeadRTT, // 默认逻辑失效阈值 9000ms
		alphaOnline:     0.3,          // 默认在线时 EWMA 系数
		alphaOffline:    0.1,          // 默认断网时 EWMA 系数
	}

	// 初始化全局 ICMP 调度器
	if err := p.initICMPDispatcher(); err != nil {
		logger.Warnf("[Pinger] Failed to initialize ICMP dispatcher: %v. ICMP ping will be disabled.", err)
	}

	if rttCacheTtlSeconds > 0 {
		go p.startRttCacheCleaner()
	}
	return p
}

// initICMPDispatcher 初始化全局 ICMP 调度器
// 创建 IPv4 和 IPv6 单例监听器，并启动常驻接收协程
func (p *Pinger) initICMPDispatcher() error {
	// 尝试初始化 IPv4 监听器（优先使用 UDP 模式，无需 Root）
	// Linux 下优先使用 udp4 协议族，这需要内核参数 net.ipv4.ping_group_range 支持
	// 现在的主流发行版默认都支持
	v4Conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		// 如果 UDP 模式失败，尝试使用传统的 ip4:icmp 模式（需要 Root）
		logger.Debugf("[Pinger] UDP4 ICMP listener failed, trying ip4:icmp: %v", err)
		v4Conn, err = icmp.ListenPacket("ip4:icmp", "0.0.0.0")
		if err != nil {
			logger.Warnf("[Pinger] IPv4 ICMP listener initialization failed: %v", err)
		} else {
			p.v4Conn = v4Conn
			logger.Info("[Pinger] IPv4 ICMP listener initialized (ip4:icmp mode)")
		}
	} else {
		p.v4Conn = v4Conn
		p.v4IsUDP = true
		logger.Info("[Pinger] IPv4 ICMP listener initialized (udp4 mode)")
	}

	// 尝试初始化 IPv6 监听器
	v6Conn, err := icmp.ListenPacket("udp6", "::")
	if err != nil {
		// 如果 UDP 模式失败，尝试使用传统的 ip6:ipv6-icmp 模式（需要 Root）
		logger.Debugf("[Pinger] UDP6 ICMP listener failed, trying ip6:ipv6-icmp: %v", err)
		v6Conn, err = icmp.ListenPacket("ip6:ipv6-icmp", "::")
		if err != nil {
			logger.Warnf("[Pinger] IPv6 ICMP listener initialization failed: %v", err)
		} else {
			p.v6Conn = v6Conn
			p.v6IsUDP = false
			logger.Info("[Pinger] IPv6 ICMP listener initialized (ip6:ipv6-icmp mode)")
		}
	} else {
		p.v6Conn = v6Conn
		p.v6IsUDP = true
		logger.Info("[Pinger] IPv6 ICMP listener initialized (udp6 mode)")
	}

	// 如果至少有一个监听器初始化成功，启动接收协程
	if p.v4Conn != nil || p.v6Conn != nil {
		go p.startICMPReceiver()
		close(p.icmpReady) // 通知 ICMP 调度器已就绪
		return nil
	}

	return fmt.Errorf("failed to initialize any ICMP listener")
}

// startICMPReceiver 启动常驻接收协程
// 专门循环 ReadFrom，每读到一个包，解析其回传的 ID，并将时间戳扔进对应的 chan 中
func (p *Pinger) startICMPReceiver() {
	logger.Info("[Pinger] Starting ICMP receiver goroutines")

	// IPv4 接收协程
	if p.v4Conn != nil {
		go func() {
			defer p.v4Conn.Close()
			buf := make([]byte, 1500)
			for {
				select {
				case <-p.stopChan:
					logger.Info("[Pinger] IPv4 ICMP receiver stopped")
					return
				default:
					p.v4Conn.SetReadDeadline(time.Now().Add(1 * time.Second))
					n, _, err := p.v4Conn.ReadFrom(buf)
					if err != nil {
						// 超时是正常的，继续循环
						if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
							continue
						}
						logger.Debugf("[Pinger] IPv4 ICMP read error: %v", err)
						continue
					}

					// 解析 ICMP 报文
					// 软容错改造：智能判断报文格式，解决 Linux/Windows 兼容性问题
					// 不要硬跳字节。检查 buf[0]。
					// 如果 buf[0] == 0x45 (IPv4) 且长度 > 20，跳过 20 字节。
					// 否则，直接解析。
					icmpData := buf[:n]
					if n > 0 {
						// 检查第一个字节判断是否为 IPv4 报文
						// 0x45 = IPv4, IHL=5 (20字节首部)
						if buf[0] == 0x45 && n > 20 {
							// IPv4 报文，跳过 20 字节首部
							icmpData = buf[20:n]
						}
						// 否则直接解析（UDP 模式或其他情况）
					}
					rm, err := icmp.ParseMessage(1, icmpData)
					if err != nil {
						continue
					}

					// 只处理 Echo Reply
					if rm.Type == ipv4.ICMPTypeEchoReply {
						if echo, ok := rm.Body.(*icmp.Echo); ok {
							// 查找对应的回调 channel
							// 修复：在 Linux UDP 模式下，ID 被内核占用，使用 Seq 进行匹配
							trackingID := echo.ID
							if p.v4IsUDP {
								trackingID = echo.Seq
							}
							if v, exists := p.pendingProbes.Load(uint16(trackingID)); exists {
								if ch, ok := v.(chan time.Time); ok {
									select {
									case ch <- time.Now():
										// 成功发送时间戳
									default:
										// channel 已满或已关闭，忽略
									}
								}
							}
						}
					}
				}
			}
		}()
	}

	// IPv6 接收协程
	if p.v6Conn != nil {
		go func() {
			defer p.v6Conn.Close()
			buf := make([]byte, 1500)
			for {
				select {
				case <-p.stopChan:
					logger.Info("[Pinger] IPv6 ICMP receiver stopped")
					return
				default:
					p.v6Conn.SetReadDeadline(time.Now().Add(1 * time.Second))
					n, _, err := p.v6Conn.ReadFrom(buf)
					if err != nil {
						// 超时是正常的，继续循环
						if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
							continue
						}
						logger.Debugf("[Pinger] IPv6 ICMP read error: %v", err)
						continue
					}

					// 解析 ICMPv6 报文
					// 软容错改造：智能判断报文格式，解决 Linux/Windows 兼容性问题
					// 不要硬跳字节。检查 buf[0]。
					// 如果是 IPv6 报文且长度 > 40，跳过 40 字节。
					// 否则，直接解析。
					icmpData := buf[:n]
					if n > 0 {
						// 修复 #5：使用更宽松的 IPv6 版本号检查
						// 原先只检查 buf[0] == 0x60，但某些系统的 Traffic Class 字段可能不为 0
						// 使用 (buf[0] >> 4) == 0x06 检查版本号，兼容 Traffic Class 不为 0 的情况
						// IPv6 版本号在高 4 位，值为 6 (0110)
						if (buf[0]>>4) == 0x06 && n > 40 {
							// IPv6 报文，跳过 40 字节首部
							icmpData = buf[40:n]
						}
						// 否则直接解析（UDP 模式或其他情况）
					}
					rm, err := icmp.ParseMessage(58, icmpData)
					if err != nil {
						continue
					}

					// 只处理 Echo Reply
					if rm.Type == ipv6.ICMPTypeEchoReply {
						if echo, ok := rm.Body.(*icmp.Echo); ok {
							// 查找对应的回调 channel
							// 修复：在 Linux UDP 模式下，ID 被内核占用，使用 Seq 进行匹配
							trackingID := echo.ID
							if p.v6IsUDP {
								trackingID = echo.Seq
							}
							if v, exists := p.pendingProbes.Load(uint16(trackingID)); exists {
								if ch, ok := v.(chan time.Time); ok {
									select {
									case ch <- time.Now():
										// 成功发送时间戳
									default:
										// channel 已满或已关闭，忽略
									}
								}
							}
						}
					}
				}
			}
		}()
	}
}

// getNextID 获取下一个唯一的 ICMP ID
// 使用原子操作确保并发安全，ID 在 1-65535 范围内循环
// 修复：确保ID永远在1-65535之间，避免ID为0
func (p *Pinger) getNextID() uint16 {
	id := atomic.AddUint32(&p.idCounter, 1)
	return uint16(id%65534 + 1) // 确保ID在1-65535之间
}

// isIPv6 判断 IP 地址是否为 IPv6
func (p *Pinger) isIPv6(ip string) bool {
	return net.ParseIP(ip).To4() == nil
}
