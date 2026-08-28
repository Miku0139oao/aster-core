package process

import (
	"encoding/hex"
	"net/netip"
	"testing"
)

func TestIsPid(t *testing.T) {
	if !isPid("1") || !isPid("12345") {
		t.Fatal("expected numeric pid")
	}
	if isPid("") || isPid("1a") || isPid("12 ") || isPid("p") {
		t.Fatal("expected non-pid to be rejected")
	}
}

func TestParseHexIPv4(t *testing.T) {
	// 0100007F is 127.0.0.1 on little-endian hosts
	addr, err := parseHexIPv4("0100007F")
	if err != nil {
		t.Fatal(err)
	}
	want := netip.MustParseAddr("127.0.0.1")
	if littleEndian && addr != want {
		t.Fatalf("got %s want %s", addr, want)
	}
	addrPort, port, err := parseHexAddrPort("0100007F:0050", false)
	if err != nil {
		t.Fatal(err)
	}
	if port != 80 {
		t.Fatalf("port=%d", port)
	}
	if littleEndian && addrPort != want {
		t.Fatalf("got %s want %s", addrPort, want)
	}
}

func TestParseHexIPv6Loopback(t *testing.T) {
	s := "00000000000000000000000001000000"
	addr, err := parseHexIPv6(s)
	if err != nil {
		t.Fatal(err)
	}
	if littleEndian && !addr.IsLoopback() {
		t.Fatalf("got %s, want loopback", addr)
	}
}

func BenchmarkIsPid(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !isPid("12345") {
			b.Fatal()
		}
	}
}

func BenchmarkParseHexIPv4(b *testing.B) {
	const s = "0100007F"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := parseHexIPv4(s); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseHexIPv4DecodeString(b *testing.B) {
	const s = "0100007F"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := hex.DecodeString(s); err != nil {
			b.Fatal(err)
		}
	}
}
