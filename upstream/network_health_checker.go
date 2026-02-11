package upstream

import (
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
	ProbeInterval    time.Duration // 探测间隔
	FailureThreshold int           // 失败阈值
	ProbeTimeout     time.Duration // 单次探测超时
	ProbeIPs         []string      // 探测IP列表
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
		ProbeInterval:    5 * time.Minute,
		FailureThreshold: 2,
		ProbeTimeout:     2 * time.Second,
		ProbeIPs: []string{
			"8.8.8.8",   // Google DNS
			"8.8.4.4",   // Google DNS
			"223.5.5.5", // Alibaba DNS (China)
			"223.6.6.6", // Alibaba DNS (China)
		},
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

	ticker := time.NewTicker(c.config.ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.performProbe()
		}
	}
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

		// 如果之前网络异常，现在恢复，记录日志
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

// probe 使用ICMP ping测试探测所有IP，只有全部失败才返回false
// 返回true表示至少有一个IP ping通（网络正常）
// 返回false表示所有IP都ping不通（网络掉线）
func (c *networkHealthChecker) probe() bool {
	// 并发探测所有IP
	resultCh := make(chan bool, len(c.config.ProbeIPs))
	var wg sync.WaitGroup

	for _, ip := range c.config.ProbeIPs {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			resultCh <- c.probeIP(ip)
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
