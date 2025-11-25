package adblock

// Rule 表示一条广告拦截规则
type Rule struct {
	Pattern string // 规则模式字符串
	Raw     string // 原始规则文本
}

// BlockMode 定义拦截模式
type BlockMode string

const (
	BlockModeNXDomain BlockMode = "nxdomain" // 返回 NXDOMAIN
	BlockModeRefused  BlockMode = "refused"  // 返回 REFUSED
	BlockModeZeroIP   BlockMode = "zero_ip"  // 返回 0.0.0.0 或 ::
)

// MatchResult 匹配结果
type MatchResult struct {
	Matched bool   // 是否匹配
	Rule    string // 匹配的规则内容
}
