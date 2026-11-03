package memconservative

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Miku0139oao/aster-core/component/geodata/router"

	"google.golang.org/protobuf/proto"
)

// writeGeoIPList encodes list as a GeoIPList protobuf and writes it to a temp file.
func writeGeoIPList(t *testing.T, list *router.GeoIPList) string {
	t.Helper()
	data, err := proto.Marshal(list)
	if err != nil {
		t.Fatalf("failed to marshal GeoIPList: %v", err)
	}
	filename := filepath.Join(t.TempDir(), "geoip.dat")
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		t.Fatalf("failed to write test geodata file: %v", err)
	}
	return filename
}

// writeGeoSiteList encodes list as a GeoSiteList protobuf and writes it to a temp file.
func writeGeoSiteList(t *testing.T, list *router.GeoSiteList) string {
	t.Helper()
	data, err := proto.Marshal(list)
	if err != nil {
		t.Fatalf("failed to marshal GeoSiteList: %v", err)
	}
	filename := filepath.Join(t.TempDir(), "geosite.dat")
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		t.Fatalf("failed to write test geodata file: %v", err)
	}
	return filename
}

func testGeoIPList() *router.GeoIPList {
	return &router.GeoIPList{
		Entry: []*router.GeoIP{
			{
				CountryCode: "PRIVATE",
				Cidr: []*router.CIDR{
					{Ip: []byte{10, 0, 0, 0}, Prefix: 8},
					{Ip: []byte{172, 16, 0, 0}, Prefix: 12},
				},
			},
			{
				CountryCode: "CN",
				Cidr: []*router.CIDR{
					{Ip: []byte{1, 0, 1, 0}, Prefix: 24},
				},
			},
			{
				CountryCode: "US",
				Cidr: []*router.CIDR{
					{Ip: []byte{8, 8, 8, 8}, Prefix: 32},
				},
			},
			{
				// Multi-byte length varint: a large CIDR list forces a >127 byte message,
				// which exercises the multi-byte varint paths of the decoder.
				CountryCode:  "MULTI",
				Cidr:         manyCIDRs(200),
				ReverseMatch: true,
			},
		},
	}
}

func manyCIDRs(n int) []*router.CIDR {
	cidrs := make([]*router.CIDR, 0, n)
	for i := 0; i < n; i++ {
		cidrs = append(cidrs, &router.CIDR{
			Ip:     []byte{192, byte(i >> 8), byte(i), 0},
			Prefix: 24,
		})
	}
	return cidrs
}

func TestDecodeGeoIPList(t *testing.T) {
	list := testGeoIPList()
	filename := writeGeoIPList(t, list)

	for _, entry := range list.Entry {
		t.Run(entry.CountryCode, func(t *testing.T) {
			got, err := Decode(filename, entry.CountryCode)
			if err != nil {
				t.Fatalf("Decode(%q) returned error: %v", entry.CountryCode, err)
			}
			var decoded router.GeoIP
			if err := proto.Unmarshal(got, &decoded); err != nil {
				t.Fatalf("decoded bytes are not a valid GeoIP message: %v", err)
			}
			if !proto.Equal(&decoded, entry) {
				t.Fatalf("decoded GeoIP mismatch:\n got: %v\nwant: %v", decoded.String(), entry.String())
			}
		})
	}
}

func TestDecodeGeoIPListCaseInsensitive(t *testing.T) {
	filename := writeGeoIPList(t, testGeoIPList())

	got, err := Decode(filename, "cn")
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	var decoded router.GeoIP
	if err := proto.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("decoded bytes are not a valid GeoIP message: %v", err)
	}
	if decoded.CountryCode != "CN" {
		t.Fatalf("decoded country code = %q, want %q", decoded.CountryCode, "CN")
	}
}

func TestDecodeLastEntry(t *testing.T) {
	filename := writeGeoIPList(t, testGeoIPList())

	got, err := Decode(filename, "MULTI")
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	var decoded router.GeoIP
	if err := proto.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("decoded bytes are not a valid GeoIP message: %v", err)
	}
	if len(decoded.Cidr) != 200 {
		t.Fatalf("decoded %d CIDRs, want 200", len(decoded.Cidr))
	}
}

func TestDecodeGeoSiteList(t *testing.T) {
	list := &router.GeoSiteList{
		Entry: []*router.GeoSite{
			{
				CountryCode: "CATEGORY-ADS",
				Domain: []*router.Domain{
					{Type: router.Domain_Domain, Value: "ads.example.com"},
					{Type: router.Domain_Full, Value: "tracker.example.net"},
				},
			},
			{
				CountryCode: "GOOGLE",
				Domain: []*router.Domain{
					{Type: router.Domain_Domain, Value: "google.com"},
					{Type: router.Domain_Regex, Value: `^www\.google\.[a-z.]+$`},
				},
			},
		},
	}
	filename := writeGeoSiteList(t, list)

	got, err := Decode(filename, "google")
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	var decoded router.GeoSite
	if err := proto.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("decoded bytes are not a valid GeoSite message: %v", err)
	}
	if !proto.Equal(&decoded, list.Entry[1]) {
		t.Fatalf("decoded GeoSite mismatch:\n got: %v\nwant: %v", decoded.String(), list.Entry[1].String())
	}
}

func TestDecodeCodeNotFound(t *testing.T) {
	filename := writeGeoIPList(t, testGeoIPList())

	_, err := Decode(filename, "NOT-A-REAL-CODE")
	if !errors.Is(err, errCodeNotFound) {
		t.Fatalf("Decode error = %v, want %v", err, errCodeNotFound)
	}
}

func TestDecodeEmptyFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "empty.dat")
	if err := os.WriteFile(filename, nil, 0o644); err != nil {
		t.Fatalf("failed to write empty file: %v", err)
	}

	_, err := Decode(filename, "CN")
	if !errors.Is(err, errCodeNotFound) {
		t.Fatalf("Decode error = %v, want %v", err, errCodeNotFound)
	}
}

func TestDecodeInvalidFileHeader(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "invalid.dat")
	if err := os.WriteFile(filename, []byte("this is not a geodata file"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := Decode(filename, "CN")
	if !errors.Is(err, errInvalidGeodataFile) {
		t.Fatalf("Decode error = %v, want %v", err, errInvalidGeodataFile)
	}
}

func TestDecodeTruncatedMatchedRecord(t *testing.T) {
	list := testGeoIPList()
	data, err := proto.Marshal(list)
	if err != nil {
		t.Fatalf("failed to marshal GeoIPList: %v", err)
	}
	// The first entry (PRIVATE) is matched, then the file stops inside its body.
	// The length prefix of the first entry is one byte; keep the 0A tag, the
	// length byte and the inner tag + code length byte + code bytes, then stop.
	truncated := data[:len(data)-1]
	filename := filepath.Join(t.TempDir(), "truncated.dat")
	if err := os.WriteFile(filename, truncated, 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err = Decode(filename, "MULTI")
	if !errors.Is(err, errFailedToReadExpectedLenBytes) && !errors.Is(err, errFailedToReadBytes) {
		t.Fatalf("Decode error = %v, want %v or %v", err, errFailedToReadExpectedLenBytes, errFailedToReadBytes)
	}
}

// TestDecodeRealGeoSite decodes a real geodata list when one is available in the
// repository root. The file is untracked, so the test skips when it is absent.
func TestDecodeRealGeoSite(t *testing.T) {
	const geoFile = "../../../GeoSite.dat"
	if _, err := os.Stat(geoFile); err != nil {
		t.Skipf("%s is not available: %v", geoFile, err)
	}

	const code = "google"
	got, err := Decode(geoFile, code)
	if err != nil {
		t.Fatalf("Decode(%q) returned error: %v", code, err)
	}
	var decoded router.GeoSite
	if err := proto.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("decoded bytes are not a valid GeoSite message: %v", err)
	}
	if !strings.EqualFold(decoded.CountryCode, code) {
		t.Fatalf("decoded country code = %q, want %q", decoded.CountryCode, code)
	}
	if len(decoded.Domain) == 0 {
		t.Fatal("decoded GeoSite has no domains")
	}
}

func BenchmarkDecodeRealGeoSite(b *testing.B) {
	const geoFile = "../../../GeoSite.dat"
	if _, err := os.Stat(geoFile); err != nil {
		b.Skipf("%s is not available: %v", geoFile, err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Decode(geoFile, "google"); err != nil {
			b.Fatal(err)
		}
	}
}
