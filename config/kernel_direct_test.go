package config

import (
	"net/netip"
	"testing"
)

func TestParseTunKernelDirectRequirements(t *testing.T) {
	for _, rawTun := range []RawTun{
		{KernelDirect: true},
		{KernelDirect: true, AutoRoute: true},
		{KernelDirect: true, AutoRedirect: true},
	} {
		if err := parseTun(rawTun, &DNS{}, &General{}); err == nil {
			t.Fatalf("expected kernel-direct dependency error for %+v", rawTun)
		}
	}

	general := &General{}
	err := parseTun(RawTun{KernelDirect: true, KernelDirectMaxEntries: 2048, AutoRoute: true, AutoRedirect: true}, &DNS{}, general)
	if err != nil {
		t.Fatal(err)
	}
	if !general.Tun.KernelDirect {
		t.Fatal("kernel-direct was not propagated to listener config")
	}
	if general.Tun.KernelDirectMaxEntries != 2048 {
		t.Fatalf("kernel-direct-max-entries was not propagated: %d", general.Tun.KernelDirectMaxEntries)
	}
}

func TestParseTunKernelDirectMaxEntriesContract(t *testing.T) {
	tests := []struct {
		name       string
		configured uint32
		want       uint32
		wantError  bool
	}{
		{name: "zero defaults and persists", configured: 0, want: 4096},
		{name: "explicit value persists", configured: 2048, want: 2048},
		{name: "maximum persists", configured: 65536, want: 65536},
		{name: "over maximum errors", configured: 65537, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			general := &General{}
			err := parseTun(RawTun{
				KernelDirect:           true,
				KernelDirectMaxEntries: test.configured,
				AutoRoute:              true,
				AutoRedirect:           true,
			}, &DNS{}, general)
			if test.wantError {
				if err == nil {
					t.Fatalf("parseTun accepted kernel-direct-max-entries %d; config/config.go must reject values over 65536", test.configured)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTun rejected kernel-direct-max-entries %d: %v", test.configured, err)
			}
			if got := general.Tun.KernelDirectMaxEntries; got != test.want {
				t.Fatalf("parseTun persisted kernel-direct-max-entries %d, want %d; update config/config.go default propagation", got, test.want)
			}
		})
	}
}

func TestParseTunKernelDirectEBPFRequirementsAndPropagation(t *testing.T) {
	base := RawTun{KernelDirect: true, AutoRoute: true, AutoRedirect: true}
	for _, mutate := range []func(*RawTun){
		func(tun *RawTun) {
			tun.KernelDirect = false
			tun.KernelDirectEBPF = true
			tun.KernelDirectEBPFInterfaces = []string{"br-lan"}
		},
		func(tun *RawTun) { tun.KernelDirectEBPF = true },
		func(tun *RawTun) { tun.KernelDirectEBPFRequired = true },
		func(tun *RawTun) { tun.KernelDirectEBPFProxy = true },
		func(tun *RawTun) { tun.KernelDirectEBPFProxyRedirect = true },
		func(tun *RawTun) {
			tun.KernelDirectEBPF = true
			tun.KernelDirectEBPFInterfaces = []string{"br-lan"}
			tun.KernelDirectEBPFProxyPrefixes = []netip.Prefix{netip.MustParsePrefix("1.1.1.0/24")}
		},
		func(tun *RawTun) {
			tun.KernelDirectEBPFDirectPrefixes = []netip.Prefix{netip.MustParsePrefix("8.8.8.0/24")}
		},
	} {
		raw := base
		mutate(&raw)
		if err := parseTun(raw, &DNS{}, &General{}); err == nil {
			t.Fatalf("expected eBPF dependency error for %+v", raw)
		}
	}

	raw := base
	raw.KernelDirectEBPF = true
	raw.KernelDirectEBPFRequired = true
	raw.KernelDirectEBPFInterfaces = []string{"br-lan"}
	raw.KernelDirectEBPFMark = 0x40000000
	raw.KernelDirectEBPFMaxEntries = 4096
	raw.KernelDirectEBPFProxy = true
	raw.KernelDirectEBPFProxyRedirect = true
	raw.KernelDirectEBPFProxyMark = 0x20000000
	raw.KernelDirectEBPFFlowEntries = 8192
	raw.KernelDirectEBPFDirectPrefixes = []netip.Prefix{netip.MustParsePrefix("8.8.8.0/24")}
	raw.KernelDirectEBPFProxyPrefixes = []netip.Prefix{netip.MustParsePrefix("1.1.1.0/24")}
	general := &General{}
	if err := parseTun(raw, &DNS{}, general); err != nil {
		t.Fatal(err)
	}
	if !general.Tun.KernelDirectEBPF || !general.Tun.KernelDirectEBPFRequired {
		t.Fatal("eBPF flags were not propagated")
	}
	if len(general.Tun.KernelDirectEBPFInterfaces) != 1 || general.Tun.KernelDirectEBPFInterfaces[0] != "br-lan" {
		t.Fatalf("unexpected interfaces: %v", general.Tun.KernelDirectEBPFInterfaces)
	}
	if general.Tun.KernelDirectEBPFMark != raw.KernelDirectEBPFMark || general.Tun.KernelDirectEBPFMaxEntries != raw.KernelDirectEBPFMaxEntries {
		t.Fatal("eBPF sizing/mark options were not propagated")
	}
	if !general.Tun.KernelDirectEBPFProxy || !general.Tun.KernelDirectEBPFProxyRedirect || general.Tun.KernelDirectEBPFProxyMark != raw.KernelDirectEBPFProxyMark || general.Tun.KernelDirectEBPFFlowEntries != raw.KernelDirectEBPFFlowEntries {
		t.Fatal("eBPF proxy/flow options were not propagated")
	}
	if len(general.Tun.KernelDirectEBPFDirectPrefixes) != 1 || general.Tun.KernelDirectEBPFDirectPrefixes[0].String() != "8.8.8.0/24" {
		t.Fatalf("eBPF direct prefixes were not propagated: %v", general.Tun.KernelDirectEBPFDirectPrefixes)
	}
	if len(general.Tun.KernelDirectEBPFProxyPrefixes) != 1 || general.Tun.KernelDirectEBPFProxyPrefixes[0].String() != "1.1.1.0/24" {
		t.Fatalf("eBPF proxy prefixes were not propagated: %v", general.Tun.KernelDirectEBPFProxyPrefixes)
	}
}
