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

// On the shipped data the extracted TLD must be a necessary condition: if a
// regex matches a host, the host must carry that label at its end.
func TestRegexTLDIsNecessaryOnRealData(t *testing.T) {
	loader, err := geodata.GetGeoDataLoader("memconservative")
	if err != nil {
		t.Fatal(err)
	}
	domains, err := loader.LoadGeoSite("geolocation-!cn")
	if err != nil {
		t.Fatal(err)
	}
	hosts := []string{
		"13mei5.buzz", "x.13mei6.buzz", "18jmttios12.com", "a.b.783.tv",
		"www.github.com", "no-such-host.invalid", "example.org", "a.b.example.co-uk",
	}
	indexed, skipped := 0, 0
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
			skipped++
			continue
		}
		indexed++
		for _, h := range hosts {
			if !m.Match(h) {
				continue
			}
			if !strings.Contains(h, "."+tld) && !strings.HasSuffix(h, tld) {
				t.Fatalf("regex %q matched %q but tld %q is not a label suffix", d.Value, h, tld)
			}
		}
	}
	t.Logf("indexed=%d skipped=%d", indexed, skipped)
	if indexed == 0 {
		t.Fatal("no regex was indexable; the design assumption is wrong")
	}
}
