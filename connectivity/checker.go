package connectivity

import (
	"fmt"
	"net"
	"smartdnssort/logger"
	"sync"
	"sync/atomic"
	"time"

	"os"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// NetworkHealthChecker 网络健康检查器接口
type NetworkHealthChecker interface {
	// IsNetworkHealthy 返回网络是否健康
	IsNetworkHealthy() bool

	// Start 启动定时探测循环
	Start()

	// Stop 停止定时探测循环
	Stop()
}

// NetworkHealthConfig 网络健康检查配置
type NetworkHealthConfig struct {
	ProbeInterval          time.Duration // 探测间隔（已废弃，使用 NormalProbeInterval）
	NormalProbeInterval    time.Duration // 健康状态下的探测间隔
	UnhealthyProbeInterval time.Duration // 故障状态下的探测间隔
	FailureThreshold       int           // 失败阈值（判定故障）
	ProbeTimeout           time.Duration // 单次探测超时
	ProbeIPs               []string      // 探测 IP 列表
	ProbePorts             []int         // TCP 探测端口列表（备选方案）
}

// networkHealthChecker 网络健康检查器实现
type networkHealthChecker struct {
	// 原子操作：网络是否健康
	networkHealthy atomic.Bool

	// 连续失败次数
	consecutiveFailures int

	// 互斥锁保护 consecutiveFailures
	mu sync.Mutex

	// 配置参数
	config NetworkHealthConfig

	// 控制循环
	stopCh chan struct{}
	done   sync.WaitGroup
}

// NewNetworkHealthChecker 创建网络健康检查器（使用默认配置）
func NewNetworkHealthChecker() NetworkHealthChecker {
	return NewNetworkHealthCheckerWithConfig(DefaultNetworkHealthConfig())
}

// NewNetworkHealthCheckerWithConfig 使用自定义配置创建网络健康检查器
func NewNetworkHealthCheckerWithConfig(config NetworkHealthConfig) NetworkHealthChecker {
	checker := &networkHealthChecker{
		config: config,
		stopCh: make(chan struct{}),
	}

	// 初始状态：认为网络正常
	checker.networkHealthy.Store(true)

	return checker
}

// DefaultNetworkHealthConfig 返回默认配置
func DefaultNetworkHealthConfig() NetworkHealthConfig {
	return NetworkHealthConfig{
		// 梯度探测间隔：健康状态 1 分钟，故障状态 20 秒
		NormalProbeInterval:    1 * time.Minute,
		UnhealthyProbeInterval: 20 * time.Second,
		// 向后兼容：保留旧字段
		ProbeInterval: 1 * time.Minute,
		// 故障判定：3 次失败（约 30-60 秒）
		FailureThreshold: 3,
		// 恢复判定：1 次成功即可恢复（快恢复）
		ProbeTimeout: 2 * time.Second,
		ProbeIPs: []string{
			"8.8.8.8",   // Google DNS
			"8.8.4.4",   // Google DNS
			"1.0.0.1",   // Cloudflare DNS
			"223.5.5.5", // Alibaba DNS (China)
			"223.6.6.6", // Alibaba DNS (China)
		},
		// TCP 探测端口（备选方案）
		// 优先使用 443 (HTTPS)，因为 53 (DNS) 极易被本地防火墙或 DNS 服务劫持到本地，导致误判
		ProbePorts: []int{443, 53},
	}
}

// IsNetworkHealthy 返回网络是否健康
func (c *networkHealthChecker) IsNetworkHealthy() bool {
	return c.networkHealthy.Load()
}

// Start 启动定时探测循环
func (c *networkHealthChecker) Start() {
	c.done.Add(1)
	go c.probeLoop()
}

// Stop 停止定时探测循环
func (c *networkHealthChecker) Stop() {
	close(c.stopCh)
	c.done.Wait()
}

// probeLoop 探测循环
func (c *networkHealthChecker) probeLoop() {
	defer c.done.Done()

	// 即时首探：启动时立即执行一次探测
	c.performProbe()

	// 使用梯度探测间隔
	var ticker *time.Ticker
	var tickerCh <-chan time.Time
	var lastInterval time.Duration

	// 创建初始 ticker
	lastInterval = c.getCurrentProbeInterval()
	ticker = time.NewTicker(lastInterval)
	defer ticker.Stop()
	tickerCh = ticker.C

	for {
		select {
		case <-c.stopCh:
			return
		case <-tickerCh:
			c.performProbe()

			// 根据当前状态动态调整探测间隔
			newInterval := c.getCurrentProbeInterval()
			// 仅在间隔变化时重建 ticker，避免不必要的开销
			if newInterval != lastInterval {
				if ticker != nil {
					ticker.Stop()
				}
				ticker = time.NewTicker(newInterval)
				tickerCh = ticker.C
				lastInterval = newInterval
			}
		}
	}
}

// getCurrentProbeInterval 根据当前网络状态返回探测间隔
func (c *networkHealthChecker) getCurrentProbeInterval() time.Duration {
	if c.networkHealthy.Load() {
		// 健康状态：使用正常间隔
		return c.config.NormalProbeInterval
	}
	// 故障状态：使用更短的间隔以快速检测恢复
	return c.config.UnhealthyProbeInterval
}

// performProbe 执行一次探测
func (c *networkHealthChecker) performProbe() {
	healthy := c.probe()

	c.mu.Lock()
	defer c.mu.Unlock()

	if healthy {
		// 探测成功，重置失败计数
		if c.consecutiveFailures > 0 {
			logger.Infof("Network health check passed, failures reset from %d", c.consecutiveFailures)
		}
		c.consecutiveFailures = 0

		// 如果之前网络异常，现在恢复，记录日志（快恢复机制）
		if !c.networkHealthy.Load() {
			logger.Info("Network health recovered, statistics unfrozen")
			c.networkHealthy.Store(true)
		}
	} else {
		// 探测失败，增加失败计数
		c.consecutiveFailures++
		logger.Warnf("Network health check failed (%d/%d)", c.consecutiveFailures, c.config.FailureThreshold)

		// 连续失败达到阈值，标记网络异常
		if c.consecutiveFailures >= c.config.FailureThreshold {
			if c.networkHealthy.Load() {
				logger.Warn("Network health abnormal detected, statistics frozen")
				c.networkHealthy.Store(false)
			}
		}
	}
}

// probe 使用混合协议探测所有 IP，只有全部失败才返回 false
// 优先使用 ICMP ping，失败时尝试 TCP 连接作为备选
// 返回 true 表示至少有一个 IP 探测成功（网络正常）
// 返回 false 表示所有 IP 都探测失败（网络掉线）
func (c *networkHealthChecker) probe() bool {
	// 并发探测所有 IP
	resultCh := make(chan bool, len(c.config.ProbeIPs))
	var wg sync.WaitGroup

	for _, ip := range c.config.ProbeIPs {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			// 优先尝试 ICMP 探测
			if c.probeIP(ip) {
				resultCh <- true
				return
			}

			// ICMP 失败，尝试 TCP 连接作为备选
			if c.probeIPWithTCP(ip) {
				resultCh <- true
				return
			}

			resultCh <- false
		}(ip)
	}

	// 在 goroutine 完成时关闭 channel
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 统计成功和失败的数量，快速失败优化：一旦有成功就返回
	for result := range resultCh {
		if result {
			// 一旦有一个成功，整个探测即认为成功
			return true
		}
	}

	// 所有 IP 都失败
	return false
}

// probeIP 使用 ICMP ping 测试单个 IP
func (c *networkHealthChecker) probeIP(ip string) bool {
	// 1. 尝试创建 ICMP 连接
	// 优先尝试 "ip4:icmp" (需要 root 权限)
	// 如果失败尝试 "udp4:icmp" (非特权模式，某些 Linux 版本支持)
	privileged := true
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		privileged = false
		conn, err = icmp.ListenPacket("udp4:icmp", "0.0.0.0")
		if err != nil {
			logger.Debugf("Failed to create ICMP connection for %s (both privileged and unprivileged): %v", ip, err)
			return false
		}
	}
	defer conn.Close()

	// 设置读超时
	conn.SetDeadline(time.Now().Add(c.config.ProbeTimeout))

	// 使用 PID 作为 ID 减少冲突
	id := os.Getpid() & 0xffff
	seq := 1

	// 创建 ICMP echo 请求
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:  id,
			Seq: seq,
		},
	}

	// 编码消息
	b, err := msg.Marshal(nil)
	if err != nil {
		return false
	}

	// 记录开始时间
	start := time.Now()

	// 发送探测包
	var target net.Addr
	if privileged {
		target = &net.IPAddr{IP: net.ParseIP(ip)}
	} else {
		target = &net.UDPAddr{IP: net.ParseIP(ip)}
	}

	_, err = conn.WriteTo(b, target)
	if err != nil {
		logger.Debugf("Failed to send ICMP to %s: %v", ip, err)
		return false
	}

	// 接收循环（过滤非目标包）
	reply := make([]byte, 256)
	for {
		n, from, err := conn.ReadFrom(reply)
		if err != nil {
			return false
		}

		// 解析回复
		// 注意：raw 和 udp 下解析方式略有不同（Protocol 1 = ICMP）
		rm, err := icmp.ParseMessage(1, reply[:n])
		if err != nil {
			continue
		}

		// 检查源 IP
		fromIP := ""
		if addr, ok := from.(*net.IPAddr); ok {
			fromIP = addr.IP.String()
		} else if addr, ok := from.(*net.UDPAddr); ok {
			fromIP = addr.IP.String()
		}

		if fromIP != ip {
			continue
		}

		// 检查是否是对应的 echo 回复
		if rm.Type == ipv4.ICMPTypeEchoReply {
			if echoReply, ok := rm.Body.(*icmp.Echo); ok {
				if echoReply.ID == id && echoReply.Seq == seq {
					rtt := time.Since(start)
					// 增加虚假成功防御：如果目标是全球公网 IP，但 RTT 极低 (如 < 1ms)，
					// 说明极有可能是在本地环回或被本地防火墙虚假响应。
					if rtt < 1*time.Millisecond {
						logger.Warnf("Suspicious tiny RTT (%v) to global IP %s, might be local interception", rtt, ip)
						// 即使可疑我们也先认为是通的，但记录警告
					}
					logger.Debugf("ICMP probe to %s successful (%s), RTT: %v", ip, fromIP, rtt)
					return true
				}
			}
		}
	}
}

// probeIPWithTCP 使用 TCP 连接测试单个 IP（ICMP 的备选方案）
// 尝试连接配置的端口（如 53, 443），只要有一个端口连接成功就返回 true
func (c *networkHealthChecker) probeIPWithTCP(ip string) bool {
	if len(c.config.ProbePorts) == 0 {
		return false
	}

	for _, port := range c.config.ProbePorts {
		if c.probeIPWithPort(ip, port) {
			logger.Debugf("TCP probe to %s:%d successful", ip, port)
			return true
		}
	}

	logger.Debugf("TCP probe to %s failed on all ports", ip)
	return false
}

// probeIPWithPort 使用 TCP 连接测试指定 IP 和端口
func (c *networkHealthChecker) probeIPWithPort(ip string, port int) bool {
	address := net.JoinHostPort(ip, fmt.Sprintf("%d", port))

	start := time.Now()
	// 设置连接超时
	conn, err := net.DialTimeout("tcp", address, c.config.ProbeTimeout)
	if err != nil {
		logger.Debugf("TCP connection to %s failed: %v", address, err)
		return false
	}
	defer conn.Close()

	rtt := time.Since(start)
	// 高级防御：如果通过 Port 53 探测成功且 RTT 异常低，高度怀疑是被本地 DNS 服务截获
	if port == 53 && rtt < 1*time.Millisecond {
		logger.Warnf("TCP probe to %s:53 succeeded with suspiciously low RTT (%v), possibly intercepted by local DNS server. Ignoring this result.", ip, rtt)
		return false
	}

	// 连接成功
	return true
}

// ====== 全局单例管理 ======

var (
	globalNetworkChecker NetworkHealthChecker
	checkerOnce          sync.Once
)

// GetGlobalNetworkChecker 获取全局网络健康检查器单例
func GetGlobalNetworkChecker() NetworkHealthChecker {
	checkerOnce.Do(func() {
		globalNetworkChecker = NewNetworkHealthChecker()
		globalNetworkChecker.Start()
	})
	return globalNetworkChecker
}

// ShutdownNetworkChecker 关闭网络检查器（程序退出时调用）
func ShutdownNetworkChecker() {
	if globalNetworkChecker != nil {
		globalNetworkChecker.Stop()
	}
}
