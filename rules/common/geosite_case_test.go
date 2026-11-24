package common

import (
	"testing"

	"github.com/Miku0139oao/aster-core/component/geodata"
	"github.com/Miku0139oao/aster-core/component/geodata/router"
)

// GEOSITE.MatchDomain forwards the raw request host to the matcher, so case
// folding has to happen inside the matcher. The matcher is injected here
// because the test must not depend on GeoSite.dat, which is not in the repo.
func TestGeoSiteMatchDomainFoldsHostCase(t *testing.T) {
	domains := []*router.Domain{
		{Type: router.Domain_Domain, Value: "github.com"},
		{Type: router.Domain_Full, Value: "exact.example"},
		{Type: router.Domain_Plain, Value: "keyword-tracker"},
		{Type: router.Domain_Regex, Value: `(^|\.)13mei[5-7]\.buzz$`},
	}
	matchers := []struct {
		name string
		new  func([]*router.Domain) (router.DomainMatcher, error)
	}{
		{"succinct", router.NewSuccinctMatcherGroup},
		{"mph", router.NewMphMatcherGroup},
	}
	hosts := map[string]bool{
		"www.github.com":          true,
		"WWW.GitHub.COM":          true,
		"EXACT.EXAMPLE":           true,
		"ads.KEYWORD-TRACKER.net": true,
		"13MEI5.BUZZ":             true,
		"no-such-host.invalid":    false,
	}
	for _, mc := range matchers {
		t.Run(mc.name, func(t *testing.T) {
			m, err := mc.new(domains)
			if err != nil {
				t.Fatalf("build matcher: %v", err)
			}
			gs := &GEOSITE{
				matcher:         m,
				matcherGen:      geodata.GeoSiteGeneration(),
				matcherResolved: true,
			}
			for host, want := range hosts {
				if got := gs.MatchDomain(host); got != want {
					t.Errorf("MatchDomain(%q) = %v, want %v", host, got, want)
				}
			}
		})
	}
}
