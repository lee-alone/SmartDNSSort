package resolver

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestStatsHandlerGetStatsJSON 测试获取JSON格式的统计数据
func TestStatsHandlerGetStatsJSON(t *testing.T) {
	stats := NewStats()
	handler := NewStatsHandler(stats)

	// 记录一些查询
	stats.RecordQuery(100*time.Millisecond, true)
	stats.RecordQuery(200*time.Millisecond, true)
	stats.RecordQuery(150*time.Millisecond, false)
	stats.RecordCacheHit()
	stats.RecordCacheHit()
	stats.RecordCacheMiss()

	// 获取JSON数据
	jsonData := handler.GetStatsJSON()

	// 验证JSON格式
	var statsMap map[string]interface{}
	err := json.Unmarshal([]byte(jsonData), &statsMap)
	if err != nil {
		t.Fatalf("Failed to unmarshal stats JSON: %v", err)
	}

	// 验证关键字段
	if totalQueries, ok := statsMap["total_queries"].(float64); !ok || totalQueries != 3 {
		t.Errorf("Expected total_queries=3, got %v", statsMap["total_queries"])
	}

	if successQueries, ok := statsMap["success_queries"].(float64); !ok || successQueries != 2 {
		t.Errorf("Expected success_queries=2, got %v", statsMap["success_queries"])
	}

	if failedQueries, ok := statsMap["failed_queries"].(float64); !ok || failedQueries != 1 {
		t.Errorf("Expected failed_queries=1, got %v", statsMap["failed_queries"])
	}

	if cacheHits, ok := statsMap["cache_hits"].(float64); !ok || cacheHits != 2 {
		t.Errorf("Expected cache_hits=2, got %v", statsMap["cache_hits"])
	}

	if cacheMisses, ok := statsMap["cache_misses"].(float64); !ok || cacheMisses != 1 {
		t.Errorf("Expected cache_misses=1, got %v", statsMap["cache_misses"])
	}

	// 验证计算字段存在
	if _, ok := statsMap["success_rate"]; !ok {
		t.Errorf("Expected success_rate in stats")
	}

	if _, ok := statsMap["cache_hit_rate"]; !ok {
		t.Errorf("Expected cache_hit_rate in stats")
	}
}

// TestStatsHandlerSplitJSON 测试JSON分割
func TestStatsHandlerSplitJSON(t *testing.T) {
	stats := NewStats()
	handler := NewStatsHandler(stats)

	// 创建一个长的JSON字符串
	longData := strings.Repeat("a", 1000)

	segments := handler.splitJSON(longData, 255)

	// 验证分割结果
	if len(segments) != 4 {
		t.Errorf("Expected 4 segments, got %d", len(segments))
	}

	// 验证每个段的长度
	for i, segment := range segments {
		if i < len(segments)-1 {
			if len(segment) != 255 {
				t.Errorf("Expected segment %d to have length 255, got %d", i, len(segment))
			}
		} else {
			if len(segment) != 235 {
				t.Errorf("Expected last segment to have length 235, got %d", len(segment))
			}
		}
	}

	// 验证重新组合后的数据
	combined := strings.Join(segments, "")
	if combined != longData {
		t.Errorf("Combined data does not match original")
	}
}

// TestStatsHandlerHandleStatsQuery 测试处理统计查询
func TestStatsHandlerHandleStatsQuery(t *testing.T) {
	stats := NewStats()
	handler := NewStatsHandler(stats)

	// 记录一些查询
	stats.RecordQuery(100*time.Millisecond, true)
	stats.RecordCacheHit()

	// 创建DNS查询消息
	m := new(dns.Msg)
	m.SetQuestion("stats.resolver.local.", dns.TypeTXT)

	// 创建一个模拟的ResponseWriter
	w := &mockResponseWriter{
		msg: nil,
	}

	// 处理查询
	handler.HandleStatsQuery(w, m)

	// 验证响应
	if w.msg == nil {
		t.Fatalf("Expected response message, got nil")
	}

	if len(w.msg.Answer) == 0 {
		t.Fatalf("Expected answer records, got none")
	}

	// 验证答案是TXT记录
	for _, rr := range w.msg.Answer {
		if txt, ok := rr.(*dns.TXT); !ok {
			t.Errorf("Expected TXT record, got %T", rr)
		} else {
			if len(txt.Txt) == 0 {
				t.Errorf("Expected TXT data, got empty")
			}
		}
	}
}

// TestIsStatsQuery 测试统计查询检测
func TestIsStatsQuery(t *testing.T) {
	tests := []struct {
		name     string
		qname    string
		qtype    uint16
		expected bool
	}{
		{
			name:     "Valid stats query",
			qname:    "stats.resolver.local.",
			qtype:    dns.TypeTXT,
			expected: true,
		},
		{
			name:     "Wrong QNAME",
			qname:    "example.com.",
			qtype:    dns.TypeTXT,
			expected: false,
		},
		{
			name:     "Wrong QTYPE",
			qname:    "stats.resolver.local.",
			qtype:    dns.TypeA,
			expected: false,
		},
		{
			name:     "Empty question",
			qname:    "",
			qtype:    dns.TypeTXT,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := new(dns.Msg)
			if tt.qname != "" {
				m.SetQuestion(tt.qname, tt.qtype)
			}

			result := IsStatsQuery(m)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// mockResponseWriter 模拟DNS响应写入器
type mockResponseWriter struct {
	msg *dns.Msg
}

func (w *mockResponseWriter) WriteMsg(m *dns.Msg) error {
	w.msg = m
	return nil
}

func (w *mockResponseWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (w *mockResponseWriter) LocalAddr() net.Addr {
	return nil
}

func (w *mockResponseWriter) RemoteAddr() net.Addr {
	return nil
}

func (w *mockResponseWriter) TsigStatus() error {
	return nil
}

func (w *mockResponseWriter) TsigTimersOnly(b bool) {
}

func (w *mockResponseWriter) Hijack() {
}

func (w *mockResponseWriter) Close() error {
	return nil
}
