package adblock

import (
	"strings"
	"sync"

	radix "github.com/hashicorp/go-immutable-radix"
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
// 使用 Radix Tree 实现：高效的后缀匹配
type SuffixMatcher struct {
	tree *radix.Tree
	mu   sync.Mutex // 仅用于保护写操作（更新 tree 指针）
}

// NewSuffixMatcher 创建一个新的后缀匹配器
func NewSuffixMatcher() *SuffixMatcher {
	return &SuffixMatcher{
		tree: radix.New(), // 初始化一个空的 Radix Tree
	}
}

// Match 检查域名是否匹配后缀规则
// 逻辑：如果规则是 example.com，那么 example.com 和 sub.example.com 都应该匹配
func (m *SuffixMatcher) Match(domain string) (bool, string) {
	// 转换为小写以确保大小写不敏感的匹配
	domain = strings.ToLower(domain)

	// 1. 颠倒待查询的域名 (e.g., "sub.example.com" -> "com.example.sub")
	parts := strings.Split(domain, ".")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	reversedDomain := strings.Join(parts, ".")

	// 2. 使用 Radix Tree 的 LongestPrefix 方法进行高效查找
	// LongestPrefix 会找到树中与 reversedDomain 拥有最长共同前缀的那个 key
	// 这正是我们需要的后缀匹配逻辑
	// 由于读取是并发安全的，这里不需要加锁
	_, _, found := m.tree.Root().LongestPrefix([]byte(reversedDomain))

	if found {
		// 找到了匹配
		return true, "||" + domain + "^"
	}

	return false, ""
}

// AddRule 添加一条后缀匹配规则
// 输入应该是纯域名部分，例如 "example.com" (来自 ||example.com^)
func (m *SuffixMatcher) AddRule(domain string) {
	// 转换为小写以确保统一处理
	domain = strings.ToLower(domain)

	// 1. 颠倒域名，以便进行前缀匹配 (e.g., "example.com" -> "com.example")
	parts := strings.Split(domain, ".")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	reversedDomain := strings.Join(parts, ".")

	// 2. 锁定并更新 Radix 树
	m.mu.Lock()
	defer m.mu.Unlock()

	// Insert 操作返回一个新的树，这是实现不可变性的关键
	// 为了简化存储，只存储 true 作为标记，避免不必要的字符串存储
	newTree, _, _ := m.tree.Insert([]byte(reversedDomain), true)
	m.tree = newTree // 原子地替换树的指针
}

// Count 返回规则数量
func (m *SuffixMatcher) Count() int {
	// Radix Tree 的 Len() 方法是线程安全的，无需加锁
	return m.tree.Len()
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
