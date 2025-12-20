package main

import (
	"fmt"
	"smartdnssort/cache"
	"smartdnssort/config"
	"testing"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
)

// TestGenericRecordIntegration 测试通用记录的端到端处理
func TestGenericRecordIntegration(t *testing.T) {
	// 测试缓存功能
	t.Run("Generic Record Caching", func(t *testing.T) {
		// 创建缓存配置
		cacheConfig := &config.CacheConfig{
			FastResponseTTL: 15,
			UserReturnTTL:   60,
			ErrorCacheTTL:   300,
		}

		cache := cache.NewCache(cacheConfig)
		domain := "example.com"
		qtype := dns.TypeMX

		// 创建测试记录
		mx := &dns.MX{
			Hdr: dns.RR_Header{
				Name:   dns.Fqdn(domain),
				Rrtype: dns.TypeMX,
				Class:  dns.ClassINET,
				Ttl:    300,
			},
			Preference: 10,
			Mx:         "mail.example.com.",
		}

		records := []dns.RR{mx}
		cnames := []string{}
		ttl := uint32(300)

		// 存储到缓存
		cache.SetRawRecords(domain, qtype, records, cnames, ttl)

		// 从缓存读取
		cached, found := cache.GetRaw(domain, qtype)
		assert.True(t, found, "Should find cached record")
		assert.NotNil(t, cached, "Cached entry should not be nil")
		assert.Equal(t, 1, len(cached.Records), "Should have one record")

		// 验证记录内容
		cachedMX, ok := cached.Records[0].(*dns.MX)
		assert.True(t, ok, "Cached record should be MX type")
		assert.Equal(t, uint16(10), cachedMX.Preference, "MX preference should match")
		assert.Equal(t, "mail.example.com.", cachedMX.Mx, "MX target should match")

		fmt.Printf("Successfully cached and retrieved MX record\n")
	})
}
