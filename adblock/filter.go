package adblock

// FilterEngine is the interface for adblock filter engines.
type FilterEngine interface {
	CheckHost(domain string) (blocked bool, rule string)
	LoadRules(rules []string) error
	Count() int
}
