package adblock

import (
	"regexp"
	"strings"
	"sync"
)

// RegexRule 存储编译后的正则规则
type RegexRule struct {
	Pattern *regexp.Regexp
	Raw     string
}

// SimpleFilter is a basic adblock filter engine.
type SimpleFilter struct {
	exactMatcher  *ExactMatcher
	suffixMatcher *SuffixMatcher
	hostsMatcher  *HostsMatcher
	regexRules    []RegexRule // 新增：正则规则列表
	mu            sync.RWMutex
}

// NewSimpleFilter creates a new SimpleFilter.
func NewSimpleFilter() *SimpleFilter {
	return &SimpleFilter{
		exactMatcher:  NewExactMatcher(),
		suffixMatcher: NewSuffixMatcher(),
		hostsMatcher:  NewHostsMatcher(),
	}
}

// CheckHost implements the FilterEngine interface.
func (f *SimpleFilter) CheckHost(domain string) (MatchResult, string) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	// 1. 检查精确匹配 (黑名单)
	if matched, rule := f.exactMatcher.Match(domain); matched {
		return MatchBlocked, "Blacklist: " + rule
	}

	// 2. 检查 Hosts 匹配
	if matched, rule := f.hostsMatcher.Match(domain); matched {
		return MatchBlocked, "Hosts: " + rule
	}

	// 3. 检查后缀匹配 (AdBlock ||example.com^)
	if matched, rule := f.suffixMatcher.Match(domain); matched {
		return MatchBlocked, "AdBlock: " + rule
	}

	// 4. 检查正则匹配 (新增)
	for _, rule := range f.regexRules {
		if rule.Pattern.MatchString(domain) {
			return MatchBlocked, "Regex: " + rule.Raw
		}
	}

	return MatchNeutral, ""
}

// LoadRules implements the FilterEngine interface.
// It parses rules and adds them to the appropriate matcher.
func (f *SimpleFilter) LoadRules(rules []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Reset matchers
	f.exactMatcher = NewExactMatcher()
	f.suffixMatcher = NewSuffixMatcher()
	f.hostsMatcher = NewHostsMatcher()
	f.regexRules = make([]RegexRule, 0)

	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" || strings.HasPrefix(rule, "!") || strings.HasPrefix(rule, "#") {
			continue // Skip empty lines and comments
		}

		// 正则表达式规则 /pattern/
		if strings.HasPrefix(rule, "/") && strings.HasSuffix(rule, "/") {
			pattern := rule[1 : len(rule)-1]
			// 强制不区分大小写
			if !strings.HasPrefix(pattern, "(?i)") {
				pattern = "(?i)" + pattern
			}
			if re, err := regexp.Compile(pattern); err == nil {
				f.regexRules = append(f.regexRules, RegexRule{Pattern: re, Raw: rule})
			}
			continue
		}

		// AdBlock Plus style rules
		if strings.HasPrefix(rule, "||") && strings.HasSuffix(rule, "^") {
			domainPart := strings.TrimPrefix(rule, "||")
			domainPart = strings.TrimSuffix(domainPart, "^")

			// 检查是否包含通配符 *
			if strings.Contains(domainPart, "*") {
				// 转换为正则
				// ||example*.com^ -> 匹配 example*.com 及其子域名
				// 逻辑：
				// 1. 转义点号 . -> \.
				// 2. 替换 * -> .*
				// 3. 处理 || 前缀：匹配字符串开始或点号
				// 4. 处理 ^ 后缀：匹配字符串结束或分隔符

				regexStr := regexp.QuoteMeta(domainPart)
				regexStr = strings.ReplaceAll(regexStr, "\\*", ".*")

				// 完整正则：(^|\.)domainPart($|[^a-zA-Z0-9_%.])
				// 但在 DNS 场景下，我们通常只匹配域名结束。
				// 简单起见，我们匹配后缀
				finalRegex := "(?i)(^|\\.)" + regexStr + "$"

				if re, err := regexp.Compile(finalRegex); err == nil {
					f.regexRules = append(f.regexRules, RegexRule{Pattern: re, Raw: rule})
				}
			} else {
				f.suffixMatcher.AddRule(domainPart)
			}
			continue
		}

		// Hosts file style rules
		if strings.Contains(rule, " ") || strings.Contains(rule, "\t") {
			parts := strings.Fields(rule)
			if len(parts) >= 2 {
				// Typically "127.0.0.1 domain.com" or "0.0.0.0 domain.com"
				// We just care about the domain part
				f.hostsMatcher.AddRule(rule)
				continue
			}
		}

		// Plain domain (exact match)
		f.exactMatcher.AddRule(rule)

	}
	return nil
}

// Count implements the FilterEngine interface.
func (f *SimpleFilter) Count() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.exactMatcher.Count() + f.suffixMatcher.Count() + f.hostsMatcher.Count() + len(f.regexRules)
}
