package adblock

// MatchResult is the result of a host check.
type MatchResult int

const (
	MatchNeutral MatchResult = iota
	MatchBlocked
	MatchAllowed
)

// FilterEngine is the interface for adblock filter engines.
type FilterEngine interface {
	CheckHost(domain string) (result MatchResult, rule string)
	LoadRules(rules []string) error
	Count() int
}
