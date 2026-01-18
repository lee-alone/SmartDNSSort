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
func cacheKey(domain string, qtype uint16) string {
	return domain + "#" + strconv.FormatUint(uint64(qtype), 10)
}
