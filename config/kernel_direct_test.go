package config

import "testing"

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
	err := parseTun(RawTun{KernelDirect: true, AutoRoute: true, AutoRedirect: true}, &DNS{}, general)
	if err != nil {
		t.Fatal(err)
	}
	if !general.Tun.KernelDirect {
		t.Fatal("kernel-direct was not propagated to listener config")
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
}
