package cidr

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"testing"

	"go4.org/netipx"
)

func TestWriteBinCompactMatchesRangeEncoding(t *testing.T) {
	prefixes := []string{
		"255.255.255.255/32", "0.0.0.0/31",
		"127.255.255.254/31", "128.0.0.0/31",
		"192.0.2.0/25", "192.0.2.128/25",
		"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128",
		"::/127", "8000::/126", "2001:db8:abcd:1234:5678:90ab:cdef:1234/128",
		"::ffff:192.0.2.0/120",
	}
	for _, merged := range []bool{false, true} {
		name := "unmerged"
		if merged {
			name = "merged"
		}
		t.Run(name, func(t *testing.T) {
			set := NewIpCidrSet()
			var builder netipx.IPSetBuilder
			var ranges []netipx.IPRange
			for _, s := range prefixes {
				p := netip.MustParsePrefix(s)
				if err := set.AddIpCidr(p); err != nil {
					t.Fatal(err)
				}
				builder.AddPrefix(p)
				ranges = append(ranges, netipx.RangeOfPrefix(p))
			}
			if merged {
				if err := set.Merge(); err != nil {
					t.Fatal(err)
				}
				reference, err := builder.IPSet()
				if err != nil {
					t.Fatal(err)
				}
				ranges = reference.Ranges()
			}
			// Independent original wire encoder: do not use compact bounds,
			// conversion helpers, or the production range-iteration helper.
			want := []byte{1}
			var count [8]byte
			binary.BigEndian.PutUint64(count[:], uint64(len(ranges)))
			want = append(want, count[:]...)
			for _, r := range ranges {
				from, to := r.From().As16(), r.To().As16()
				want = append(want, from[:]...)
				want = append(want, to[:]...)
			}
			var got bytes.Buffer
			if err := set.WriteBin(&got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got.Bytes(), want) {
				t.Fatalf("wire encoding differs:\ngot  %x\nwant %x", got.Bytes(), want)
			}
			if merged && set.rr != nil {
				t.Fatal("export restored retained IPRange storage")
			}
		})
	}
}
