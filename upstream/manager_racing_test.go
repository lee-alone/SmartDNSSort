package upstream

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestIsNetworkError 测试网络错误分类
func TestIsNetworkError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "connection refused",
			err:      fmt.Errorf("connection refused"),
			expected: true,
		},
		{
			name:     "connection reset",
			err:      fmt.Errorf("connection reset by peer"),
			expected: true,
		},
		{
			name:     "i/o timeout",
			err:      fmt.Errorf("i/o timeout"),
			expected: true,
		},
		{
			name:     "no such host",
			err:      fmt.Errorf("no such host"),
			expected: true,
		},
		{
			name:     "SERVFAIL (not network error)",
			err:      fmt.Errorf("dns query failed: rcode=2"),
			expected: false,
		},
		{
			name:     "net.Timeout error",
			err:      &timeoutError{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNetworkError(tt.err)
			if result != tt.expected {
				t.Errorf("isNetworkError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

// timeoutError 用于测试的超时错误
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return false }

// TestShouldSkipServerInRacing 测试服务器跳过逻辑
func TestShouldSkipServerInRacing(t *testing.T) {
	tests := []struct {
		name     string
		status   HealthStatus
		expected bool
	}{
		{
			name:     "healthy server",
			status:   HealthStatusHealthy,
			expected: false,
		},
		{
			name:     "degraded server",
			status:   HealthStatusDegraded,
			expected: false,
		},
		{
			name:     "unhealthy server",
			status:   HealthStatusUnhealthy,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health := NewServerHealth("test:53", DefaultHealthCheckConfig(), &StatsConfig{
				UpstreamStatsBucketMinutes: 10,
				UpstreamStatsRetentionDays: 90,
			})
			health.status = tt.status

			srv := &HealthAwareUpstream{
				health: health,
			}

			result := shouldSkipServerInRacing(srv)
			if result != tt.expected {
				t.Errorf("shouldSkipServerInRacing(status=%v) = %v, want %v", tt.status, result, tt.expected)
			}
		})
	}
}

// TestCalculateRacingBatchParams 测试动态批次参数计算
func TestCalculateRacingBatchParams(t *testing.T) {
	// 创建一个最小的Manager实例用于测试
	manager := &Manager{}

	tests := []struct {
		name              string
		remainingCount    int
		stdDev            time.Duration
		expectedBatchSize int
		expectedStagger   time.Duration
	}{
		{
			name:              "few servers, stable network",
			remainingCount:    2,
			stdDev:            10 * time.Millisecond,
			expectedBatchSize: 2,
			expectedStagger:   20 * time.Millisecond,
		},
		{
			name:              "few servers, jittery network",
			remainingCount:    2,
			stdDev:            60 * time.Millisecond,
			expectedBatchSize: 3,
			expectedStagger:   15 * time.Millisecond,
		},
		{
			name:              "many servers, stable network",
			remainingCount:    10,
			stdDev:            10 * time.Millisecond,
			expectedBatchSize: 3,
			expectedStagger:   20 * time.Millisecond,
		},
		{
			name:              "many servers, jittery network",
			remainingCount:    10,
			stdDev:            60 * time.Millisecond,
			expectedBatchSize: 4,
			expectedStagger:   15 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batchSize, stagger := manager.calculateRacingBatchParams(tt.remainingCount, tt.stdDev)

			if batchSize != tt.expectedBatchSize {
				t.Errorf("batchSize = %d, want %d", batchSize, tt.expectedBatchSize)
			}
			if stagger != tt.expectedStagger {
				t.Errorf("stagger = %v, want %v", stagger, tt.expectedStagger)
			}
		})
	}
}

// TestContains 测试字符串包含检查
func TestContains(t *testing.T) {
	tests := []struct {
		s      string
		substr string
		want   bool
	}{
		{"connection refused", "refused", true},
		{"Connection Refused", "refused", true},
		{"CONNECTION REFUSED", "refused", true},
		{"timeout", "timeout", true},
		{"no such host", "such", true},
		{"error message", "xyz", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s contains %s", tt.s, tt.substr), func(t *testing.T) {
			result := contains(tt.s, tt.substr)
			if result != tt.want {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.want)
			}
		})
	}
}

// TestToLower 测试字符转小写
func TestToLower(t *testing.T) {
	tests := []struct {
		b    byte
		want byte
	}{
		{'A', 'a'},
		{'Z', 'z'},
		{'a', 'a'},
		{'0', '0'},
		{'@', '@'},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("toLower(%c)", tt.b), func(t *testing.T) {
			result := toLower(tt.b)
			if result != tt.want {
				t.Errorf("toLower(%c) = %c, want %c", tt.b, result, tt.want)
			}
		})
	}
}

// MockUpstream 用于测试的模拟上游服务器
type MockUpstream struct {
	address string
	delay   time.Duration
	err     error
	reply   *dns.Msg
}

func (m *MockUpstream) Address() string {
	return m.address
}

func (m *MockUpstream) Protocol() string {
	return "udp"
}

func (m *MockUpstream) Exchange(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.err != nil {
		return nil, m.err
	}

	if m.reply != nil {
		return m.reply, nil
	}

	// 返回一个默认的成功响应
	reply := new(dns.Msg)
	reply.SetReply(msg)
	reply.Rcode = dns.RcodeSuccess
	return reply, nil
}

// TestRacingEarlyTrigger 测试错误抢跑机制
func TestRacingEarlyTrigger(t *testing.T) {
	// 创建模拟的上游服务器
	primary := &MockUpstream{
		address: "primary:53",
		delay:   100 * time.Millisecond,
		err:     fmt.Errorf("connection refused"),
	}

	secondary := &MockUpstream{
		address: "secondary:53",
		delay:   10 * time.Millisecond,
	}

	// 创建Manager
	manager := &Manager{
		servers: []*HealthAwareUpstream{
			NewHealthAwareUpstream(primary, DefaultHealthCheckConfig(), &StatsConfig{
				UpstreamStatsBucketMinutes: 10,
				UpstreamStatsRetentionDays: 90,
			}),
			NewHealthAwareUpstream(secondary, DefaultHealthCheckConfig(), &StatsConfig{
				UpstreamStatsBucketMinutes: 10,
				UpstreamStatsRetentionDays: 90,
			}),
		},
		strategy:            "racing",
		timeoutMs:           5000,
		racingDelayMs:       100,
		racingMaxConcurrent: 2,
		dynamicParamOptimization: &DynamicParamOptimization{
			ewmaAlpha:  0.2,
			maxStepMs:  10,
			avgLatency: 100 * time.Millisecond,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeA)

	startTime := time.Now()
	result, err := manager.queryRacing(ctx, "example.com", dns.TypeA, msg, false)
	elapsed := time.Since(startTime)

	// 由于主服务器立即报错，应该快速启动备选
	// 总耗时应该接近 secondary 的延迟 + 一些开销，而不是 primary 的延迟
	if elapsed > 200*time.Millisecond {
		t.Errorf("queryRacing took %v, expected < 200ms (early trigger should activate secondary quickly)", elapsed)
	}

	if err != nil {
		t.Logf("queryRacing returned error: %v (expected if secondary also fails)", err)
	}

	if result != nil {
		t.Logf("queryRacing returned result from secondary: %v", result)
	}
}
