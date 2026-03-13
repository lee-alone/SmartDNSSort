package upstream

import (
	"fmt"
	"net"
	"smartdnssort/logger"
	"sync"
	"sync/atomic"
	"time"

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
	ProbeInterval          time.Duration // 探测间隔（已废弃，使用NormalProbeInterval）
	NormalProbeInterval    time.Duration // 健康状态下的探测间隔
	UnhealthyProbeInterval time.Duration // 故障状态下的探测间隔
	FailureThreshold       int           // 失败阈值（判定故障）
	ProbeTimeout           time.Duration // 单次探测超时
	ProbeIPs               []string      // 探测IP列表
	ProbePorts             []int         // TCP探测端口列表（备选方案）
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
		// 梯度探测间隔：健康状态1分钟，故障状态20秒
		NormalProbeInterval:    1 * time.Minute,
		UnhealthyProbeInterval: 20 * time.Second,
		// 向后兼容：保留旧字段
		ProbeInterval: 1 * time.Minute,
		// 故障判定：3次失败（约30-60秒）
		FailureThreshold: 3,
		// 恢复判定：1次成功即可恢复（快恢复）
		ProbeTimeout: 2 * time.Second,
		ProbeIPs: []string{
			"8.8.8.8",   // Google DNS
			"8.8.4.4",   // Google DNS
			"223.5.5.5", // Alibaba DNS (China)
			"223.6.6.6", // Alibaba DNS (China)
		},
		// TCP探测端口（备选方案）
		ProbePorts: []int{53, 443}, // DNS TCP端口和HTTPS端口
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

	// 创建初始ticker
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
			// 仅在间隔变化时重建ticker，避免不必要的开销
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

// probe 使用混合协议探测所有IP，只有全部失败才返回false
// 优先使用ICMP ping，失败时尝试TCP连接作为备选
// 返回true表示至少有一个IP探测成功（网络正常）
// 返回false表示所有IP都探测失败（网络掉线）
func (c *networkHealthChecker) probe() bool {
	// 并发探测所有IP
	resultCh := make(chan bool, len(c.config.ProbeIPs))
	var wg sync.WaitGroup

	for _, ip := range c.config.ProbeIPs {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			// 优先尝试ICMP探测
			if c.probeIP(ip) {
				resultCh <- true
				return
			}

			// ICMP失败，尝试TCP连接作为备选
			if c.probeIPWithTCP(ip) {
				resultCh <- true
				return
			}

			resultCh <- false
		}(ip)
	}

	// 在goroutine完成时关闭channel
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 统计成功和失败的数量，快速失败优化：一旦有成功就返回
	for result := range resultCh {
		if result {
			// 已经至少一个成功，立即返回
			// 其他goroutine继续运行但结果被忽略
			return true
		}
	}

	// 所有IP都失败
	return false
}

// probeIP 使用ICMP ping测试单个IP
func (c *networkHealthChecker) probeIP(ip string) bool {
	// 创建ICMP连接
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		logger.Debugf("Failed to create ICMP connection for %s: %v", ip, err)
		return false
	}
	defer conn.Close()

	// 设置读超时
	conn.SetDeadline(time.Now().Add(c.config.ProbeTimeout))

	// 记录开始时间用于RTT统计
	start := time.Now()

	// 创建ICMP echo请求
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:  1,
			Seq: 1,
		},
	}

	// 编码消息
	b, err := msg.Marshal(nil)
	if err != nil {
		logger.Debugf("Failed to marshal ICMP message for %s: %v", ip, err)
		return false
	}

	// 发送ICMP echo请求
	_, err = conn.WriteTo(b, &net.IPAddr{IP: net.ParseIP(ip)})
	if err != nil {
		logger.Debugf("Failed to send ICMP echo to %s: %v", ip, err)
		return false
	}

	// 接收ICMP echo回复（缓冲区大小优化为256字节）
	reply := make([]byte, 256)
	_, _, err = conn.ReadFrom(reply)
	if err != nil {
		logger.Debugf("Failed to receive ICMP echo reply from %s: %v", ip, err)
		return false
	}

	// 解析回复（1 = ICMPv4 protocol number）
	rm, err := icmp.ParseMessage(1, reply)
	if err != nil {
		logger.Debugf("Failed to parse ICMP reply from %s: %v", ip, err)
		return false
	}

	// 检查是否是echo回复，并验证ID和Seq
	if rm.Type == ipv4.ICMPTypeEchoReply {
		if echoReply, ok := rm.Body.(*icmp.Echo); ok {
			if echoReply.ID == 1 && echoReply.Seq == 1 {
				rtt := time.Since(start)
				logger.Debugf("Ping to %s successful, RTT: %v", ip, rtt)
				return true
			}
		}
	}

	logger.Debugf("Unexpected ICMP response from %s: %v", ip, rm.Type)
	return false
}

// probeIPWithTCP 使用TCP连接测试单个IP（ICMP的备选方案）
// 尝试连接配置的端口（如53, 443），只要有一个端口连接成功就返回true
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

// probeIPWithPort 使用TCP连接测试指定IP和端口
func (c *networkHealthChecker) probeIPWithPort(ip string, port int) bool {
	address := net.JoinHostPort(ip, fmt.Sprintf("%d", port))

	// 设置连接超时
	conn, err := net.DialTimeout("tcp", address, c.config.ProbeTimeout)
	if err != nil {
		logger.Debugf("TCP connection to %s failed: %v", address, err)
		return false
	}
	defer conn.Close()

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
