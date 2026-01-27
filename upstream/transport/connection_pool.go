package transport

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
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
		maxConnections = 10
	}

	pool := &ConnectionPool{
		address:           address,
		network:           network,
		maxConnections:    maxConnections,
		idleTimeout:       idleTimeout,
		dialTimeout:       5 * time.Second,
		readTimeout:       3 * time.Second,
		writeTimeout:      3 * time.Second,
		idleConns:         make(chan *PooledConnection, maxConnections),
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
	case <-ctx.Done():
		return nil, ctx.Err()
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
			p.mu.Unlock()

			// 连接池满，检查是否启用快速失败
			if p.fastFailMode {
				logger.Warnf("[ConnectionPool] 连接池已满，快速失败: %s", p.address)
				return nil, fmt.Errorf("connection pool exhausted for %s", p.address)
			}

			// 否则等待，但有最大等待时间
			waitCtx, cancel := context.WithTimeout(ctx, p.maxWaitTime)
			defer cancel()

			select {
			case poolConn = <-p.idleConns:
				if poolConn.closed {
					p.mu.Lock()
					p.activeCount--
					p.mu.Unlock()
					return p.Exchange(ctx, msg)
				}
			case <-waitCtx.Done():
				logger.Warnf("[ConnectionPool] 等待连接超时: %s", p.address)
				return nil, fmt.Errorf("wait for connection timeout")
			}
		}
	}

	// 2. 执行查询
	reply, err := p.exchangeOnConnection(ctx, poolConn, msg)

	// 3. 处理结果并归还连接
	if err != nil {
		if p.isTemporaryError(err) {
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

	// 检查网络错误
	if ne, ok := err.(net.Error); ok {
		return ne.Temporary() || ne.Timeout()
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

// adjustPoolSize 自动调整连接池大小
func (p *ConnectionPool) adjustPoolSize() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if time.Since(p.lastAdjustTime) < 60*time.Second {
		return
	}

	utilization := float64(p.activeCount) / float64(p.maxConnections)

	// 如果利用率过高，增加最大连接数
	if utilization > 0.8 && p.maxConnections < MaxConnectionsLimit {
		newMax := min(p.maxConnections+5, MaxConnectionsLimit)
		logger.Debugf("[ConnectionPool] 自动扩容: %d -> %d (利用率: %.1f%%)", p.maxConnections, newMax, utilization*100)
		p.maxConnections = newMax
		// 扩大通道容量
		newChan := make(chan *PooledConnection, newMax)
		for {
			select {
			case conn := <-p.idleConns:
				newChan <- conn
			default:
				p.idleConns = newChan
				return
			}
		}
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
			break
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
		"max_connections": p.maxConnections,
		"total_created":   p.totalCreated,
		"total_destroyed": p.totalDestroyed,
		"total_errors":    p.totalErrors,
		"total_requests":  p.totalRequests,
		"reuse_rate":      reuseRate,
		"error_rate":      errorRate,
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
