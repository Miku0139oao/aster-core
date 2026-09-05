package cidr

import (
	"bytes"
	"net/netip"
	"sort"
	"testing"

	"go4.org/netipx"
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

			rangesLen := set.rangeCount()

			if rangesLen != test.expectedLen {
				t.Errorf("Expected len: %v, got: %v", test.expectedLen, rangesLen)
			}
			if set.rr != nil {
				t.Fatal("merged set retained duplicate []IPRange")
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

func TestNilReceiverContracts(t *testing.T) {
	var set *IpCidrSet
	if !set.IsEmpty() {
		t.Fatal("nil set should be empty")
	}
	if set.IsContain(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("nil IsContain")
	}
	if set.MatchIp(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("nil MatchIp")
	}
	if set.ToIPSet() == nil {
		t.Fatal("nil ToIPSet should return an empty IPSet")
	}
}

func TestMergeIdempotentKeepsCompactSet(t *testing.T) {
	set := NewIpCidrSet()
	if err := set.AddIpCidrForString("10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	if set.rr != nil {
		t.Fatal("first Merge retained rr")
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	if set.rr != nil {
		t.Fatal("second Merge reconstructed rr")
	}
	if !set.merged {
		t.Fatal("second Merge cleared merged")
	}
	if !set.IsContain(netip.MustParseAddr("10.1.2.3")) {
		t.Fatal("second Merge erased compact tables")
	}
	if set.IsEmpty() {
		t.Fatal("merged compact set reported empty")
	}
	if !set.MatchIp(netip.MustParseAddr("10.1.2.3")) {
		t.Fatal("MatchIp failed on merged compact set")
	}
}

func TestAddInvalidAfterMergeDoesNotMutate(t *testing.T) {
	set := NewIpCidrSet()
	if err := set.AddIpCidrForString("10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	if err := set.AddIpCidr(netip.Prefix{}); err == nil {
		t.Fatal("expected invalid prefix to fail")
	}
	if !set.merged {
		t.Fatal("invalid Add cleared merged")
	}
	if set.rr != nil {
		t.Fatal("invalid Add reconstructed rr")
	}
	if set.v4From == nil {
		t.Fatal("invalid Add dropped compact tables")
	}
	if !set.IsContain(netip.MustParseAddr("10.1.2.3")) {
		t.Fatal("invalid Add lost 10/8")
	}
}

func TestMergedEmptyIsEmpty(t *testing.T) {
	set := NewIpCidrSet()
	if !set.IsEmpty() {
		t.Fatal("new set should be empty")
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	if !set.merged {
		t.Fatal("Merge of empty set should mark merged")
	}
	if set.rr != nil {
		t.Fatal("empty Merge retained rr")
	}
	if !set.IsEmpty() {
		t.Fatal("merged empty set with nil rr must be empty")
	}
	if set.MatchIp(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("MatchIp on merged empty set")
	}
}

func TestForeachEarlyStopAndOrder(t *testing.T) {
	set := NewIpCidrSet()
	for _, cidr := range []string{"2001:db8::/32", "10.0.0.0/8"} {
		if err := set.AddIpCidrForString(cidr); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	var got []string
	set.Foreach(func(prefix netip.Prefix) bool {
		got = append(got, prefix.String())
		return true
	})
	want := []string{"10.0.0.0/8", "2001:db8::/32"}
	if len(got) != len(want) {
		t.Fatalf("Foreach prefixes %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Foreach order %v want %v", got, want)
		}
	}
	n := 0
	set.Foreach(func(prefix netip.Prefix) bool {
		n++
		return false
	})
	if n != 1 {
		t.Fatalf("early stop visited %d prefixes", n)
	}
}

func TestToIPSetCallerOwnsOutput(t *testing.T) {
	set := NewIpCidrSet()
	if err := set.AddIpCidrForString("10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	owned := set.ToIPSet()
	if err := set.AddIpCidrForString("11.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	if !owned.Contains(netip.MustParseAddr("10.1.2.3")) {
		t.Fatal("ToIPSet lost original content")
	}
	if owned.Contains(netip.MustParseAddr("11.1.2.3")) {
		t.Fatal("ToIPSet aliased live set after Add")
	}
	if !set.IsContain(netip.MustParseAddr("11.1.2.3")) {
		t.Fatal("live set missing added range")
	}
}

func TestDumpThenAddThenMerge(t *testing.T) {
	set := NewIpCidrSet()
	for _, cidr := range []string{"10.0.0.0/8", "2001:db8::/32"} {
		if err := set.AddIpCidrForString(cidr); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := set.WriteBin(&encoded); err != nil {
		t.Fatal(err)
	}
	n := 0
	set.Foreach(func(netip.Prefix) bool {
		n++
		return true
	})
	if n != 2 {
		t.Fatalf("Foreach after merge got %d", n)
	}
	ipset := set.ToIPSet()
	if !ipset.Contains(netip.MustParseAddr("10.1.2.3")) || !ipset.Contains(netip.MustParseAddr("2001:db8::1")) {
		t.Fatal("ToIPSet after dump missing mixed families")
	}
	if err := set.AddIpCidrForString("192.168.0.0/16"); err != nil {
		t.Fatal(err)
	}
	if !set.IsContain(netip.MustParseAddr("10.1.2.3")) {
		t.Fatal("Add after dump lost 10/8")
	}
	if !set.IsContain(netip.MustParseAddr("192.168.1.1")) {
		t.Fatal("Add after dump missed 192.168/16")
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	if set.rr != nil {
		t.Fatal("re-merge retained rr")
	}
	for _, ip := range []string{"10.1.2.3", "192.168.1.1", "2001:db8::1"} {
		if !set.IsContain(netip.MustParseAddr(ip)) {
			t.Fatalf("lost %s after dump/Add/Merge", ip)
		}
	}
}

func TestMergedBoundsAndFullFamily(t *testing.T) {
	set := NewIpCidrSet()
	for _, cidr := range []string{
		"0.0.0.0/0",
		"::/0",
	} {
		if err := set.AddIpCidrForString(cidr); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	if set.rr != nil {
		t.Fatal("merged /0 set retained rr")
	}
	for _, ip := range []string{
		"0.0.0.0",
		"255.255.255.255",
		"::",
		"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
	} {
		if !set.IsContain(netip.MustParseAddr(ip)) {
			t.Fatalf("full-family miss for %s", ip)
		}
	}

	singles := NewIpCidrSet()
	for _, cidr := range []string{
		"0.0.0.0/32",
		"255.255.255.255/32",
		"::/128",
		"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128",
	} {
		if err := singles.AddIpCidrForString(cidr); err != nil {
			t.Fatal(err)
		}
	}
	if err := singles.Merge(); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"0.0.0.0", "255.255.255.255", "::", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"} {
		if !singles.IsContain(netip.MustParseAddr(ip)) {
			t.Fatalf("singleton miss for %s", ip)
		}
	}
	if singles.IsContain(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("singleton set contained 1.1.1.1")
	}
}

func TestMergedSetMatchesNetipxControl(t *testing.T) {
	cidrs := []string{
		"10.0.0.0/8",
		"10.1.0.0/16",
		"192.168.1.0/24",
		"2001:db8::/32",
		"2001:db8:1::/48",
		"::ffff:10.0.0.0/104",
		"0.0.0.0/32",
		"255.255.255.255/32",
	}
	set := NewIpCidrSet()
	for _, cidr := range cidrs {
		if err := set.AddIpCidrForString(cidr); err != nil {
			t.Fatal(err)
		}
	}
	queries := []netip.Addr{
		netip.MustParseAddr("10.1.2.3"),
		netip.MustParseAddr("11.0.0.1"),
		netip.MustParseAddr("192.168.1.9"),
		netip.MustParseAddr("192.168.2.1"),
		netip.MustParseAddr("2001:db8::1"),
		netip.MustParseAddr("2001:db9::1"),
		netip.MustParseAddr("::ffff:10.1.2.3"),
		netip.MustParseAddr("0.0.0.0"),
		netip.MustParseAddr("255.255.255.255"),
		netip.MustParseAddr("::"),
	}
	for _, ip := range queries {
		got := set.IsContain(ip)
		want := netipxControlContains(t, cidrs, ip)
		if got != want {
			t.Fatalf("before Merge %s: got %v want %v", ip, got, want)
		}
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	if set.rr != nil {
		t.Fatal("merged mixed set retained rr")
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	control := netipxControlSet(t, cidrs)
	for _, ip := range queries {
		got := set.IsContain(ip)
		want := control.Contains(ip)
		if got != want {
			t.Fatalf("after Merge %s: got %v want %v", ip, got, want)
		}
	}
	var dumped []string
	set.Foreach(func(prefix netip.Prefix) bool {
		dumped = append(dumped, prefix.String())
		return true
	})
	wantPrefixes := make([]string, 0, len(control.Prefixes()))
	for _, p := range control.Prefixes() {
		wantPrefixes = append(wantPrefixes, p.String())
	}
	if !stringSlicesEqual(dumped, wantPrefixes) {
		t.Fatalf("Foreach prefixes %v want %v", dumped, wantPrefixes)
	}
	owned := set.ToIPSet()
	if !owned.Equal(control) {
		t.Fatal("ToIPSet diverged from netipx control")
	}
}

func netipxControlSet(t *testing.T, cidrs []string) *netipx.IPSet {
	t.Helper()
	var b netipx.IPSetBuilder
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			t.Fatal(err)
		}
		b.AddPrefix(prefix)
	}
	set, err := b.IPSet()
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func netipxControlContains(t *testing.T, cidrs []string, ip netip.Addr) bool {
	t.Helper()
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			t.Fatal(err)
		}
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
