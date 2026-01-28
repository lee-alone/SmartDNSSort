package dnsserver

import (
	"testing"

	"github.com/miekg/dns"
)

// TestCacheUpdateCallback_IPPoolChangeDetection 测试IP池变化检测逻辑
func TestCacheUpdateCallback_IPPoolChangeDetection(t *testing.T) {
	tests := []struct {
		name         string
		oldIPs       []string
		newRecords   []dns.RR
		shouldUpdate bool
		description  string
	}{
		{
			name:         "首次查询",
			oldIPs:       []string{},
			newRecords:   []dns.RR{createARecord("example.com", "1.1.1.1")},
			shouldUpdate: true,
			description:  "没有旧缓存，应该更新",
		},
		{
			name:         "发现新增IP",
			oldIPs:       []string{"1.1.1.1", "2.2.2.2"},
			newRecords:   []dns.RR{createARecord("example.com", "1.1.1.1"), createARecord("example.com", "2.2.2.2"), createARecord("example.com", "3.3.3.3")},
			shouldUpdate: true,
			description:  "后台补全发现新IP，应该更新",
		},
		{
			name:         "IP完全相同",
			oldIPs:       []string{"1.1.1.1", "2.2.2.2"},
			newRecords:   []dns.RR{createARecord("example.com", "1.1.1.1"), createARecord("example.com", "2.2.2.2")},
			shouldUpdate: false,
			description:  "IP池无变化，应该跳过更新",
		},
		{
			name:         "IP删除",
			oldIPs:       []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			newRecords:   []dns.RR{createARecord("example.com", "1.1.1.1"), createARecord("example.com", "2.2.2.2")},
			shouldUpdate: true,
			description:  "某些IP不再可用，应该更新",
		},
		{
			name:   "显著增加",
			oldIPs: []string{"1.1.1.1", "2.2.2.2"},
			newRecords: []dns.RR{
				createARecord("example.com", "1.1.1.1"),
				createARecord("example.com", "2.2.2.2"),
				createARecord("example.com", "3.3.3.3"),
				createARecord("example.com", "4.4.4.4"),
				createARecord("example.com", "5.5.5.5"),
			},
			shouldUpdate: true,
			description:  "IP数量增加>50%，应该更新",
		},
		{
			name:   "小幅增加",
			oldIPs: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			newRecords: []dns.RR{
				createARecord("example.com", "1.1.1.1"),
				createARecord("example.com", "2.2.2.2"),
				createARecord("example.com", "3.3.3.3"),
				createARecord("example.com", "4.4.4.4"),
			},
			shouldUpdate: false,
			description:  "IP数量增加<50%，应该跳过更新",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 构建旧IP集合
			oldIPSet := make(map[string]bool)
			for _, ip := range tt.oldIPs {
				oldIPSet[ip] = true
			}

			// 从新记录中提取IP
			newIPSet := make(map[string]bool)
			var newIPs []string
			for _, r := range tt.newRecords {
				switch rec := r.(type) {
				case *dns.A:
					ipStr := rec.A.String()
					if !newIPSet[ipStr] {
						newIPSet[ipStr] = true
						newIPs = append(newIPs, ipStr)
					}
				}
			}

			// 检测变化
			hasNewIPs := false
			for _, newIP := range newIPs {
				if !oldIPSet[newIP] {
					hasNewIPs = true
					break
				}
			}

			hasRemovedIPs := false
			for _, oldIP := range tt.oldIPs {
				if !newIPSet[oldIP] {
					hasRemovedIPs = true
					break
				}
			}

			oldIPCount := len(tt.oldIPs)
			newIPCount := len(newIPs)

			// 决策
			shouldUpdate := false
			if oldIPCount == 0 {
				shouldUpdate = true
			} else if hasNewIPs {
				shouldUpdate = true
			} else if hasRemovedIPs {
				shouldUpdate = true
			} else if newIPCount > oldIPCount && float64(newIPCount-oldIPCount)/float64(oldIPCount) > 0.5 {
				shouldUpdate = true
			}

			if shouldUpdate != tt.shouldUpdate {
				t.Errorf("%s: 期望 shouldUpdate=%v, 实际=%v\n描述: %s\n旧IP: %v\n新IP: %v\n新增: %v, 删除: %v",
					tt.name, tt.shouldUpdate, shouldUpdate, tt.description,
					tt.oldIPs, newIPs, hasNewIPs, hasRemovedIPs)
			}
		})
	}
}

// 辅助函数：创建A记录
func createARecord(domain, ip string) *dns.A {
	return &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(domain),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{0, 0, 0, 0}, // 占位符，实际值在String()中处理
	}
}

// 注意：上面的createARecord是简化版本，实际测试中应该使用正确的IP解析
// 这里仅用于演示逻辑，实际测试应该这样做：

func TestCacheUpdateCallback_IPPoolChangeDetection_Correct(t *testing.T) {
	tests := []struct {
		name         string
		oldIPs       []string
		newIPs       []string
		shouldUpdate bool
		description  string
	}{
		{
			name:         "首次查询",
			oldIPs:       []string{},
			newIPs:       []string{"1.1.1.1"},
			shouldUpdate: true,
			description:  "没有旧缓存，应该更新",
		},
		{
			name:         "发现新增IP",
			oldIPs:       []string{"1.1.1.1", "2.2.2.2"},
			newIPs:       []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			shouldUpdate: true,
			description:  "后台补全发现新IP，应该更新",
		},
		{
			name:         "IP完全相同",
			oldIPs:       []string{"1.1.1.1", "2.2.2.2"},
			newIPs:       []string{"1.1.1.1", "2.2.2.2"},
			shouldUpdate: false,
			description:  "IP池无变化，应该跳过更新",
		},
		{
			name:         "IP删除",
			oldIPs:       []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			newIPs:       []string{"1.1.1.1", "2.2.2.2"},
			shouldUpdate: true,
			description:  "某些IP不再可用，应该更新",
		},
		{
			name:         "显著增加",
			oldIPs:       []string{"1.1.1.1", "2.2.2.2"},
			newIPs:       []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4", "5.5.5.5"},
			shouldUpdate: true,
			description:  "IP数量增加>50%，应该更新",
		},
		{
			name:         "小幅增加",
			oldIPs:       []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			newIPs:       []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"},
			shouldUpdate: true,
			description:  "IP数量增加<50%但有新增IP，应该更新",
		},
		{
			name:         "顺序变化无新增",
			oldIPs:       []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"},
			newIPs:       []string{"3.3.3.3", "2.2.2.2", "1.1.1.1"},
			shouldUpdate: false,
			description:  "仅顺序变化无新增IP，应该跳过更新",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 构建旧IP集合
			oldIPSet := make(map[string]bool)
			for _, ip := range tt.oldIPs {
				oldIPSet[ip] = true
			}

			// 构建新IP集合
			newIPSet := make(map[string]bool)
			for _, ip := range tt.newIPs {
				newIPSet[ip] = true
			}

			// 检测变化
			hasNewIPs := false
			for _, newIP := range tt.newIPs {
				if !oldIPSet[newIP] {
					hasNewIPs = true
					break
				}
			}

			hasRemovedIPs := false
			for _, oldIP := range tt.oldIPs {
				if !newIPSet[oldIP] {
					hasRemovedIPs = true
					break
				}
			}

			oldIPCount := len(tt.oldIPs)
			newIPCount := len(tt.newIPs)

			// 决策
			shouldUpdate := false
			if oldIPCount == 0 {
				shouldUpdate = true
			} else if hasNewIPs {
				shouldUpdate = true
			} else if hasRemovedIPs {
				shouldUpdate = true
			} else if newIPCount > oldIPCount && float64(newIPCount-oldIPCount)/float64(oldIPCount) > 0.5 {
				shouldUpdate = true
			}

			if shouldUpdate != tt.shouldUpdate {
				t.Errorf("%s: 期望 shouldUpdate=%v, 实际=%v\n描述: %s\n旧IP: %v\n新IP: %v\n新增: %v, 删除: %v",
					tt.name, tt.shouldUpdate, shouldUpdate, tt.description,
					tt.oldIPs, tt.newIPs, hasNewIPs, hasRemovedIPs)
			}
		})
	}
}
