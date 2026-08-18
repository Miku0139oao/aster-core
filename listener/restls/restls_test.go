package restls

import (
	"testing"

	LC "github.com/Miku0139oao/aster-core/listener/config"
)

func TestNewCarriesRateLimit(t *testing.T) {
	const want uint64 = 654321
	builder := New(LC.ResTLS{RateLimit: want}, nil)
	if builder.config.RateLimit != want {
		t.Fatalf("rate limit = %d, want %d", builder.config.RateLimit, want)
	}
}
