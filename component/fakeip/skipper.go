package fakeip

import (
	"strings"
	"sync"

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

// skipperMetaPool reuses Metadata for the FilterRule path. Rule.Match is an
// interface call, so a stack Metadata would escape; pooling removes the 416 B
// heap alloc per DNS name. Entries are zeroed before Put so Host/GeoIP are not
// retained. Filter rules must not keep the Metadata pointer after Match returns.
var skipperMetaPool = sync.Pool{
	New: func() any { return new(C.Metadata) },
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
		metadata := skipperMetaPool.Get().(*C.Metadata)
		*metadata = C.Metadata{Host: domain}
		matched := false
		action := ""
		for _, rule := range p.Rules {
			if m, a := rule.Match(metadata, C.RuleMatchHelper{}); m {
				matched = true
				action = a
				break
			}
		}
		*metadata = C.Metadata{}
		skipperMetaPool.Put(metadata)
		if matched {
			return action == UseRealIP
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
