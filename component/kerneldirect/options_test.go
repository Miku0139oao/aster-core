package kerneldirect

import (
	"math"
	"net/netip"
	"strings"
	"testing"
)

func TestNormalizeMaxEntries(t *testing.T) {
	tests := []struct {
		name    string
		in      uint32
		want    uint32
		wantErr bool
	}{
		{name: "zero defaults", in: 0, want: DefaultMaxEntries},
		{name: "one persists", in: 1, want: 1},
		{name: "default identity", in: DefaultMaxEntries, want: DefaultMaxEntries},
		{name: "explicit mid-range", in: 8192, want: 8192},
		{name: "maximum accepted", in: MaximumMaxEntries, want: MaximumMaxEntries},
		{name: "just over maximum rejected", in: MaximumMaxEntries + 1, wantErr: true},
		{name: "uint32 max rejected", in: math.MaxUint32, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeMaxEntries(test.in)
			if test.wantErr {
				if err == nil {
					t.Fatalf("NormalizeMaxEntries(%d) = %d, want error", test.in, got)
				}
				if got != 0 {
					t.Fatalf("NormalizeMaxEntries(%d) returned %d with error %v; want 0", test.in, got, err)
				}
				if !strings.Contains(err.Error(), "65536") && !strings.Contains(err.Error(), "maximum") {
					t.Fatalf("NormalizeMaxEntries(%d) error %q should name the maximum", test.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeMaxEntries(%d) error: %v", test.in, err)
			}
			if got != test.want {
				t.Fatalf("NormalizeMaxEntries(%d) = %d, want %d", test.in, got, test.want)
			}
		})
	}
}

func TestNormalizeMaxEntriesRejectsInsteadOfClamping(t *testing.T) {
	// Config/API callers must see an error. Register still clamps as a
	// backstop; that split is intentional and must not drift here.
	got, err := NormalizeMaxEntries(MaximumMaxEntries + 1)
	if err == nil {
		t.Fatal("NormalizeMaxEntries must reject values over MaximumMaxEntries")
	}
	if got == MaximumMaxEntries {
		t.Fatal("NormalizeMaxEntries clamped over-max input; only Register may do that")
	}
}

func TestFastPathOverheadCountsStaticKeysAndProxyDefaults(t *testing.T) {
	direct := []netip.Prefix{netip.MustParsePrefix("8.8.8.8/32")}
	proxy := []netip.Prefix{netip.MustParsePrefix("1.1.1.0/24"), netip.MustParsePrefix("2001:db8::/32")}
	bypass := []netip.Prefix{netip.MustParsePrefix("203.0.113.1/32")}
	empty := []netip.Prefix{}

	tests := []struct {
		name          string
		proxySteering bool
		direct        []netip.Prefix
		proxy         []netip.Prefix
		bypass        []netip.Prefix
		want          uint32
	}{
		{name: "no prefixes without steering"},
		{name: "empty slices without steering", direct: empty, proxy: empty, bypass: empty},
		{name: "proxy /0 pair only", proxySteering: true, want: 2},
		{name: "empty slices with steering", proxySteering: true, direct: empty, proxy: empty, bypass: empty, want: 2},
		{name: "static direct only", direct: direct, want: 1},
		{name: "static proxy only", proxy: proxy, want: 2},
		{name: "static bypass only", bypass: bypass, want: 1},
		{name: "all lists without steering", direct: direct, proxy: proxy, bypass: bypass, want: 4},
		{name: "PROXY /0 pair plus one bypass", proxySteering: true, bypass: bypass, want: 3},
		{name: "all lists with steering", proxySteering: true, direct: direct, proxy: proxy, bypass: bypass, want: 6},
		{
			name:          "duplicate prefixes share one LPM key",
			proxySteering: true,
			direct:        []netip.Prefix{direct[0], direct[0]},
			bypass:        []netip.Prefix{bypass[0], bypass[0]},
			want:          4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := FastPathOverhead(test.proxySteering, test.direct, test.proxy, test.bypass)
			if got != test.want {
				t.Fatalf("FastPathOverhead = %d, want %d", got, test.want)
			}
		})
	}
}

func TestFastPathOverheadExceedsLegalMaxWithoutReservation(t *testing.T) {
	if MaximumMaxEntries != DefaultEBPFMaxEntries {
		t.Fatalf("learned cap %d and eBPF map cap %d must stay paired so overhead is the overflow", MaximumMaxEntries, DefaultEBPFMaxEntries)
	}
	if FastPathOverhead(false, nil, nil, nil) != 0 {
		t.Fatal("zero-overhead pack must fit a legal-max learned set exactly")
	}
	if MaximumMaxEntries+FastPathOverhead(false, nil, nil, nil) != DefaultEBPFMaxEntries {
		t.Fatal("legal-max learned set with no static keys must still fill the eBPF map exactly")
	}

	overhead := FastPathOverhead(true, nil, nil, []netip.Prefix{netip.MustParsePrefix("203.0.113.1/32")})
	if overhead != 3 {
		t.Fatalf("PROXY /0 pair + one bypass overhead = %d, want 3", overhead)
	}
	if MaximumMaxEntries+overhead <= DefaultEBPFMaxEntries {
		t.Fatal("legal-max learned set plus bypass/PROXY /0 still fits; packer must reserve overhead")
	}

	// Any positive overhead overflows a legal-max learned set because the two
	// caps are paired. Keep that reservation contract explicit.
	for _, overhead := range []uint32{
		FastPathOverhead(true, nil, nil, nil),
		FastPathOverhead(false, []netip.Prefix{netip.MustParsePrefix("8.8.8.8/32")}, nil, nil),
		FastPathOverhead(true, nil, []netip.Prefix{netip.MustParsePrefix("1.1.1.0/24")}, []netip.Prefix{netip.MustParsePrefix("203.0.113.1/32")}),
	} {
		if overhead == 0 {
			t.Fatal("expected positive FastPathOverhead for reservation cases")
		}
		if MaximumMaxEntries+overhead <= DefaultEBPFMaxEntries {
			t.Fatalf("overhead %d still fits in the paired eBPF cap; packer reservation would be optional", overhead)
		}
	}
}
