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
	var want bytes.Buffer
	want.WriteByte(1)
	if err := binary.Write(&want, binary.BigEndian, int64(len(set.rr))); err != nil {
		t.Fatal(err)
	}
	for _, r := range set.rr {
		if err := binary.Write(&want, binary.BigEndian, r.From().As16()); err != nil {
			t.Fatal(err)
		}
		if err := binary.Write(&want, binary.BigEndian, r.To().As16()); err != nil {
			t.Fatal(err)
		}
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

func benchMergedIpCidrSet(b *testing.B, size int) *IpCidrSet {
	b.Helper()
	set := NewIpCidrSet()
	for i := 0; i < size; i++ {
		value := i * 2
		addr := netip.AddrFrom4([4]byte{10, byte(value >> 16), byte(value >> 8), byte(value)})
		if err := set.AddIpCidr(netip.PrefixFrom(addr, 32)); err != nil {
			b.Fatal(err)
		}
	}
	if err := set.Merge(); err != nil {
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
			set := NewIpCidrSet()
			for i := 0; i < size; i++ {
				value := i * 2 // leave one-address gaps so Merge cannot coalesce ranges
				addr := netip.AddrFrom4([4]byte{10, byte(value >> 16), byte(value >> 8), byte(value)})
				if err := set.AddIpCidr(netip.PrefixFrom(addr, 32)); err != nil {
					b.Fatal(err)
				}
			}
			if err := set.Merge(); err != nil {
				b.Fatal(err)
			}
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
