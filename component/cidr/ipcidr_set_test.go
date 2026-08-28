package cidr

import (
	"net/netip"
	"testing"
)

func TestIpv4(t *testing.T) {
	tests := []struct {
		name     string
		ipCidr   string
		ip       string
		expected bool
	}{
		{
			name:     "Test Case 1",
			ipCidr:   "149.154.160.0/20",
			ip:       "149.154.160.0",
			expected: true,
		},
		{
			name:     "Test Case 2",
			ipCidr:   "192.168.0.0/16",
			ip:       "10.0.0.1",
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set := &IpCidrSet{}
			set.AddIpCidrForString(test.ipCidr)

			result := set.IsContainForString(test.ip)
			if result != test.expected {
				t.Errorf("Expected result: %v, got: %v", test.expected, result)
			}
		})
	}
}

func TestIpv6(t *testing.T) {
	tests := []struct {
		name     string
		ipCidr   string
		ip       string
		expected bool
	}{
		{
			name:     "Test Case 1",
			ipCidr:   "2409:8000::/20",
			ip:       "2409:8087:1e03:21::27",
			expected: true,
		},
		{
			name:     "Test Case 2",
			ipCidr:   "240e::/16",
			ip:       "240e:964:ea02:100:1800::71",
			expected: true,
		},
	}
	// Add more test cases as needed

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set := &IpCidrSet{}
			set.AddIpCidrForString(test.ipCidr)

			result := set.IsContainForString(test.ip)
			if result != test.expected {
				t.Errorf("Expected result: %v, got: %v", test.expected, result)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	tests := []struct {
		name        string
		ipCidr1     string
		ipCidr2     string
		ipCidr3     string
		expectedLen int
	}{
		{
			name:        "Test Case 1",
			ipCidr1:     "2409:8000::/20",
			ipCidr2:     "2409:8000::/21",
			ipCidr3:     "2409:8000::/48",
			expectedLen: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set := &IpCidrSet{}
			set.AddIpCidrForString(test.ipCidr1)
			set.AddIpCidrForString(test.ipCidr2)
			set.Merge()

			rangesLen := len(set.rr)

			if rangesLen != test.expectedLen {
				t.Errorf("Expected len: %v, got: %v", test.expectedLen, rangesLen)
			}
		})
	}
}

func TestMergedContainsIPv4AndIPv6(t *testing.T) {
	set := NewIpCidrSet()
	for _, cidr := range []string{"10.0.0.0/8", "2001:db8::/32", "192.168.1.0/24"} {
		if err := set.AddIpCidrForString(cidr); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"10.1.2.3", "192.168.1.9", "2001:db8::1"} {
		if !set.IsContain(netip.MustParseAddr(ip)) {
			t.Fatalf("expected hit for %s", ip)
		}
	}
	for _, ip := range []string{"11.0.0.1", "192.168.2.1", "2001:db9::1", "fe80::1", "::ffff:10.1.2.3"} {
		if set.IsContain(netip.MustParseAddr(ip)) {
			t.Fatalf("expected miss for %s", ip)
		}
	}
}

func TestIsContainAfterAddInvalidatesMergedLookup(t *testing.T) {
	set := NewIpCidrSet()
	if err := set.AddIpCidrForString("10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	hit := netip.MustParseAddr("10.1.2.3")
	miss := netip.MustParseAddr("11.1.2.3")
	if !set.IsContain(hit) {
		t.Fatal("expected 10.1.2.3 in merged 10/8")
	}
	if set.IsContain(miss) {
		t.Fatal("did not expect 11.1.2.3 in merged 10/8")
	}
	if err := set.AddIpCidrForString("11.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	if set.merged {
		t.Fatal("Add after Merge left merged=true with stale compact tables")
	}
	if set.v4From != nil || set.v6From != nil {
		t.Fatal("Add after Merge left compact lookup tables allocated")
	}
	if !set.IsContain(miss) {
		t.Fatal("stale merged lookup missed 11.1.2.3 after Add")
	}
	lo := netip.MustParseAddr("10.0.0.0")
	hi := netip.MustParseAddr("10.255.255.255")
	if !set.IsContain(lo) || !set.IsContain(hi) {
		t.Fatal("expected 10/8 inclusive bounds to still hit after Add")
	}
}

func TestIsContainBeforeMergeWithUnsortedRanges(t *testing.T) {
	set := NewIpCidrSet()
	if err := set.AddIpCidrForString("200.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	if err := set.AddIpCidrForString("10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}

	for _, ip := range []string{"200.1.2.3", "10.1.2.3"} {
		addr := netip.MustParseAddr(ip)
		if !set.IsContain(addr) {
			t.Fatalf("expected %s to be contained before Merge", ip)
		}
	}
}
