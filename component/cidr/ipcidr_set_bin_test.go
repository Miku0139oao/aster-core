package cidr

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"strconv"
	"testing"

	"go4.org/netipx"
)

func writeBinaryRange(t *testing.T, buffer *bytes.Buffer, value netipx.IPRange) {
	t.Helper()
	if err := binary.Write(buffer, binary.BigEndian, value.From().As16()); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(buffer, binary.BigEndian, value.To().As16()); err != nil {
		t.Fatal(err)
	}
}

func TestIpCidrSetWriteBinMatchesBinaryPackage(t *testing.T) {
	// Golden bytes are built from netipx ranges, not from IpCidrSet internals.
	r4 := netipx.RangeOfPrefix(netip.MustParsePrefix("10.0.0.0/8"))
	r6 := netipx.RangeOfPrefix(netip.MustParsePrefix("2001:db8::/32"))
	var want bytes.Buffer
	want.WriteByte(1)
	if err := binary.Write(&want, binary.BigEndian, int64(2)); err != nil {
		t.Fatal(err)
	}
	writeBinaryRange(t, &want, r4)
	writeBinaryRange(t, &want, r6)

	set := NewIpCidrSet()
	for _, cidr := range []string{"10.0.0.0/8", "2001:db8::/32"} {
		if err := set.AddIpCidrForString(cidr); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := set.WriteBin(&got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Fatal("WriteBin wire format changed")
	}
}

func TestReadIpCidrSetNormalizesUnsortedRanges(t *testing.T) {
	var encoded bytes.Buffer
	encoded.WriteByte(1)
	if err := binary.Write(&encoded, binary.BigEndian, int64(2)); err != nil {
		t.Fatal(err)
	}
	writeBinaryRange(t, &encoded, netipx.RangeOfPrefix(netip.MustParsePrefix("200.0.0.0/8")))
	writeBinaryRange(t, &encoded, netipx.RangeOfPrefix(netip.MustParsePrefix("10.0.0.0/8")))

	set, err := ReadIpCidrSet(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !set.merged {
		t.Fatal("binary IP set was not normalized for binary lookup")
	}
	if set.rr != nil {
		t.Fatal("ReadIpCidrSet retained duplicate []IPRange after Merge")
	}
	for _, value := range []string{"10.1.2.3", "200.1.2.3"} {
		if !set.IsContain(netip.MustParseAddr(value)) {
			t.Fatalf("normalized set does not contain %s", value)
		}
	}
	if set.IsContain(netip.MustParseAddr("192.0.2.1")) {
		t.Fatal("normalized set contains unrelated address")
	}
}

func TestReadIpCidrSetRejectsUnboundedRangeCount(t *testing.T) {
	var encoded bytes.Buffer
	encoded.WriteByte(1)
	if err := binary.Write(&encoded, binary.BigEndian, int64(maxBinaryIPRanges+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadIpCidrSet(&encoded); err == nil {
		t.Fatal("expected excessive range count to fail before allocation")
	}
}

func TestWriteBinReadRoundTripIndependentGolden(t *testing.T) {
	r4 := netipx.RangeOfPrefix(netip.MustParsePrefix("10.0.0.0/8"))
	r6 := netipx.RangeOfPrefix(netip.MustParsePrefix("2001:db8::/32"))
	mapped := netipx.RangeOfPrefix(netip.MustParsePrefix("::ffff:10.0.0.0/104"))
	var golden bytes.Buffer
	golden.WriteByte(1)
	if err := binary.Write(&golden, binary.BigEndian, int64(3)); err != nil {
		t.Fatal(err)
	}
	// netip.Addr.Compare: all IPv4, then IPv6 by 128-bit value (::ffff:10.0.0.0 < 2001:db8::).
	writeBinaryRange(t, &golden, r4)
	writeBinaryRange(t, &golden, mapped)
	writeBinaryRange(t, &golden, r6)

	set := NewIpCidrSet()
	for _, cidr := range []string{"2001:db8::/32", "::ffff:10.0.0.0/104", "10.0.0.0/8"} {
		if err := set.AddIpCidrForString(cidr); err != nil {
			t.Fatal(err)
		}
	}
	if err := set.Merge(); err != nil {
		t.Fatal(err)
	}
	if !set.IsContain(netip.MustParseAddr("::ffff:10.1.2.3")) {
		t.Fatal("live merged set must keep IPv4-mapped v6 membership")
	}
	var got bytes.Buffer
	if err := set.WriteBin(&got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), golden.Bytes()) {
		t.Fatal("merged WriteBin does not match independent netipx golden")
	}

	decoded, err := ReadIpCidrSet(bytes.NewReader(golden.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.rr != nil {
		t.Fatal("round-trip retained rr")
	}
	// ReadIpCidrSet Unmaps IPv4-mapped endpoints, so the mapped /104 becomes
	// IPv4 10/8 and merges with the existing v4 range. Live WriteBin still
	// emits the mapped v6 range; lookup after Read follows Unmap semantics.
	control := netipxControlSet(t, []string{"10.0.0.0/8", "2001:db8::/32"})
	for _, ip := range []string{"10.1.2.3", "2001:db8::1", "11.0.0.1", "192.0.2.1"} {
		addr := netip.MustParseAddr(ip)
		if decoded.IsContain(addr) != control.Contains(addr) {
			t.Fatalf("round-trip mismatch for %s", ip)
		}
	}
	if decoded.IsContain(netip.MustParseAddr("::ffff:10.1.2.3")) {
		t.Fatal("Read Unmap must not keep IPv4-mapped v6 membership")
	}
	if err := decoded.AddIpCidrForString("192.168.0.0/16"); err != nil {
		t.Fatal(err)
	}
	if !decoded.IsContain(netip.MustParseAddr("10.1.2.3")) || !decoded.IsContain(netip.MustParseAddr("192.168.1.1")) {
		t.Fatal("Add after dump/read lost content")
	}
}

// buildNoncoalescingMergedSet builds size IPv4 /32 ranges with one-address
// gaps so Merge cannot coalesce. Parent A/B fixture for 1k/100k lookup.
func buildNoncoalescingMergedSet(size int) (*IpCidrSet, error) {
	set := NewIpCidrSet()
	for i := 0; i < size; i++ {
		value := i * 2
		addr := netip.AddrFrom4([4]byte{10, byte(value >> 16), byte(value >> 8), byte(value)})
		if err := set.AddIpCidr(netip.PrefixFrom(addr, 32)); err != nil {
			return nil, err
		}
	}
	if err := set.Merge(); err != nil {
		return nil, err
	}
	return set, nil
}

func benchMergedIpCidrSet(b *testing.B, size int) *IpCidrSet {
	b.Helper()
	set, err := buildNoncoalescingMergedSet(size)
	if err != nil {
		b.Fatal(err)
	}
	return set
}

func BenchmarkIpCidrSetWriteBin(b *testing.B) {
	set := benchMergedIpCidrSet(b, 10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var encoded bytes.Buffer
		if err := set.WriteBin(&encoded); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadIpCidrSet(b *testing.B) {
	set := benchMergedIpCidrSet(b, 10_000)
	var encoded bytes.Buffer
	if err := set.WriteBin(&encoded); err != nil {
		b.Fatal(err)
	}
	payload := encoded.Bytes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ReadIpCidrSet(bytes.NewReader(payload)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIpCidrSetMergedMiss(b *testing.B) {
	for _, size := range []int{1_000, 100_000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			set := benchMergedIpCidrSet(b, size)
			miss := netip.MustParseAddr("192.0.2.1")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if set.IsContain(miss) {
					b.Fatal("unexpected match")
				}
			}
		})
	}
}
