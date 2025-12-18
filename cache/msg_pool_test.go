package cache

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestMsgPoolGet(t *testing.T) {
	pool := NewMsgPool()

	// 获取第一个对象
	msg1 := pool.Get()
	if msg1 == nil {
		t.Fatal("Expected non-nil msg from pool")
	}

	// 获取第二个对象（应该是新创建的）
	msg2 := pool.Get()
	if msg2 == nil {
		t.Fatal("Expected non-nil msg from pool")
	}

	// 两个对象应该不同
	if msg1 == msg2 {
		t.Fatal("Expected different msg objects")
	}
}

func TestMsgPoolPut(t *testing.T) {
	pool := NewMsgPool()

	// 创建并填充一个消息
	msg := pool.Get()
	msg.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)
	msg.Answer = append(msg.Answer, &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn("example.com"),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: net.IPv4(1, 2, 3, 4),
	})

	// 验证消息有内容
	if len(msg.Question) == 0 || len(msg.Answer) == 0 {
		t.Fatal("Expected msg to have question and answer")
	}

	// 放回池中
	pool.Put(msg)

	// 从池中获取（应该是同一个对象，但已重置）
	msg2 := pool.Get()
	if len(msg2.Question) != 0 || len(msg2.Answer) != 0 {
		t.Fatal("Expected msg to be reset after Put")
	}
}

func TestMsgPoolReset(t *testing.T) {
	pool := NewMsgPool()

	msg := pool.Get()
	msg.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)
	msg.Answer = append(msg.Answer, &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn("example.com"),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: net.IPv4(1, 2, 3, 4),
	})

	// 验证消息有内容
	if len(msg.Question) == 0 || len(msg.Answer) == 0 {
		t.Fatal("Expected msg to have question and answer")
	}

	// 重置消息
	pool.Reset(msg)

	// 验证消息已重置
	if len(msg.Question) != 0 || len(msg.Answer) != 0 {
		t.Fatal("Expected msg to be reset")
	}

	// 验证容量被控制
	if cap(msg.Question) > 4 {
		t.Fatal("Expected question capacity to be controlled")
	}
	if cap(msg.Answer) > 8 {
		t.Fatal("Expected answer capacity to be controlled")
	}
}

func TestMsgPoolPutNil(t *testing.T) {
	pool := NewMsgPool()

	// 不应该 panic
	pool.Put(nil)
}

func TestMsgPoolResetNil(t *testing.T) {
	pool := NewMsgPool()

	// 不应该 panic
	pool.Reset(nil)
}

func TestMsgPoolReuse(t *testing.T) {
	pool := NewMsgPool()

	// 第一次使用
	msg1 := pool.Get()
	msg1.SetQuestion(dns.Fqdn("example.com"), dns.TypeA)
	msg1Ptr := msg1

	pool.Put(msg1)

	// 第二次使用（应该复用同一个对象）
	msg2 := pool.Get()
	if msg2 != msg1Ptr {
		t.Fatal("Expected msg to be reused from pool")
	}

	// 验证对象已重置
	if len(msg2.Question) != 0 {
		t.Fatal("Expected msg to be reset")
	}
}

func TestMsgPoolMultiplePutGet(t *testing.T) {
	pool := NewMsgPool()

	// 获取多个对象
	msgs := make([]*dns.Msg, 5)
	for i := 0; i < 5; i++ {
		msgs[i] = pool.Get()
		msgs[i].SetQuestion(dns.Fqdn("example.com"), dns.TypeA)
	}

	// 放回所有对象
	for i := 0; i < 5; i++ {
		pool.Put(msgs[i])
	}

	// 再次获取，应该能复用之前的对象
	for i := 0; i < 5; i++ {
		msg := pool.Get()
		if len(msg.Question) != 0 {
			t.Fatal("Expected msg to be reset")
		}
	}
}

func TestMsgPoolCapacityControl(t *testing.T) {
	pool := NewMsgPool()

	msg := pool.Get()

	// 添加大量 RR 记录，超过容量阈值
	for i := 0; i < 20; i++ {
		msg.Answer = append(msg.Answer, &dns.A{
			Hdr: dns.RR_Header{
				Name:   dns.Fqdn("example.com"),
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    300,
			},
			A: net.IPv4(1, 2, 3, byte(i)),
		})
	}

	// 验证容量已增长
	if cap(msg.Answer) < 20 {
		t.Fatal("Expected answer capacity to grow")
	}

	// 放回对象
	pool.Put(msg)

	// 再次获取，容量应该被控制
	msg2 := pool.Get()
	if cap(msg2.Answer) > 8 {
		t.Fatal("Expected answer capacity to be controlled after reset")
	}
	if len(msg2.Answer) != 0 {
		t.Fatal("Expected answer to be empty after reset")
	}
}

func TestMsgPoolEDNSCleanup(t *testing.T) {
	pool := NewMsgPool()

	msg := pool.Get()

	// 添加 EDNS 选项
	opt := &dns.OPT{
		Hdr: dns.RR_Header{
			Name:   ".",
			Rrtype: dns.TypeOPT,
		},
	}
	msg.Extra = append(msg.Extra, opt)

	// 验证 EDNS 存在
	if msg.IsEdns0() == nil {
		t.Fatal("Expected EDNS to be set")
	}

	// 验证 Extra 不为空
	if len(msg.Extra) == 0 {
		t.Fatal("Expected Extra to have OPT record")
	}

	// 放回对象
	pool.Put(msg)

	// 再次获取，Extra 应该被清理（包括 EDNS）
	msg2 := pool.Get()
	if len(msg2.Extra) != 0 {
		t.Fatal("Expected Extra to be empty after reset")
	}
	if msg2.IsEdns0() != nil {
		t.Fatal("Expected EDNS to be cleaned after reset")
	}
}
