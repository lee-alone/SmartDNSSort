package upstream

import (
	"context"

	"github.com/miekg/dns"
)

// Upstream 定义了上游服务器的统一行为 (Transport Layer)
type Upstream interface {
	// Exchange 执行核心查询
	// context 用于控制超时和取消
	Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error)

	// Address 返回服务器的显示地址 (用于日志和调试)
	Address() string

	// Protocol 返回协议类型 (udp/tcp/dot/doh)
	Protocol() string
}
