package inbound

import "testing"

func TestResTLSBuildCarriesRateLimit(t *testing.T) {
	const want uint64 = 123456
	got := (ResTLS{RateLimit: want}).Build()
	if got.RateLimit != want {
		t.Fatalf("rate limit = %d, want %d", got.RateLimit, want)
	}
}
