package iface

import (
	"net"
	"net/netip"
	"testing"
)

func firstUpInterfaceName(tb testing.TB) string {
	tb.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		tb.Fatalf("net.Interfaces: %v", err)
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp != 0 && ifi.Name != "" {
			return ifi.Name
		}
	}
	tb.Skip("no up interface")
	return ""
}

func BenchmarkResolveInterface(b *testing.B) {
	name := firstUpInterfaceName(b)
	if _, err := ResolveInterface(name); err != nil {
		b.Fatalf("warmup ResolveInterface(%q): %v", name, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ResolveInterface(name); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIsLocalIp(b *testing.B) {
	addr := netip.MustParseAddr("127.0.0.1")
	if _, err := IsLocalIp(addr); err != nil {
		b.Fatalf("warmup IsLocalIp: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := IsLocalIp(addr); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIsLocalIpParallel(b *testing.B) {
	addr := netip.MustParseAddr("127.0.0.1")
	if _, err := IsLocalIp(addr); err != nil {
		b.Fatalf("warmup IsLocalIp: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := IsLocalIp(addr); err != nil {
				b.Error(err)
			}
		}
	})
}
