package upstream

import (
	"io"
	"net/http"
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

// networkHealthChecker 网络健康检查器实现
type networkHealthChecker struct {
	// 原子操作：网络是否健康
	networkHealthy atomic.Bool

	// 连续失败次数
	consecutiveFailures int

	// 互斥锁保护 consecutiveFailures
	mu sync.Mutex

	// 配置参数（硬编码）
	probeInterval    time.Duration // 探测间隔（5分钟）
	failureThreshold int           // 失败阈值（2次）
	probeTimeout     time.Duration // 单次探测超时（5秒）
	probeURLs        []string      // 探测URL列表

	// 控制循环
	stopCh chan struct{}
	done   sync.WaitGroup
}

// NewNetworkHealthChecker 创建网络健康检查器
func NewNetworkHealthChecker() NetworkHealthChecker {
	checker := &networkHealthChecker{
		probeInterval:    5 * time.Minute, // 5分钟探测一次
		failureThreshold: 2,               // 连续失败2次标记异常
		probeTimeout:     5 * time.Second, // 5秒超时
		probeURLs: []string{
			"http://www.msftconnecttest.com/connecttest.txt",    // Windows NCSI
			"http://connectivitycheck.gstatic.com/generate_204", // Android NCSI
			"http://www.apple.com/library/test/success.html",    // Apple NCSI
		},
		stopCh: make(chan struct{}),
	}

	// 初始状态：认为网络正常
	checker.networkHealthy.Store(true)

	return checker
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

	ticker := time.NewTicker(c.probeInterval)
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
		c.consecutiveFailures = 0

		// 如果之前网络异常，现在恢复，记录日志
		if !c.networkHealthy.Load() {
			logger.Info("Network health recovered, statistics unfrozen")
			c.networkHealthy.Store(true)
		}
	} else {
		// 探测失败，增加失败计数
		c.consecutiveFailures++

		// 连续失败达到阈值，标记网络异常
		if c.consecutiveFailures >= c.failureThreshold {
			if c.networkHealthy.Load() {
				logger.Warn("Network health abnormal detected, statistics frozen")
				c.networkHealthy.Store(false)
			}
		}
	}
}

// probe 并发探测所有URL，任一成功则返回true
func (c *networkHealthChecker) probe() bool {
	// 使用WaitGroup和channel并发探测，任一成功即返回
	resultCh := make(chan bool, len(c.probeURLs))
	var wg sync.WaitGroup

	for _, url := range c.probeURLs {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			resultCh <- c.probeURL(url)
		}(url)
	}

	// 在goroutine完成时关闭channel
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// 任一URL返回success就认为网络正常
	for result := range resultCh {
		if result {
			return true
		}
	}

	return false
}

// probeURL 单个URL的探测
func (c *networkHealthChecker) probeURL(url string) bool {
	client := &http.Client{
		Timeout: c.probeTimeout,
		// 不自动跟随重定向，检查原始响应状态码
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // 不自动跟随重定向
		},
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}

	// 添加合理的User-Agent
	req.Header.Set("User-Agent", "SmartDNSSort/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return false
	}

	defer func() {
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}()

	// HTTP状态码判断：
	// Windows NCSI 返回 200 OK
	// Android NCSI 返回 204 No Content
	// Apple NCSI 返回 302 Found (重定向)，302说明网络通畅
	// 只要能收到HTTP响应就说明网络通常，因此200/204/302都视为成功
	return resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusNoContent ||
		resp.StatusCode == http.StatusFound
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
