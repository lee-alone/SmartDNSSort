package cache

import (
	"sync"

	"github.com/miekg/dns"
)

// MsgPool 提供 dns.Msg 对象的复用机制，减少内存分配和 GC 压力
type MsgPool struct {
	pool *sync.Pool
}

// NewMsgPool 创建一个新的 dns.Msg 对象池
func NewMsgPool() *MsgPool {
	return &MsgPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return &dns.Msg{}
			},
		},
	}
}

// Get 从池中获取一个 dns.Msg 对象
// 如果池中没有可用对象，会创建一个新的
func (mp *MsgPool) Get() *dns.Msg {
	msg := mp.pool.Get().(*dns.Msg)
	return msg
}

// Put 将 dns.Msg 对象放回池中
// 在放回前会重置对象的所有字段，确保下次使用时是干净的状态
func (mp *MsgPool) Put(msg *dns.Msg) {
	if msg == nil {
		return
	}

	mp.reset(msg)
	mp.pool.Put(msg)
}

// Reset 重置 dns.Msg 对象的所有字段
// 这个方法可以在不放回池中的情况下重置对象
func (mp *MsgPool) Reset(msg *dns.Msg) {
	if msg == nil {
		return
	}

	mp.reset(msg)
}

// reset 内部方法，执行实际的重置逻辑
// 包含容量控制和 EDNS 清理，确保对象完全干净
func (mp *MsgPool) reset(msg *dns.Msg) {
	// 重置消息头
	msg.MsgHdr = dns.MsgHdr{}
	msg.Compress = false

	// 重置 RR 切片，带容量控制
	// 如果容量超过阈值，重新分配更小的切片以节省内存
	resetRR := func(rrs *[]dns.RR) {
		if cap(*rrs) > 8 {
			*rrs = make([]dns.RR, 0, 8)
		} else {
			*rrs = (*rrs)[:0]
		}
	}

	resetRR(&msg.Answer)
	resetRR(&msg.Ns)
	resetRR(&msg.Extra)

	// 重置 Question 切片，带容量控制
	if cap(msg.Question) > 4 {
		msg.Question = make([]dns.Question, 0, 4)
	} else {
		msg.Question = msg.Question[:0]
	}
}
