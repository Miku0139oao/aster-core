package config

import (
	"net/netip"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

func TestTunCloneDetachesAndSortCanonicalizesAllSlices(t *testing.T) {
	original := Tun{
		LoopbackAddress:     []netip.Addr{netip.MustParseAddr("2001:db8::2"), netip.MustParseAddr("192.0.2.2")},
		ExcludeSrcPort:      []uint16{443, 53},
		ExcludeSrcPortRange: []string{"8000:9000", "1000:2000"},
		ExcludeDstPort:      []uint16{8443, 80},
		ExcludeDstPortRange: []string{"9000:9999", "2000:2999"},
	}
	clone := original.Clone()
	clone.Sort()
	clone.ExcludeSrcPort[0] = 1
	if original.ExcludeSrcPort[0] != 443 {
		t.Fatal("Tun.Clone retained source-port backing storage")
	}
	if clone.LoopbackAddress[0] != netip.MustParseAddr("192.0.2.2") {
		t.Fatalf("loopback addresses not canonicalized: %v", clone.LoopbackAddress)
	}
}

func TestTunEqualChecksLoopbackAndPortFilters(t *testing.T) {
	base := Tun{
		LoopbackAddress:       []netip.Addr{netip.MustParseAddr("192.0.2.1")},
		ExcludeSrcPort:        []uint16{1},
		ExcludeSrcPortRange:   []string{"2:3"},
		ExcludeDstPort:        []uint16{4},
		ExcludeDstPortRange:   []string{"5:6"},
		DisableICMPForwarding: true,
	}
	tests := map[string]func(*Tun){
		"loopback":          func(tun *Tun) { tun.LoopbackAddress[0] = netip.MustParseAddr("192.0.2.2") },
		"source port":       func(tun *Tun) { tun.ExcludeSrcPort[0] = 7 },
		"source port range": func(tun *Tun) { tun.ExcludeSrcPortRange[0] = "7:8" },
		"dest port":         func(tun *Tun) { tun.ExcludeDstPort[0] = 9 },
		"dest port range":   func(tun *Tun) { tun.ExcludeDstPortRange[0] = "9:10" },
		"icmp forwarding":   func(tun *Tun) { tun.DisableICMPForwarding = false },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := base.Clone()
			mutate(&changed)
			if base.Equal(changed) {
				t.Fatalf("Tun.Equal ignored %s", name)
			}
		})
	}
}

func TestTunEqualChecksDNSModeCongestionAndProcessors(t *testing.T) {
	// dns-mode and processors-per-channel are reload-relevant: if Equal ignores
	// them, a PATCH or a config reload silently keeps the old TUN listener.
	// congestion-controller likewise only takes effect when the stack restarts.
	base := Tun{
		Enable:               true,
		DNSMode:              "native",
		CongestionController: "cubic",
		ProcessorsPerChannel: 1,
		Stack:                C.TunMips,
	}
	tests := map[string]func(*Tun){
		"dns mode":               func(tun *Tun) { tun.DNSMode = "disabled" },
		"dns mode to empty":      func(tun *Tun) { tun.DNSMode = "" },
		"congestion controller":  func(tun *Tun) { tun.CongestionController = "bbr" },
		"congestion to empty":    func(tun *Tun) { tun.CongestionController = "" },
		"processors per channel": func(tun *Tun) { tun.ProcessorsPerChannel = 4 },
		"stack mips to gvisor":   func(tun *Tun) { tun.Stack = C.TunGvisor },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			changed := base.Clone()
			mutate(&changed)
			if base.Equal(changed) {
				t.Fatalf("Tun.Equal ignored %s", name)
			}
		})
	}

	// A clone with no changes must still compare equal.
	if !base.Equal(base.Clone()) {
		t.Fatal("Tun.Equal reported a change for an identical clone")
	}
}

func TestTunStackMipsRoundTrips(t *testing.T) {
	if got := C.TunMips.String(); got != "Mips" {
		t.Fatalf("TunMips.String() = %q, want %q", got, "Mips")
	}
	var stack C.TUNStack
	if err := stack.UnmarshalText([]byte("mips")); err != nil {
		t.Fatalf("stack: mips was rejected: %v", err)
	}
	if stack != C.TunMips {
		t.Fatalf("stack: mips parsed to %v, want TunMips", stack)
	}
}
