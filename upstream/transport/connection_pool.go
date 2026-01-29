package transport

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"smartdnssort/logger"

	"github.com/miekg/dns"
)

// 常量定义
const (
	MaxDNSMessageSize   = 65535
	WarnLargeMsgSize    = 4096
	MinConnections      = 2
	MaxConnectionsLimit = 50
)

var (
	// ErrPoolExhausted 当连接池达到上限且在弹性等待时间内未获取连接时返回
	ErrPoolExhausted = fmt.Errorf("transport pool exhausted")
	// ErrRequestThrottled 当系统由于极度拥塞触发主动限流时返回
	ErrRequestThrottled = fmt.Errorf("transport request throttled")
)

// ConnectionPool 管理到单个上游服务器的连接池
type ConnectionPool struct {
	address string
	network string // "udp" 或 "tcp"

	mu sync.Mutex

	// 空闲连接通道，用于高效复用和排队
	idleConns chan *PooledConnection

	// 当前总连接数（包含空闲和在使用中的）
	activeCount int

	// 配置
	maxConnections int
	idleTimeout    time.Duration
	dialTimeout    time.Duration
	readTimeout    time.Duration
	writeTimeout   time.Duration

	// 自适应参数
	minConnections    int
	targetUtilization float64
	lastAdjustTime    time.Time

	// 降级配置
	fastFailMode bool
	maxWaitTime  time.Duration

	// 监控指标
	totalCreated   int64
	totalDestroyed int64
	totalErrors    int64
	totalRequests  int64
	totalCongested int64 // 由于拥塞导致快速失败的次数

	// 性能感悟
	avgLatency   time.Duration // 平均上游延迟 (EWMA)
	waitingCount int32         // 当前正在排队等待连接的请求数

	// 清理 goroutine 控制
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// PooledConnection 代表一个复用的连接
type PooledConnection struct {
	conn        net.Conn
	address     string
	network     string
	lastUsed    time.Time
	closed      bool
	createdAt   time.Time
	usageCount  int64
	lastError   error
	lastErrorAt time.Time
}

// NewConnectionPool 创建连接池
func NewConnectionPool(address, network string, maxConnections int, idleTimeout time.Duration) *ConnectionPool {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "53")
	}

	if maxConnections <= 0 {
		// 自动计算默认值：CPU 核数 * 5，但最低不少于 20
		maxConnections = runtime.NumCPU() * 5
		if maxConnections < 20 {
			maxConnections = 20
		}
		logger.Debugf("[ConnectionPool] Auto-calculated MaxConnections for %s: %d", address, maxConnections)
	}

	pool := &ConnectionPool{
		address:           address,
		network:           network,
		maxConnections:    maxConnections,
		idleTimeout:       idleTimeout,
		dialTimeout:       5 * time.Second,
		readTimeout:       3 * time.Second,
		writeTimeout:      3 * time.Second,
		idleConns:         make(chan *PooledConnection, MaxConnectionsLimit),
		stopChan:          make(chan struct{}),
		minConnections:    MinConnections,
		targetUtilization: 0.7,
		fastFailMode:      false,
		maxWaitTime:       5 * time.Second,
	}

	// 启动清理 goroutine
	pool.wg.Add(1)
	go pool.cleanupLoop()

	// 自动预热 50% 的连接
	go func() {
		time.Sleep(100 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		warmupCount := maxConnections / 2
		if warmupCount > 0 {
			pool.Warmup(ctx, warmupCount)
		}
	}()

	// 启动连接健康检查 goroutine
	pool.wg.Add(1)
	go pool.healthCheckLoop()

	return pool
}

// Exchange 通过连接池执行 DNS 查询
func (p *ConnectionPool) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	var poolConn *PooledConnection
	var err error

	p.mu.Lock()
	p.totalRequests++
	p.mu.Unlock()

	// 1. 尝试获取连接
	select {
	case poolConn = <-p.idleConns:
		// 检查获取到的空闲连接是否已失效
		if poolConn.closed {
			p.mu.Lock()
			p.activeCount--
			p.mu.Unlock()
			return p.Exchange(ctx, msg) // 递归获取下一个
		}
	default:
		// 池中没有空闲连接，尝试创建新连接
		p.mu.Lock()
		if p.activeCount < p.maxConnections {
			p.activeCount++
			p.mu.Unlock()

			poolConn, err = p.createConnection(ctx)
			if err != nil {
				p.mu.Lock()
				p.activeCount--
				p.mu.Unlock()
				return nil, err
			}
		} else {
			// 达到上限，进入弹性等待机制
			waiting := atomic.AddInt32(&p.waitingCount, 1)
			defer atomic.AddInt32(&p.waitingCount, -1)
			p.mu.Unlock()

			// 计算弹性等待时间：通常为平均延迟的 1/10，且在 10ms - 200ms 之间
			waitDuration := p.getAdaptiveWaitTime()

			// 如果启用 fastFailMode 且排队人数过多，直接降级
			if p.fastFailMode && waiting > 20 {
				p.recordCongestion()
				return nil, ErrRequestThrottled
			}

			timer := time.NewTimer(waitDuration)
			defer timer.Stop()

			select {
			case poolConn = <-p.idleConns:
				if poolConn.closed {
					p.mu.Lock()
					p.activeCount--
					p.mu.Unlock()
					return p.Exchange(ctx, msg)
				}
			case <-timer.C:
				p.recordCongestion()
				return nil, ErrPoolExhausted
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	// 2. 执行查询
	startTime := time.Now()
	reply, err := p.exchangeOnConnection(ctx, poolConn, msg)
	latency := time.Since(startTime)

	// 3. 处理结果并归还连接
	if err != nil {
		if p.isTemporaryError(err) {
			// 特殊修复：UDP 超时后必须销毁连接，防止后续复用时读到迟到的脏包（串号问题）
			if p.network == "udp" {
				logger.Debugf("[ConnectionPool] UDP 超时，强制销毁连接以防脏包: %v", err)
				poolConn.conn.Close()
				poolConn.closed = true
				p.mu.Lock()
				p.activeCount--
				p.totalDestroyed++
				p.totalErrors++
				p.mu.Unlock()
				return nil, err
			}

			// 临时错误：放回池中，让下一个请求重试
			logger.Debugf("[ConnectionPool] 临时错误，连接放回池: %v", err)
			poolConn.lastUsed = time.Now()
			poolConn.lastError = err
			poolConn.lastErrorAt = time.Now()

			select {
			case p.idleConns <- poolConn:
				return nil, err
			default:
				poolConn.conn.Close()
				poolConn.closed = true
				p.mu.Lock()
				p.activeCount--
				p.totalDestroyed++
				p.totalErrors++
				p.mu.Unlock()
			}
		} else {
			// 永久错误：关闭连接
			logger.Debugf("[ConnectionPool] 永久错误，关闭连接: %v", err)
			poolConn.conn.Close()
			poolConn.closed = true
			p.mu.Lock()
			p.activeCount--
			p.totalDestroyed++
			p.totalErrors++
			p.mu.Unlock()
		}
		return nil, err
	}

	// 成功！更新平均延迟
	p.updateAvgLatency(latency)

	// 更新最后使用时间并放回池中
	poolConn.lastUsed = time.Now()
	poolConn.usageCount++
	select {
	case p.idleConns <- poolConn:
		// 成功归还
	default:
		// 通道满（不应发生），直接关闭
		poolConn.conn.Close()
		poolConn.closed = true
		p.mu.Lock()
		p.activeCount--
		p.totalDestroyed++
		p.mu.Unlock()
	}

	return reply, nil
}

// updateAvgLatency 更新 EWMA 平均延迟
func (p *ConnectionPool) updateAvgLatency(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.avgLatency == 0 {
		p.avgLatency = d
	} else {
		// Alpha = 0.2
		p.avgLatency = time.Duration(0.2*float64(d) + 0.8*float64(p.avgLatency))
	}
}

// getAdaptiveWaitTime 获取自适应等待时间
func (p *ConnectionPool) getAdaptiveWaitTime() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 默认为平均延迟的 10%
	wait := p.avgLatency / 10
	if wait < 10*time.Millisecond {
		wait = 10 * time.Millisecond
	}
	if wait > 200*time.Millisecond {
		wait = 200 * time.Millisecond
	}
	// 如果并没有平均延迟数据，使用默认的 50ms
	if p.avgLatency == 0 {
		return 50 * time.Millisecond
	}
	return wait
}

// recordCongestion 记录拥塞事件并触发自省优化
func (p *ConnectionPool) recordCongestion() {
	p.mu.Lock()
	p.totalCongested++

	// 探测：如果频繁发生拥塞，立刻触发一次扩容检查，不再等待 ticker
	if p.totalCongested%5 == 0 {
		go p.adjustPoolSizeNow()
	}
	p.mu.Unlock()
	logger.Warnf("[ConnectionPool] ⚠️ 触发快速失败/拥塞控制: %s, 活跃=%d, 排队=%d",
		p.address, p.activeCount, atomic.LoadInt32(&p.waitingCount))
}

// adjustPoolSizeNow 立刻执行扩容检查（用于 recordCongestion 触发）
func (p *ConnectionPool) adjustPoolSizeNow() {
	p.mu.Lock()
	// 如果已经达到硬限制，不再尝试
	if p.maxConnections >= MaxConnectionsLimit {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.adjustPoolSize()
}

// createConnection 创建一个新的连接
func (p *ConnectionPool) createConnection(ctx context.Context) (*PooledConnection, error) {
	dialer := &net.Dialer{
		Timeout: p.dialTimeout,
	}

	conn, err := dialer.DialContext(ctx, p.network, p.address)
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	if p.network == "tcp" {
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetNoDelay(true)
		}
	}

	p.mu.Lock()
	p.totalCreated++
	p.mu.Unlock()

	return &PooledConnection{
		conn:       conn,
		address:    p.address,
		network:    p.network,
		lastUsed:   time.Now(),
		closed:     false,
		createdAt:  time.Now(),
		usageCount: 0,
	}, nil
}

// validateMessageSize 验证 DNS 消息大小
func (p *ConnectionPool) validateMessageSize(msgLen int) error {
	if msgLen <= 0 {
		return fmt.Errorf("invalid message length: %d (must be > 0)", msgLen)
	}

	if msgLen > MaxDNSMessageSize {
		return fmt.Errorf("message too large: %d > %d", msgLen, MaxDNSMessageSize)
	}

	if msgLen > WarnLargeMsgSize {
		logger.Warnf("[ConnectionPool] 大型 DNS 消息: %d 字节 (来自 %s)", msgLen, p.address)
	}

	return nil
}

// isTemporaryError 判断是否是临时错误
func (p *ConnectionPool) isTemporaryError(err error) bool {
	if err == nil {
		return false
	}

	// 检查网络错误 - 只检查 Timeout，Temporary() 已弃用
	if ne, ok := err.(net.Error); ok {
		return ne.Timeout()
	}

	// 检查上下文错误
	if err == context.DeadlineExceeded {
		return true // 超时是临时错误
	}

	return false
}

// exchangeOnConnection 在给定连接上执行 DNS 查询
func (p *ConnectionPool) exchangeOnConnection(ctx context.Context, poolConn *PooledConnection, msg *dns.Msg) (*dns.Msg, error) {
	conn := poolConn.conn

	// 设置连接超时
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(p.readTimeout + p.writeTimeout))
	}

	// 打包 DNS 消息
	buf, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack failed: %w", err)
	}

	if p.network == "tcp" {
		// TCP DNS 格式：2 字节长度 + DNS 消息
		conn.SetWriteDeadline(time.Now().Add(p.writeTimeout))
		lenBuf := make([]byte, 2)
		lenBuf[0] = byte(len(buf) >> 8)
		lenBuf[1] = byte(len(buf))
		if _, err := conn.Write(lenBuf); err != nil {
			return nil, fmt.Errorf("write tcp length failed: %w", err)
		}
	}

	// 发送 DNS 消息体
	conn.SetWriteDeadline(time.Now().Add(p.writeTimeout))
	if _, err := conn.Write(buf); err != nil {
		return nil, fmt.Errorf("write failed: %w", err)
	}

	// 接收响应
	var resBuf []byte
	if p.network == "tcp" {
		// 1. 读取 2 字节长度前缀
		conn.SetReadDeadline(time.Now().Add(p.readTimeout))
		lenBuf := make([]byte, 2)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return nil, fmt.Errorf("read tcp length failed: %w", err)
		}
		msgLen := int(lenBuf[0])<<8 | int(lenBuf[1])

		// 验证消息大小
		if err := p.validateMessageSize(msgLen); err != nil {
			return nil, err
		}

		// 2. 精确读取消息体
		conn.SetReadDeadline(time.Now().Add(p.readTimeout))
		resBuf = make([]byte, msgLen)
		if _, err := io.ReadFull(conn, resBuf); err != nil {
			return nil, fmt.Errorf("read tcp body failed: %w", err)
		}
	} else {
		// UDP 响应
		conn.SetReadDeadline(time.Now().Add(p.readTimeout))
		resBuf = make([]byte, 4096)
		n, err := conn.Read(resBuf)
		if err != nil {
			return nil, fmt.Errorf("read udp failed: %w", err)
		}
		resBuf = resBuf[:n]
	}

	// 清除超时
	conn.SetDeadline(time.Time{})

	// 解包 DNS 消息
	reply := new(dns.Msg)
	if err := reply.Unpack(resBuf); err != nil {
		return nil, fmt.Errorf("unpack failed: %w", err)
	}

	// 关键修复：校验 Transaction ID
	if reply.Id != msg.Id {
		return nil, fmt.Errorf("dns id mismatch: request=%d, response=%d", msg.Id, reply.Id)
	}

	return reply, nil
}

// cleanupLoop 定期清理空闲连接
func (p *ConnectionPool) cleanupLoop() {
	defer p.wg.Done()

	// 初始清理间隔
	cleanupInterval := p.idleTimeout / 3
	if cleanupInterval < 30*time.Second {
		cleanupInterval = 30 * time.Second
	}
	if cleanupInterval > 5*time.Minute {
		cleanupInterval = 5 * time.Minute
	}

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			p.closeAll()
			return
		case <-ticker.C:
			p.cleanupExpiried()
			p.adjustPoolSize()

			// 根据空闲连接数动态调整清理间隔
			idleCount := len(p.idleConns)
			if idleCount > p.maxConnections/2 {
				// 空闲连接多，加快清理
				cleanupInterval = p.idleTimeout / 4
			} else if idleCount < 2 {
				// 空闲连接少，减慢清理
				cleanupInterval = p.idleTimeout / 2
			} else {
				// 正常情况
				cleanupInterval = p.idleTimeout / 3
			}

			ticker.Reset(cleanupInterval)
		}
	}
}

// healthCheckLoop 定期检查连接健康状态
func (p *ConnectionPool) healthCheckLoop() {
	defer p.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			return
		case <-ticker.C:
			p.performHealthCheck()
		}
	}
}

// performHealthCheck 执行连接健康检查
func (p *ConnectionPool) performHealthCheck() {
	p.mu.Lock()

	// 检查空闲连接数
	idleCount := len(p.idleConns)
	if idleCount < p.minConnections {
		// 补充连接
		needed := p.minConnections - idleCount
		p.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		p.Warmup(ctx, needed)
		cancel()

		p.mu.Lock()
	}

	p.mu.Unlock()

	logger.Debugf("[ConnectionPool] 健康检查完成: %s, 空闲连接数: %d", p.address, len(p.idleConns))
}

// adjustPoolSize 自动调整连接池大小
func (p *ConnectionPool) adjustPoolSize() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if time.Since(p.lastAdjustTime) < 10*time.Second {
		return
	}

	utilization := float64(p.activeCount) / float64(p.maxConnections)

	// 如果利用率过高，增加最大连接数
	if utilization > 0.8 && p.maxConnections < MaxConnectionsLimit {
		newMax := min(p.maxConnections+5, MaxConnectionsLimit)
		logger.Debugf("[ConnectionPool] 自动扩容: %d -> %d (利用率: %.1f%%)", p.maxConnections, newMax, utilization*100)
		p.maxConnections = newMax
	}

	// 如果利用率过低，减少最大连接数
	if utilization < 0.2 && p.maxConnections > p.minConnections {
		newMax := max(p.maxConnections-2, p.minConnections)
		logger.Debugf("[ConnectionPool] 自动缩容: %d -> %d (利用率: %.1f%%)", p.maxConnections, newMax, utilization*100)
		p.maxConnections = newMax
	}

	p.lastAdjustTime = time.Now()
}

func (p *ConnectionPool) cleanupExpiried() {
	// 遍历池中连接，清理过期的
	count := len(p.idleConns)
	for i := 0; i < count; i++ {
		select {
		case conn := <-p.idleConns:
			if !conn.closed && time.Since(conn.lastUsed) < p.idleTimeout {
				// 没过期，放回去
				p.idleConns <- conn
			} else {
				// 已过期或已标记关闭
				if !conn.closed {
					conn.conn.Close()
					conn.closed = true
				}
				p.mu.Lock()
				p.activeCount--
				p.totalDestroyed++
				p.mu.Unlock()
				logger.Debugf("[ConnectionPool] 清理空闲过期的连接: %s", p.address)
			}
		default:
			return
		}
	}
}

func (p *ConnectionPool) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		select {
		case conn := <-p.idleConns:
			conn.conn.Close()
			conn.closed = true
			p.activeCount--
		default:
			return
		}
	}
}

func (p *ConnectionPool) Close() error {
	close(p.stopChan)
	p.wg.Wait()
	return nil
}

// Warmup 预热连接池
func (p *ConnectionPool) Warmup(ctx context.Context, count int) error {
	if count > p.maxConnections {
		count = p.maxConnections
	}

	for i := 0; i < count; i++ {
		conn, err := p.createConnection(ctx)
		if err != nil {
			logger.Warnf("[ConnectionPool] 预热失败: %v", err)
			continue
		}

		select {
		case p.idleConns <- conn:
			p.mu.Lock()
			p.activeCount++
			p.mu.Unlock()
		default:
			conn.conn.Close()
		}
	}

	logger.Debugf("[ConnectionPool] 预热完成: %s, 预热连接数: %d", p.address, count)
	return nil
}

// GetConnectionStats 获取单个连接的统计信息
func (p *ConnectionPool) GetConnectionStats() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	var avgUsageCount float64
	var maxUsageCount int64
	var minUsageCount int64 = math.MaxInt64

	count := len(p.idleConns)
loop:
	for i := 0; i < count; i++ {
		select {
		case conn := <-p.idleConns:
			avgUsageCount += float64(conn.usageCount)
			if conn.usageCount > maxUsageCount {
				maxUsageCount = conn.usageCount
			}
			if conn.usageCount < minUsageCount {
				minUsageCount = conn.usageCount
			}
			p.idleConns <- conn
		default:
			break loop
		}
	}

	if count > 0 {
		avgUsageCount /= float64(count)
	}

	if minUsageCount == math.MaxInt64 {
		minUsageCount = 0
	}

	return map[string]interface{}{
		"avg_usage_count": avgUsageCount,
		"max_usage_count": maxUsageCount,
		"min_usage_count": minUsageCount,
	}
}

// GetStats 获取连接池统计信息
func (p *ConnectionPool) GetStats() map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	reuseRate := 0.0
	if p.totalCreated > 0 {
		reuseRate = float64(p.totalRequests) / float64(p.totalCreated)
	}

	errorRate := 0.0
	if p.totalRequests > 0 {
		errorRate = float64(p.totalErrors) / float64(p.totalRequests) * 100
	}

	return map[string]interface{}{
		"address":         p.address,
		"network":         p.network,
		"active_count":    p.activeCount,
		"idle_count":      len(p.idleConns),
		"waiting_count":   atomic.LoadInt32(&p.waitingCount),
		"max_connections": p.maxConnections,
		"total_created":   p.totalCreated,
		"total_destroyed": p.totalDestroyed,
		"total_errors":    p.totalErrors,
		"total_requests":  p.totalRequests,
		"total_congested": p.totalCongested,
		"reuse_rate":      reuseRate,
		"error_rate":      errorRate,
		"avg_latency_ms":  float64(p.avgLatency.Microseconds()) / 1000.0,
	}
}

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
