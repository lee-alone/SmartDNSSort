package upstream

import (
	"context"
	"net"
	"smartdnssort/logger"
	"sync"
	"sync/atomic"
	"time"
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
	ProbeInterval       time.Duration // 探测间隔
	FailureThreshold    int           // 失败阈值
	ProbeTimeout        time.Duration // 单次探测超时
	ProbeDomains        []string      // 探测域名列表
	TestPorts           []string      // 测试的端口列表
	MaxTestIPsPerDomain int           // 每个域名最多测试的IP数
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
		ProbeInterval:       5 * time.Minute,
		FailureThreshold:    2,
		ProbeTimeout:        2 * time.Second,       // 2秒超时
		MaxTestIPsPerDomain: 3,                     // 每个域名最多测试3个IP
		TestPorts:           []string{"443", "80"}, // 443优先，更可靠
		ProbeDomains: []string{
			"www.taobao.com",
			"www.apple.com",
			"www.microsoft.com",
			"www.cloudflare.com",
			"www.jd.com",
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

// probe 使用TCP连接测试探测所有域名，只有全部失败才返回false
// 返回true表示至少有一个域名ping通（网络正常）
// 返回false表示所有域名都ping不通（网络掉线）
func (c *networkHealthChecker) probe() bool {
	// 为所有DNS解析创建共享的context，3秒超时
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 并发探测所有域名
	resultCh := make(chan bool, len(c.config.ProbeDomains))
	var wg sync.WaitGroup

	for _, domain := range c.config.ProbeDomains {
		wg.Add(1)
		go func(domain string) {
			defer wg.Done()
			resultCh <- c.probeDomainWithCtx(domain, ctx)
		}(domain)
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

	// 所有域名都失败
	return false
}

// probeDomainWithCtx 使用TCP连接测试探测单个域名
// 测试前3个IP（优先选择IPv4），每个IP测试443和80端口
// ctx 是共享的DNS解析超时context
func (c *networkHealthChecker) probeDomainWithCtx(domain string, ctx context.Context) bool {
	// 解析域名获取IP地址（使用共享的context）
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, domain)
	if err != nil || len(ips) == 0 {
		return false
	}

	dialer := net.Dialer{Timeout: c.config.ProbeTimeout}

	// 测试前 MaxTestIPsPerDomain 个 IP（优先选择 IPv4）
	testCount := 0
	for _, ipAddr := range ips {
		if testCount >= c.config.MaxTestIPsPerDomain {
			break
		}

		ip := ipAddr.IP

		// 优先测 IPv4
		if ip.To4() == nil {
			continue
		}
		testCount++

		// 测试配置的端口（443优先，更可靠）
		for _, port := range c.config.TestPorts {
			conn, err := dialer.Dial("tcp", ip.String()+":"+port)
			if err == nil {
				conn.Close()
				return true
			}
		}
	}

	return false
}

// probeDomain 使用TCP连接测试探测单个域名（已弃用，保留用于向后兼容）
// 新代码应使用 probeDomainWithCtx
func (c *networkHealthChecker) probeDomain(domain string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.probeDomainWithCtx(domain, ctx)
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
