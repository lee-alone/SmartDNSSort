package transport

import (
	"context"
	"net"
	"sync"
	"time"

	"smartdnssort/logger"

	"github.com/miekg/dns"
)

type DoT struct {
	address    string
	serverName string
	pool       *TLSConnectionPool
	mu         sync.Mutex
}

func NewDoT(addr string) *DoT {
	// addr might be "tls://dns.google:853" or just "dns.google:853"
	// We need to parse it to get hostname for SNI

	host, port, _ := net.SplitHostPort(addr)

	if port == "" {
		port = "853"
	}

	// If host is empty (e.g. parse error), try to use original addr as host
	if host == "" {
		host = addr
	}

	address := net.JoinHostPort(host, port)

	// 创建 TLS 连接池：最多 10 个并发连接，空闲超时 5 分钟
	pool := NewTLSConnectionPool(address, host, 10, 5*time.Minute)

	return &DoT{
		address:    address,
		serverName: host,
		pool:       pool,
	}
}

func (t *DoT) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	// 通过 TLS 连接池执行查询
	reply, err := t.pool.Exchange(ctx, msg)
	if err != nil {
		logger.Debugf("[DoT] 查询失败: %v", err)
		return nil, err
	}

	return reply, nil
}

func (t *DoT) Address() string {
	return "tls://" + t.address
}

func (t *DoT) Protocol() string {
	return "dot"
}

// Close 关闭连接池
func (t *DoT) Close() error {
	return t.pool.Close()
}
