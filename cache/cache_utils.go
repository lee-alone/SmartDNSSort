package cache

import (
	"strings"
)

// extractDomain 从缓存键中提取域名
func (c *Cache) extractDomain(key string) string {
	parts := strings.Split(key, "#")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// isProtectedDomain 检查域名是否受保护（例如，是热点域名）
func (c *Cache) isProtectedDomain(domain string) bool {
	if c.prefetcher == nil || !c.config.ProtectPrefetchDomains {
		return false
	}
	return c.prefetcher.IsTopDomain(domain)
}
