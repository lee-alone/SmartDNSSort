package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math"
	"net"
	"sync"
	"time"

	"smartdnssort/logger"

	"github.com/miekg/dns"
)

// TLSConnectionPool 管理到单个 DoT 服务器的 TLS 连接池
type TLSConnectionPool struct {
	address    string
	serverName string

	mu sync.Mutex

	// 空闲连接通道
	idleConns chan *PooledTLSConnection

	// 当前总连接数
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

	tlsConfig *tls.Config

	// 清理 goroutine 控制
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// PooledTLSConnection 代表一个复用的 TLS 连接
type PooledTLSConnection struct {
	conn        *tls.Conn
	address     string
	lastUsed    time.Time
	closed      bool
	createdAt   time.Time
	usageCount  int64
	lastError   error
	lastErrorAt time.Time
}

// NewTLSConnectionPool 创建 TLS 连接池
func NewTLSConnectionPool(address, serverName string, maxConnections int, idleTimeout time.Duration) *TLSConnectionPool {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "853")
	}

	tlsConfig := &tls.Config{
		ServerName: serverName,
	}

	if maxConnections <= 0 {
		maxConnections = 10
	}

	pool := &TLSConnectionPool{
		address:           address,
		serverName:        serverName,
		maxConnections:    maxConnections,
		idleTimeout:       idleTimeout,
		dialTimeout:       5 * time.Second,
		readTimeout:       3 * time.Second,
		writeTimeout:      3 * time.Second,
		tlsConfig:         tlsConfig,
		idleConns:         make(chan *PooledTLSConnection, maxConnections),
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

// Exchange 通过 TLS 连接池执行 DNS 查询
func (p *TLSConnectionPool) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	var poolConn *PooledTLSConnection
	var err error

	p.mu.Lock()
	p.totalRequests++
	p.mu.Unlock()

	// 1. 获取连接（独占方式）
	select {
	case poolConn = <-p.idleConns:
		if poolConn.closed {
			p.mu.Lock()
			p.activeCount--
			p.mu.Unlock()
			return p.Exchange(ctx, msg)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
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
				logger.Warnf("[TLSConnectionPool] 连接池已满，快速失败: %s", p.address)
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
				logger.Warnf("[TLSConnectionPool] 等待连接超时: %s", p.address)
				return nil, fmt.Errorf("wait for connection timeout")
			}
		}
	}

	// 2. 在独占连接上执行查询
	reply, err := p.exchangeOnConnection(ctx, poolConn, msg)

	// 3. 处理故障和归还
	if err != nil {
		if p.isTemporaryError(err) {
			logger.Debugf("[TLSConnectionPool] 临时错误，连接放回池: %v", err)
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
			logger.Debugf("[TLSConnectionPool] 永久错误，关闭连接: %v", err)
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

	poolConn.lastUsed = time.Now()
	poolConn.usageCount++
	select {
	case p.idleConns <- poolConn:
	default:
		poolConn.conn.Close()
		poolConn.closed = true
		p.mu.Lock()
		p.activeCount--
		p.totalDestroyed++
		p.mu.Unlock()
	}

	return reply, nil
}

// createConnection 创建一个新的 TLS 连接
func (p *TLSConnectionPool) createConnection(ctx context.Context) (*PooledTLSConnection, error) {
	dialer := &net.Dialer{
		Timeout: p.dialTimeout,
	}

	conn, err := tls.DialWithDialer(dialer, "tcp", p.address, p.tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("tls dial failed: %w", err)
	}

	if tcpConn, ok := conn.NetConn().(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	p.mu.Lock()
	p.totalCreated++
	p.mu.Unlock()

	return &PooledTLSConnection{
		conn:       conn,
		address:    p.address,
		lastUsed:   time.Now(),
		closed:     false,
		createdAt:  time.Now(),
		usageCount: 0,
	}, nil
}

// isTemporaryError 判断是否是临时错误
func (p *TLSConnectionPool) isTemporaryError(err error) bool {
	if err == nil {
		return false
	}

	if ne, ok := err.(net.Error); ok {
		return ne.Temporary() || ne.Timeout()
	}

	if err == context.DeadlineExceeded {
		return true
	}

	return false
}

// validateMessageSize 验证 DNS 消息大小
func (p *TLSConnectionPool) validateMessageSize(msgLen int) error {
	if msgLen <= 0 {
		return fmt.Errorf("invalid message length: %d (must be > 0)", msgLen)
	}

	if msgLen > MaxDNSMessageSize {
		return fmt.Errorf("message too large: %d > %d", msgLen, MaxDNSMessageSize)
	}

	if msgLen > WarnLargeMsgSize {
		logger.Warnf("[TLSConnectionPool] 大型 DNS 消息: %d 字节 (来自 %s)", msgLen, p.address)
	}

	return nil
}

// exchangeOnConnection 在给定 TLS 连接上执行 DNS 查询
func (p *TLSConnectionPool) exchangeOnConnection(ctx context.Context, poolConn *PooledTLSConnection, msg *dns.Msg) (*dns.Msg, error) {
	conn := poolConn.conn

	// 设置连接超时
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(p.readTimeout + p.writeTimeout))
	}

	buf, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack failed: %w", err)
	}

	// DoT 使用 TCP 格式：2 字节长度 + DNS 消息
	conn.SetWriteDeadline(time.Now().Add(p.writeTimeout))
	lenBuf := make([]byte, 2)
	lenBuf[0] = byte(len(buf) >> 8)
	lenBuf[1] = byte(len(buf))
	if _, err := conn.Write(lenBuf); err != nil {
		return nil, fmt.Errorf("write length failed: %w", err)
	}
	if _, err := conn.Write(buf); err != nil {
		return nil, fmt.Errorf("write body failed: %w", err)
	}

	// 接收响应：必须先读 2 字节长度
	conn.SetReadDeadline(time.Now().Add(p.readTimeout))
	respLenBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, respLenBuf); err != nil {
		return nil, fmt.Errorf("read resp length failed: %w", err)
	}
	msgLen := int(respLenBuf[0])<<8 | int(respLenBuf[1])

	if err := p.validateMessageSize(msgLen); err != nil {
		return nil, err
	}

	// 精确读取消息体，防止截断或读取过多
	conn.SetReadDeadline(time.Now().Add(p.readTimeout))
	resBuf := make([]byte, msgLen)
	if _, err := io.ReadFull(conn, resBuf); err != nil {
		return nil, fmt.Errorf("read resp body failed: %w", err)
	}

	// 清除超时
	conn.SetDeadline(time.Time{})

	reply := new(dns.Msg)
	if err := reply.Unpack(resBuf); err != nil {
		return nil, fmt.Errorf("unpack failed: %w", err)
	}

	return reply, nil
}

// cleanupLoop 定期清理空闲连接
func (p *TLSConnectionPool) cleanupLoop() {
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
				cleanupInterval = p.idleTimeout / 4
			} else if idleCount < 2 {
				cleanupInterval = p.idleTimeout / 2
			} else {
				cleanupInterval = p.idleTimeout / 3
			}

			ticker.Reset(cleanupInterval)
		}
	}
}

// adjustPoolSize 自动调整连接池大小
func (p *TLSConnectionPool) adjustPoolSize() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if time.Since(p.lastAdjustTime) < 60*time.Second {
		return
	}

	utilization := float64(p.activeCount) / float64(p.maxConnections)

	if utilization > 0.8 && p.maxConnections < MaxConnectionsLimit {
		newMax := min(p.maxConnections+5, MaxConnectionsLimit)
		logger.Debugf("[TLSConnectionPool] 自动扩容: %d -> %d (利用率: %.1f%%)", p.maxConnections, newMax, utilization*100)
		p.maxConnections = newMax
	}

	if utilization < 0.2 && p.maxConnections > p.minConnections {
		newMax := max(p.maxConnections-2, p.minConnections)
		logger.Debugf("[TLSConnectionPool] 自动缩容: %d -> %d (利用率: %.1f%%)", p.maxConnections, newMax, utilization*100)
		p.maxConnections = newMax
	}

	p.lastAdjustTime = time.Now()
}

func (p *TLSConnectionPool) cleanupExpiried() {
	count := len(p.idleConns)
	for i := 0; i < count; i++ {
		select {
		case conn := <-p.idleConns:
			if !conn.closed && time.Since(conn.lastUsed) < p.idleTimeout {
				p.idleConns <- conn
			} else {
				if !conn.closed {
					conn.conn.Close()
					conn.closed = true
				}
				p.mu.Lock()
				p.activeCount--
				p.totalDestroyed++
				p.mu.Unlock()
				logger.Debugf("[TLSConnectionPool] 清理过期的 TLS 连接: %s", p.address)
			}
		default:
			return
		}
	}
}

func (p *TLSConnectionPool) closeAll() {
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

func (p *TLSConnectionPool) Close() error {
	close(p.stopChan)
	p.wg.Wait()
	return nil
}

// Warmup 预热连接池
func (p *TLSConnectionPool) Warmup(ctx context.Context, count int) error {
	if count > p.maxConnections {
		count = p.maxConnections
	}

	for i := 0; i < count; i++ {
		conn, err := p.createConnection(ctx)
		if err != nil {
			logger.Warnf("[TLSConnectionPool] 预热失败: %v", err)
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

	logger.Debugf("[TLSConnectionPool] 预热完成: %s, 预热连接数: %d", p.address, count)
	return nil
}

// GetConnectionStats 获取单个连接的统计信息
func (p *TLSConnectionPool) GetConnectionStats() map[string]interface{} {
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
func (p *TLSConnectionPool) GetStats() map[string]interface{} {
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
