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
	return &URLFilterEngine{}, nil
}

func (e *URLFilterEngine) LoadRules(rules []string) error {
	rulesStr := strings.Join(rules, "\n")
	ruleScanner := filterlist.NewRuleScanner(strings.NewReader(rulesStr), 1, false)

	config := &filterlist.StringConfig{
		RulesText:      rulesStr,
		ID:             1,
		IgnoreCosmetic: false,
	}
	stringList := filterlist.NewString(config)

	var rulesList []filterlist.Interface
	rulesList = append(rulesList, stringList)

	storage, err := filterlist.NewRuleStorage(rulesList)
	if err != nil {
		return err
	}

	e.engine = urlfilter.NewDNSEngine(storage)
	e.ruleCount = len(rules)

	// 你原来的精确计数逻辑，一字未动
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

	result, matched := e.engine.Match(domain)
	if !matched || result == nil {
		return false, ""
	}

	if result.NetworkRule != nil {
		// 新版统一用 .Text() 方法获取原始规则文本
		ruleText := result.NetworkRule.Text()
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
	if e.engine.RulesCount > 0 {
		return e.engine.RulesCount
	}
	return e.ruleCount
}
