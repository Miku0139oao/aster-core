package router_test

import (
	"strings"
	"testing"

	"github.com/Miku0139oao/aster-core/component/geodata"
	_ "github.com/Miku0139oao/aster-core/component/geodata/memconservative"
	"github.com/Miku0139oao/aster-core/component/geodata/router"
	_ "github.com/Miku0139oao/aster-core/component/geodata/standard"
	"github.com/Miku0139oao/aster-core/component/geodata/strmatcher"
)

// These are copies of real patterns from geolocation-!cn, embedded so the
// invariant is checked in CI where GeoSite.dat is not present.
var realShapedRegexes = []string{
	`(^|\.)13mei[5-7]\.buzz$`,
	`(^|\.)18jmttios[0-9]{2}\.com$`,
	`(^|\.)18j[efg]\.life$`,
	`(^|\.)1[0-9]{2}\.one$`,
	`(^|\.)2024(3[0-9]|4[0-9]|6[0-9]|7[0-9])\.xyz$`,
	`(^|\.)3[0-9]{2}\.icu$`,
	`(^|\.)783([a-z][a-z]?)?\.tv$`,
	`(^|\.)8dgo[1-57-9]\.com$`,
	`.*\.googlevideo\.com`,
	// These must stay unindexed; the matcher has to keep trying them.
	`(^|\.)example\.[a-z]+$`,
	`.*`,
}

// The extracted TLD must be a necessary condition: whenever the regex matches a
// host, that host must carry the label. This is what makes the bucket sound.
func TestRegexTLDIsNecessary(t *testing.T) {
	hosts := []string{
		"13mei5.buzz", "x.13mei6.buzz", "18jmttios12.com", "a.b.783.tv",
		"www.github.com", "no-such-host.invalid", "example.org", "a.b.example.co-uk",
		"r1---sn-abc.googlevideo.com", "googlevideo.com", "1xx.one", "20243.xyz",
		"example.anything", "383.icu", "8dgo3.com",
	}
	indexed, skipped := 0, 0
	for _, p := range realShapedRegexes {
		m, err := strmatcher.Regex.New(p)
		if err != nil {
			t.Fatalf("compile %q: %v", p, err)
		}
		tld, ok := router.RegexTLDForTest(p)
		if !ok {
			skipped++
			continue
		}
		indexed++
		for _, h := range hosts {
			if !m.Match(h) {
				continue
			}
			if !strings.Contains(h, "."+tld) && !strings.HasSuffix(h, tld) {
				t.Fatalf("regex %q matched %q but tld %q is not a label suffix", p, h, tld)
			}
		}
	}
	t.Logf("indexed=%d skipped=%d", indexed, skipped)
	if indexed == 0 {
		t.Fatal("no regex was indexable; the design assumption is wrong")
	}
}

// On a developer checkout with GeoSite.dat, also check the shipped data.
func TestRegexTLDIsNecessaryOnShippedData(t *testing.T) {
	loader, err := geodata.GetGeoDataLoader("memconservative")
	if err != nil {
		t.Skipf("no geodata loader registered: %v", err)
	}
	domains, err := loader.LoadGeoSite("geolocation-!cn")
	if err != nil {
		t.Skipf("geosite data unavailable (not checked in): %v", err)
	}
	hosts := []string{
		"13mei5.buzz", "x.13mei6.buzz", "18jmttios12.com", "a.b.783.tv",
		"www.github.com", "no-such-host.invalid", "example.org",
	}
	for _, d := range domains {
		if d.Type != router.Domain_Regex {
			continue
		}
		m, err := strmatcher.Regex.New(d.Value)
		if err != nil {
			t.Fatalf("compile %q: %v", d.Value, err)
		}
		tld, ok := router.RegexTLDForTest(d.Value)
		if !ok {
			continue
		}
		for _, h := range hosts {
			if !m.Match(h) {
				continue
			}
			if !strings.Contains(h, "."+tld) && !strings.HasSuffix(h, tld) {
				t.Fatalf("regex %q matched %q but tld %q is not a label suffix", d.Value, h, tld)
			}
		}
	}
}
