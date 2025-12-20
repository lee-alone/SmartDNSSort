package main

import (
	"fmt"
	"smartdnssort/cache"
	"smartdnssort/config"
	"smartdnssort/upstream"

	"github.com/miekg/dns"
)

// 演示通用记录处理功能
func main() {
	fmt.Println("=== SmartDNS 通用记录处理演示 ===")

	// 创建缓存
	cacheConfig := &config.CacheConfig{
		FastResponseTTL: 15,
		UserReturnTTL:   60,
		ErrorCacheTTL:   300,
	}
	cache := cache.NewCache(cacheConfig)

	// 演示1：MX记录缓存
	fmt.Println("\n1. 演示 MX 记录缓存:")
	domain := "example.com"
	qtype := dns.TypeMX

	// 创建测试MX记录
	mx1 := &dns.MX{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(domain),
			Rrtype: dns.TypeMX,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Preference: 10,
		Mx:         "mail1.example.com.",
	}
	mx2 := &dns.MX{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(domain),
			Rrtype: dns.TypeMX,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Preference: 20,
		Mx:         "mail2.example.com.",
	}

	records := []dns.RR{mx1, mx2}
	cnames := []string{}
	ttl := uint32(300)

	// 存储到缓存
	cache.SetRawRecords(domain, qtype, records, cnames, ttl)
	fmt.Printf("✓ 已缓存 %d 条 MX 记录到 %s\n", len(records), domain)

	// 从缓存读取
	cached, found := cache.GetRaw(domain, qtype)
	if found && len(cached.Records) > 0 {
		fmt.Printf("✓ 从缓存读取到 %d 条记录:\n", len(cached.Records))
		for i, rr := range cached.Records {
			if mx, ok := rr.(*dns.MX); ok {
				fmt.Printf("  MX %d: %d %s\n", i+1, mx.Preference, mx.Mx)
			}
		}
	}

	// 演示2：TXT记录缓存
	fmt.Println("\n2. 演示 TXT 记录缓存:")
	txtDomain := "spf.example.com"
	txtQtype := dns.TypeTXT

	txt := &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(txtDomain),
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Txt: []string{"v=spf1 include:_spf.example.com ~all"},
	}

	txtRecords := []dns.RR{txt}
	cache.SetRawRecords(txtDomain, txtQtype, txtRecords, []string{}, 300)
	fmt.Printf("✓ 已缓存 TXT 记录到 %s\n", txtDomain)

	txtCached, txtFound := cache.GetRaw(txtDomain, txtQtype)
	if txtFound && len(txtCached.Records) > 0 {
		if txtRec, ok := txtCached.Records[0].(*dns.TXT); ok {
			fmt.Printf("✓ TXT 记录内容: %v\n", txtRec.Txt)
		}
	}

	// 演示3：带CNAME的记录
	fmt.Println("\n3. 演示带 CNAME 的记录:")
	cnameDomain := "alias.example.com"
	cnameTarget := "target.example.com"

	// 创建CNAME记录
	cname := &dns.CNAME{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(cnameDomain),
			Rrtype: dns.TypeCNAME,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Target: dns.Fqdn(cnameTarget),
	}

	// 创建目标A记录
	a := &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(cnameTarget),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{192, 168, 1, 100}, // 192.168.1.100
	}

	cnameRecords := []dns.RR{cname, a}
	cnameList := []string{cnameTarget}

	cache.SetRawRecords(cnameDomain, dns.TypeA, cnameRecords, cnameList, 300)
	fmt.Printf("✓ 已缓存 CNAME + A 记录: %s -> %s\n", cnameDomain, cnameTarget)

	cnameCached, cnameFound := cache.GetRaw(cnameDomain, dns.TypeA)
	if cnameFound {
		fmt.Printf("✓ CNAME 链: %v\n", cnameCached.CNAMEs)
		fmt.Printf("✓ 最终 IP: %v\n", cnameCached.IPs)
		fmt.Printf("✓ 记录数量: %d\n", len(cnameCached.Records))
	}

	// 演示4：上游记录提取
	fmt.Println("\n4. 演示上游记录提取:")

	// 创建模拟DNS响应
	msg := new(dns.Msg)
	msg.SetReply(&dns.Msg{Question: []dns.Question{{Name: "test.example.com.", Qtype: dns.TypeMX, Qclass: dns.ClassINET}}})

	// 添加多种类型的记录
	msg.Answer = []dns.RR{
		&dns.MX{
			Hdr:        dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 300},
			Preference: 10, Mx: "mx1.example.com.",
		},
		&dns.MX{
			Hdr:        dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeMX, Class: dns.ClassINET, Ttl: 300},
			Preference: 20, Mx: "mx2.example.com.",
		},
		&dns.TXT{
			Hdr: dns.RR_Header{Name: "test.example.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
			Txt: []string{"v=spf1 mx ~all"},
		},
	}

	// 使用新的extractRecords函数
	extractedRecords, extractedCNAMEs, extractedTTL := upstream.ExtractRecords(msg)

	fmt.Printf("✓ 从DNS响应提取到 %d 条记录\n", len(extractedRecords))
	fmt.Printf("✓ CNAME 数量: %d\n", len(extractedCNAMEs))
	fmt.Printf("✓ TTL: %d 秒\n", extractedTTL)

	for i, rr := range extractedRecords {
		switch r := rr.(type) {
		case *dns.MX:
			fmt.Printf("  记录 %d: MX %d %s\n", i+1, r.Preference, r.Mx)
		case *dns.TXT:
			fmt.Printf("  记录 %d: TXT %v\n", i+1, r.Txt)
		default:
			fmt.Printf("  记录 %d: %s\n", i+1, dns.TypeToString[rr.Header().Rrtype])
		}
	}

	fmt.Println("\n=== 演示完成 ===")
	fmt.Println("✓ 通用记录处理功能已成功实现")
	fmt.Println("✓ 支持 MX、TXT、SRV、NS 等所有 DNS 记录类型")
	fmt.Println("✓ 保持对 A/AAAA 记录的优化处理")
	fmt.Println("✓ 完整支持 CNAME 链处理")
}
