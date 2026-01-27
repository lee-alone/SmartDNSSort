package transport

import (
	"context"
	"net"
	"smartdnssort/logger"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type TCP struct {
	address string
	pool    *ConnectionPool
	mu      sync.Mutex
}

func NewTCP(address string, maxConnections *int) *TCP {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "53")
	}

	// 创建连接池：使用传入的 maxConnections，如果为 nil 则传递 0 触发自动计算
	pool := NewConnectionPool(address, "tcp", derefOrDefaultVal(maxConnections, 0), 5*time.Minute)

	return &TCP{
		address: address,
		pool:    pool,
	}
}

func (t *TCP) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	// 通过连接池执行查询
	reply, err := t.pool.Exchange(ctx, msg)
	if err != nil {
		logger.Debugf("[TCP] 查询失败: %v", err)
		return nil, err
	}

	return reply, nil
}

func (t *TCP) Address() string {
	return "tcp://" + t.address
}

func (t *TCP) Protocol() string {
	return "tcp"
}

// Close 关闭连接池
func (t *TCP) Close() error {
	return t.pool.Close()
}
