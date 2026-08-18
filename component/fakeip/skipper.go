package fakeip

import (
	"strings"

	C "github.com/Miku0139oao/aster-core/constant"
)

const (
	UseFakeIP = "fake-ip"
	UseRealIP = "real-ip"
)

type Skipper struct {
	Rules []C.Rule
	Host  []C.DomainMatcher
	Mode  C.FilterMode
}

// ShouldSkipped return if domain should be skipped
func (p *Skipper) ShouldSkipped(domain string) bool {
	if p == nil {
		return false
	}
	// RFC 4343: DNS names are case-insensitive. Filter rules store
	// lower-cased hosts, but wire queries may arrive in mixed case.
	domain = strings.ToLower(domain)
	if len(p.Rules) > 0 {
		metadata := &C.Metadata{Host: domain}
		for _, rule := range p.Rules {
			if matched, action := rule.Match(metadata, C.RuleMatchHelper{}); matched {
				return action == UseRealIP
			}
		}
		return false
	}

	should := p.shouldSkipped(domain)
	if p.Mode == C.FilterWhiteList {
		return !should
	}
	return should
}

func (p *Skipper) shouldSkipped(domain string) bool {
	for _, matcher := range p.Host {
		if matcher.MatchDomain(domain) {
			return true
		}
	}
	return false
}
