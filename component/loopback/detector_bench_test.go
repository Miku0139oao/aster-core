package loopback

import (
	"net"
	"net/netip"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

func BenchmarkCheckConnMiss(b *testing.B) {
	detector := NewDetector()
	if detector == nil {
		b.Skip("loopback detector disabled")
	}
	metadata := &C.Metadata{
		SrcIP:   netip.MustParseAddr("192.168.1.128"),
		SrcPort: 54321,
		DstIP:   netip.MustParseAddr("1.1.1.1"),
		DstPort: 443,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := detector.CheckConn(metadata); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckConnNilDetector(b *testing.B) {
	var detector *Detector
	metadata := &C.Metadata{
		SrcIP:   netip.MustParseAddr("192.168.1.128"),
		SrcPort: 54321,
		DstIP:   netip.MustParseAddr("1.1.1.1"),
		DstPort: 443,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := detector.CheckConn(metadata); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNewConn(b *testing.B) {
	detector := NewDetector()
	if detector == nil {
		b.Skip("loopback detector disabled")
	}
	local := &net.TCPAddr{IP: net.IPv4(192, 168, 100, 101), Port: 54321}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wrapped := detector.NewConn(stubCConn{local: local})
		_ = wrapped.Close()
	}
}
