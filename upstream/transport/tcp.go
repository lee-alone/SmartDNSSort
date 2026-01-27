package transport

import (
	"context"
	"net"
	"sync"
	"time"

	"smartdnssort/logger"

	"github.com/miekg/dns"
)

type TCP struct {
	address string
	pool    *ConnectionPool
	mu      sync.Mutex
}

func NewTCP(address string) *TCP {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "53")
	}

	// 创建连接池：最多 10 个并发连接，空闲超时 5 分钟
	pool := NewConnectionPool(address, "tcp", 10, 5*time.Minute)

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
