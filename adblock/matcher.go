package adblock

import (
	"strings"
	"sync"
)

// Matcher 定义规则匹配器接口
type Matcher interface {
	// Match 检查域名是否匹配规则
	Match(domain string) (bool, string)
	// AddRule 添加规则
	AddRule(rule string)
	// Count 返回规则数量
	Count() int
}

// ExactMatcher 精确匹配器 (用于域名黑名单)
type ExactMatcher struct {
	rules map[string]struct{}
	mu    sync.RWMutex
}

// NewExactMatcher 创建一个新的精确匹配器
func NewExactMatcher() *ExactMatcher {
	return &ExactMatcher{
		rules: make(map[string]struct{}),
	}
}

// Match 检查域名是否在黑名单中
func (m *ExactMatcher) Match(domain string) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.rules[domain]; ok {
		return true, domain
	}
	return false, ""
}

// AddRule 添加一条精确匹配规则
func (m *ExactMatcher) AddRule(domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 存储时统一转小写，虽然 DNS 域名不区分大小写，但为了 map 查找一致性
	m.rules[strings.ToLower(domain)] = struct{}{}
}

// Count 返回规则数量
func (m *ExactMatcher) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rules)
}

// SuffixMatcher 后缀匹配器 (用于 ||example.com^ 类型的规则)
// 简单实现：检查域名是否以规则域名结尾
type SuffixMatcher struct {
	rules map[string]struct{}
	mu    sync.RWMutex
}

// NewSuffixMatcher 创建一个新的后缀匹配器
func NewSuffixMatcher() *SuffixMatcher {
	return &SuffixMatcher{
		rules: make(map[string]struct{}),
	}
}

// Match 检查域名是否匹配后缀规则
// 逻辑：如果规则是 example.com，那么 example.com 和 sub.example.com 都应该匹配
func (m *SuffixMatcher) Match(domain string) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 优化：直接遍历可能比较慢，但对于 map 查找，我们需要知道后缀
	// 正确的做法是：从域名的末尾开始，逐级向上查找
	// 例如查询 a.b.c.com，先查 a.b.c.com，再查 b.c.com，再查 c.com

	parts := strings.Split(domain, ".")
	for i := 0; i < len(parts); i++ {
		suffix := strings.Join(parts[i:], ".")
		if _, ok := m.rules[suffix]; ok {
			// 构造匹配到的规则形式返回
			return true, "||" + suffix + "^"
		}
	}

	return false, ""
}

// AddRule 添加一条后缀匹配规则
// 输入应该是纯域名部分，例如 "example.com" (来自 ||example.com^)
func (m *SuffixMatcher) AddRule(domain string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules[strings.ToLower(domain)] = struct{}{}
}

// Count 返回规则数量
func (m *SuffixMatcher) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rules)
}

// HostsMatcher Hosts 文件匹配器
// 格式: 127.0.0.1 example.com
type HostsMatcher struct {
	rules map[string]string // domain -> ip (虽然我们只关心是否拦截，但存一下 IP 也没坏处)
	mu    sync.RWMutex
}

// NewHostsMatcher 创建一个新的 Hosts 匹配器
func NewHostsMatcher() *HostsMatcher {
	return &HostsMatcher{
		rules: make(map[string]string),
	}
}

// Match 检查域名是否在 Hosts 列表中
func (m *HostsMatcher) Match(domain string) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if ip, ok := m.rules[domain]; ok {
		return true, ip + " " + domain
	}
	return false, ""
}

// AddRule 添加一条 Hosts 规则
func (m *HostsMatcher) AddRule(line string) {
	// 简单的解析逻辑，假设 line 已经被清洗过
	fields := strings.Fields(line)
	if len(fields) >= 2 {
		ip := fields[0]
		domain := fields[1]
		m.mu.Lock()
		defer m.mu.Unlock()
		m.rules[strings.ToLower(domain)] = ip
	}
}

// Count 返回规则数量
func (m *HostsMatcher) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rules)
}
