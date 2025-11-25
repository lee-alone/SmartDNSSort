package adblock

import (
	"strings"

	"github.com/AdguardTeam/urlfilter"
	"github.com/AdguardTeam/urlfilter/filterlist"
)

type URLFilterEngine struct {
	engine    *urlfilter.DNSEngine
	ruleCount int
}

func NewURLFilterEngine() (*URLFilterEngine, error) {
	return &URLFilterEngine{},
		nil
}

func (e *URLFilterEngine) LoadRules(rules []string) error {
	// Create a string rule list from the rules
	rulesStr := strings.Join(rules, "\n")
	ruleScanner := filterlist.NewRuleScanner(strings.NewReader(rulesStr), 1, false)

	// Create a simple in-memory rule list
	var rulesList []filterlist.RuleList
	stringList := &filterlist.StringRuleList{}
	stringList.ID = 1
	stringList.RulesText = rulesStr
	stringList.IgnoreCosmetic = false

	rulesList = append(rulesList, stringList)

	// Create rule storage
	storage, err := filterlist.NewRuleStorage(rulesList)
	if err != nil {
		return err
	}

	// Create DNS engine
	e.engine = urlfilter.NewDNSEngine(storage)
	e.ruleCount = len(rules)

	// Count from scanner for accuracy
	actualCount := 0
	for ruleScanner.Scan() {
		actualCount++
	}
	if actualCount > 0 {
		e.ruleCount = actualCount
	}

	return nil
}

func (e *URLFilterEngine) CheckHost(domain string) (bool, string) {
	if e.engine == nil {
		return false, ""
	}

	// Match against the hostname
	result, matched := e.engine.Match(domain)

	if !matched || result == nil {
		return false, ""
	}

	// Check if it's a blocking rule (not a whitelist/exception)
	if result.NetworkRule != nil {
		ruleText := result.NetworkRule.RuleText
		// If it's a whitelist rule (starts with @@), don't block
		if strings.HasPrefix(ruleText, "@@") {
			return false, ""
		}
		return true, ruleText
	}

	return false, ""
}

func (e *URLFilterEngine) Count() int {
	if e.engine == nil {
		return 0
	}
	// Use the stored count from RulesCount or our own counter
	if e.engine.RulesCount > 0 {
		return e.engine.RulesCount
	}
	return e.ruleCount
}
