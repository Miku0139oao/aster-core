package router_test

import (
	"testing"

	"github.com/Miku0139oao/aster-core/component/geodata"
	_ "github.com/Miku0139oao/aster-core/component/geodata/memconservative"
	"github.com/Miku0139oao/aster-core/component/geodata/router"
	_ "github.com/Miku0139oao/aster-core/component/geodata/standard"
	"github.com/Miku0139oao/aster-core/component/geodata/strmatcher"
)

// Build a matcher both ways over the shipped list: the indexed ApplyDomain and a
// brute-force sweep of every regex. They must agree for every host, otherwise
// the TLD bucket would be skipping a regex that could have matched.
func TestTLDIndexMatchesBruteForce(t *testing.T) {
	loader, err := geodata.GetGeoDataLoader("memconservative")
	if err != nil {
		t.Skipf("no geodata loader registered: %v", err)
	}
	for _, list := range []string{"geolocation-!cn", "cn", "private"} {
		domains, err := loader.LoadGeoSite(list)
		if err != nil {
			t.Skipf("%s unavailable: %v", list, err)
		}
		indexed, err := router.NewSuccinctMatcherGroup(domains)
		if err != nil {
			t.Fatalf("build %s: %v", list, err)
		}

		// Brute force: same construction, but every regex is tried for every host.
		var allRegex []strmatcher.Matcher
		for _, d := range domains {
			if d.Type != router.Domain_Regex {
				continue
			}
			m, err := strmatcher.Regex.New(d.Value)
			if err != nil {
				t.Fatalf("compile %q: %v", d.Value, err)
			}
			allRegex = append(allRegex, m)

		}

		hosts := []string{
			"www.github.com", "api.github.com", "example.com", "example.org",
			"13mei5.buzz", "x.13mei6.buzz", "18jmttios12.com", "a.b.783.tv",
			"no-such-host.invalid", "totally-unknown-name.com",
			"google.com", "www.google.com", "localhost", "a.b.c.d.e.f",
			"some.long.subdomain.example.net", "xn--fiq228c.tw",
		}
		for _, h := range hosts {
			want := indexed.ApplyDomain(h)
			// Only the regex bucket can differ; recompute it by brute force.
			regexHit := false
			for _, m := range allRegex {
				if m.Match(h) {
					regexHit = true
					break
				}
			}
			if !want && regexHit {
				t.Fatalf("%s: indexed said no match for %q but a regex matched", list, h)
			}
		}
		t.Logf("%s: %d domains, %d regexes, ok", list, len(domains), len(allRegex))
	}
}
