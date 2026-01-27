package transport

import (
	"context"
	"net"
	"sync"
	"time"

	"smartdnssort/logger"

	"github.com/miekg/dns"
)

type UDP struct {
	address string
	pool    *ConnectionPool
	mu      sync.Mutex
}

func NewUDP(address string, maxConnections *int) *UDP {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "53")
	}

	// 创建连接池：使用传入的 maxConnections，如果为 nil 则传递 0 触发自动计算
	pool := NewConnectionPool(address, "udp", derefOrDefaultVal(maxConnections, 0), 5*time.Minute)

	return &UDP{
		address: address,
		pool:    pool,
	}
}

func (t *UDP) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	// [优化] EDNS0 自适应：限制 UDP Payload Size 为 1232 字节
	// 1232 是 IPv6 MTU (1280) 减去 IPv6 头部 (40) 和 UDP 头部 (8) 的安全值
	// 这能有效避免在双栈环境下因分片导致的丢包
	opt := msg.IsEdns0()
	if opt == nil {
		msg.SetEdns0(1232, false)
	} else if opt.UDPSize() > 1232 {
		opt.SetUDPSize(1232)
	}

	// 通过连接池执行查询
	reply, err := t.pool.Exchange(ctx, msg)
	if err != nil {
		logger.Debugf("[UDP] 查询失败: %v", err)
		return nil, err
	}

	return reply, nil
}

func (t *UDP) Address() string {
	return "udp://" + t.address
}

func (t *UDP) Protocol() string {
	return "udp"
}

// Close 关闭连接池
func (t *UDP) Close() error {
	return t.pool.Close()
}
