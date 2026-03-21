package cache

import (
	"strconv"
	"strings"
	"sync"
)

// builderPool 复用 strings.Builder 对象，避免频繁分配
var builderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

// cacheKey 生成缓存键，包含查询类型
// DNS 域名不区分大小写，统一转换为小写以避免重复缓存
func cacheKey(domain string, qtype uint16) string {
	return strings.ToLower(domain) + "#" + strconv.FormatUint(uint64(qtype), 10)
}

// parseCacheKey 解析缓存键，返回域名和查询类型
// 返回的域名已转换为小写
func parseCacheKey(key string) (string, uint16) {
	parts := strings.Split(key, "#")
	if len(parts) != 2 {
		return "", 0
	}
	domain := parts[0]
	qtype, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		return "", 0
	}
	return domain, uint16(qtype)
}
