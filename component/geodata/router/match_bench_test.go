package router_test

import (
	"testing"

	"github.com/Miku0139oao/aster-core/component/geodata"
	_ "github.com/Miku0139oao/aster-core/component/geodata/memconservative"
	_ "github.com/Miku0139oao/aster-core/component/geodata/standard"
)

// geolocation-!cn carries 151 regex patterns. Before the TLD index, every host
// that missed the domain set ran all of them: 87us per rule evaluation, on every
// new connection.
func BenchmarkGeoSiteApply(b *testing.B) {
	m, err := geodata.LoadGeoSiteMatcher("geolocation-!cn")
	if err != nil {
		b.Skipf("geosite data unavailable: %v", err)
	}
	cases := map[string]string{
		"miss-unknown-tld": "no-such-host.invalid",
		"hit-domain-set":   "www.github.com",
	}
	for name, host := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.ApplyDomain(host)
			}
		})
	}
}
