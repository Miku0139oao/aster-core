package provider

import (
	"fmt"

	"github.com/Miku0139oao/aster-core/component/trie"
	C "github.com/Miku0139oao/aster-core/constant"
	P "github.com/Miku0139oao/aster-core/constant/provider"
	"github.com/Miku0139oao/aster-core/log"
	"github.com/Miku0139oao/aster-core/rules/common"
)

type classicalStrategy struct {
	rules      []C.Rule
	count      int
	parse      common.ParseRuleFunc
	domainTrie *trie.DomainTrie[struct{}]
	domainSet  *trie.DomainSet
}

func (c *classicalStrategy) Behavior() P.RuleBehavior {
	return P.Classical
}

func (c *classicalStrategy) Match(metadata *C.Metadata, helper C.RuleMatchHelper) bool {
	// Leftover rules run first so PROCESS/IP helpers still fire when a DOMAIN
	// entry would also match. Domain-only sets keep rules empty.
	for _, rule := range c.rules {
		if m, _ := rule.Match(metadata, helper); m {
			return true
		}
	}
	return c.domainSet != nil && c.domainSet.Has(metadata.RuleHost())
}

func (c *classicalStrategy) Count() int {
	return c.count
}

func (c *classicalStrategy) Reset() {
	c.rules = nil
	c.count = 0
	c.domainTrie = nil
	c.domainSet = nil
}

func (c *classicalStrategy) Insert(rule string) {
	tp, payload, target, params := common.ParseRulePayload(rule, false)
	switch tp {
	case "MATCH", "RULE-SET", "SUB-RULE":
		log.Warnln("parse classical rule [%s] error: %s", rule, fmt.Errorf("unsupported rule type on classical rule-set: %s", tp))
		return
	case "DOMAIN":
		if c.insertDomain(payload) {
			c.count++
			return
		}
	case "DOMAIN-SUFFIX":
		if c.insertDomainSuffix(payload) {
			c.count++
			return
		}
	}
	r, err := c.parse(tp, payload, target, params, nil)
	if err != nil {
		log.Warnln("parse classical rule [%s] error: %s", rule, err.Error())
	} else {
		c.rules = append(c.rules, r)
		c.count++
	}
}

func (c *classicalStrategy) insertDomain(domain string) bool {
	if c.domainTrie == nil {
		c.domainTrie = trie.New[struct{}]()
	}
	return c.domainTrie.Insert(domain, struct{}{}) == nil
}

func (c *classicalStrategy) insertDomainSuffix(suffix string) bool {
	if c.domainTrie == nil {
		c.domainTrie = trie.New[struct{}]()
	}
	return c.domainTrie.Insert("+."+suffix, struct{}{}) == nil
}

func (c *classicalStrategy) payloadToRule(rule string) (C.Rule, error) {
	tp, payload, target, params := common.ParseRulePayload(rule, false)
	switch tp {
	case "MATCH", "RULE-SET", "SUB-RULE":
		return nil, fmt.Errorf("unsupported rule type on classical rule-set: %s", tp)
	}
	return c.parse(tp, payload, target, params, nil)
}

func (c *classicalStrategy) FinishInsert() {
	if c.domainTrie != nil {
		c.domainSet = c.domainTrie.NewDomainSet()
		c.domainTrie = nil
	}
}

func NewClassicalStrategy(parse common.ParseRuleFunc) *classicalStrategy {
	return &classicalStrategy{rules: []C.Rule{}, parse: parse}
}
