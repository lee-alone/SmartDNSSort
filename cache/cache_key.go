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
// 使用 strings.Builder 和 sync.Pool 优化字符串拼接，避免频繁内存分配
func cacheKey(domain string, qtype uint16) string {
	builder := builderPool.Get().(*strings.Builder)
	defer builderPool.Put(builder)
	builder.Reset()

	builder.WriteString(domain)
	builder.WriteByte('#')
	builder.WriteString(strconv.FormatUint(uint64(qtype), 10))

	return builder.String()
}
