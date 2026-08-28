package sing_tun

import (
	"net/netip"
	"testing"
)

func typicalDNSHijackHandler() *ListenerHandler {
	h := &ListenerHandler{
		DnsAddrPorts: []netip.AddrPort{
			netip.MustParseAddrPort("0.0.0.0:53"),
			netip.MustParseAddrPort("198.18.0.1:53"),
			netip.MustParseAddrPort("[fdfe:dcba:9876::1]:53"),
		},
	}
	h.prepareDNSHijack()
	return h
}

func TestShouldHijackDns(t *testing.T) {
	h := typicalDNSHijackHandler()

	if !h.ShouldHijackDns(netip.MustParseAddrPort("1.1.1.1:53")) {
		t.Fatal("unspecified 0.0.0.0:53 must hijack any destination port 53")
	}
	if h.ShouldHijackDns(netip.MustParseAddrPort("1.1.1.1:443")) {
		t.Fatal("default TUN hijack must not match non-DNS ports")
	}
	if !h.ShouldHijackDns(netip.MustParseAddrPort("198.18.0.1:53")) {
		t.Fatal("exact TUN gateway:53 must match")
	}

	exact := &ListenerHandler{
		DnsAddrPorts: []netip.AddrPort{netip.MustParseAddrPort("8.8.8.8:53")},
	}
	exact.prepareDNSHijack()
	if !exact.ShouldHijackDns(netip.MustParseAddrPort("8.8.8.8:53")) {
		t.Fatal("exact 8.8.8.8:53 must match")
	}
	if exact.ShouldHijackDns(netip.MustParseAddrPort("1.1.1.1:53")) {
		t.Fatal("exact hijack must not wildcard other DNS destinations")
	}
	if exact.ShouldHijackDns(netip.MustParseAddrPort("8.8.8.8:443")) {
		t.Fatal("exact hijack must not match a different port")
	}

	mdns := &ListenerHandler{
		DnsAddrPorts: []netip.AddrPort{netip.MustParseAddrPort("0.0.0.0:5353")},
	}
	mdns.prepareDNSHijack()
	if !mdns.ShouldHijackDns(netip.MustParseAddrPort("0.0.0.0:5353")) {
		t.Fatal("unspecified 0.0.0.0:5353 must exact-match")
	}
	if !mdns.ShouldHijackDns(netip.MustParseAddrPort("1.1.1.1:53")) {
		t.Fatal("unspecified hijack address still wildcards destination port 53")
	}
	if mdns.ShouldHijackDns(netip.MustParseAddrPort("1.1.1.1:5353")) {
		t.Fatal("unspecified non-53 hijack must not wildcard other hosts on that port")
	}

	empty := &ListenerHandler{}
	empty.prepareDNSHijack()
	if empty.ShouldHijackDns(netip.MustParseAddrPort("1.1.1.1:53")) {
		t.Fatal("empty hijack list must match nothing")
	}

	unprepared := &ListenerHandler{
		DnsAddrPorts: []netip.AddrPort{netip.MustParseAddrPort("0.0.0.0:53")},
	}
	if !unprepared.ShouldHijackDns(netip.MustParseAddrPort("1.1.1.1:53")) {
		t.Fatal("unprepared handler must still scan DnsAddrPorts")
	}
}

var hijackSink bool

func BenchmarkShouldHijackDns(b *testing.B) {
	h := typicalDNSHijackHandler()
	miss := netip.MustParseAddrPort("1.1.1.1:443")
	hit := netip.MustParseAddrPort("1.1.1.1:53")

	b.Run("miss", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			hijackSink = h.ShouldHijackDns(miss)
		}
	})
	b.Run("hit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			hijackSink = h.ShouldHijackDns(hit)
		}
	})
}
