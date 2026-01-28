package upstream

import (
	"context"
	"fmt"
	"smartdnssort/logger"

	"github.com/miekg/dns"
)

// Query 是上游查询的统一入口，实现了请求去重（Deduplication）
// 相同的 (域名 + 类型 + DNSSEC状态) 在并发查询时会被合并为一次请求
func (u *Manager) Query(ctx context.Context, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error) {
	// 基础检查：如果消息为空，直接交给 rawQuery 处理（rawQuery 内部有完整的错误返回逻辑）
	if r == nil || len(r.Question) == 0 {
		return u.rawQuery(ctx, r, dnssec)
	}

	question := r.Question[0]
	domain := question.Name
	qtype := question.Qtype

	// 生成去重 Key：域名 + 类型 + DNSSEC 标志
	sfKey := fmt.Sprintf("up:%s:%d:%t", domain, qtype, dnssec)

	// 使用 any 替代 interface{}
	v, err, shared := u.requestGroup.Do(sfKey, func() (any, error) {
		return u.rawQuery(ctx, r, dnssec)
	})

	if shared {
		logger.Debugf("[Manager] 请求合并成功: %s (type=%s, dnssec=%t)", domain, dns.TypeToString[qtype], dnssec)
	}

	// 改进错误处理：先检查错误，再检查结果是否为 nil，最后进行类型断言
	if err != nil {
		return nil, err
	}

	if v == nil {
		return nil, fmt.Errorf("upstream query returned nil result without error")
	}

	result, ok := v.(*QueryResultWithTTL)
	if !ok {
		return nil, fmt.Errorf("unexpected type from singleflight: %T", v)
	}

	return result, nil
}
