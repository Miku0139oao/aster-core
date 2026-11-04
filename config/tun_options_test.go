package config

import (
	"fmt"
	"strings"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

func TestParseTunDNSModeValues(t *testing.T) {
	for _, dnsMode := range []string{"", "disabled", "native", "hijack"} {
		t.Run("value="+dnsMode, func(t *testing.T) {
			general := &General{}
			err := parseTun(RawTun{DNSMode: dnsMode}, &DNS{}, general)
			if err != nil {
				t.Fatalf("parseTun rejected dns-mode %q: %v", dnsMode, err)
			}
			if general.Tun.DNSMode != dnsMode {
				t.Fatalf("dns-mode %q was not propagated to listener config: %q", dnsMode, general.Tun.DNSMode)
			}
		})
	}
}

func TestParseTunDNSModeRejectsUnknownValue(t *testing.T) {
	for _, dnsMode := range []string{"bogus", "Hijack", "hijacked", "system"} {
		t.Run("value="+dnsMode, func(t *testing.T) {
			general := &General{}
			err := parseTun(RawTun{DNSMode: dnsMode}, &DNS{}, general)
			if err == nil {
				t.Fatalf("parseTun accepted invalid dns-mode %q", dnsMode)
			}
			if !strings.Contains(err.Error(), "invalid tun dns-mode") {
				t.Fatalf("unexpected error for dns-mode %q: %v", dnsMode, err)
			}
			if general.Tun.DNSMode != "" {
				t.Fatalf("invalid dns-mode %q leaked into listener config: %q", dnsMode, general.Tun.DNSMode)
			}
		})
	}
}

func TestValidateTunDNSModeAcceptsOnlyKnownValues(t *testing.T) {
	if err := ValidateTunDNSMode(""); err != nil {
		t.Fatalf("empty dns-mode must be accepted (sing-tun legacy default): %v", err)
	}
	for _, dnsMode := range []string{"disabled", "native", "hijack"} {
		if err := ValidateTunDNSMode(dnsMode); err != nil {
			t.Fatalf("dns-mode %q must be accepted: %v", dnsMode, err)
		}
	}
	for _, dnsMode := range []string{"disabled ", "HIJACK", "off"} {
		if err := ValidateTunDNSMode(dnsMode); err == nil {
			t.Fatalf("dns-mode %q must be rejected", dnsMode)
		}
	}
}

func TestParseTunCongestionControllerAndProcessorsPerChannel(t *testing.T) {
	general := &General{}
	err := parseTun(RawTun{
		CongestionController: "bbr",
		ProcessorsPerChannel: 3,
	}, &DNS{}, general)
	if err != nil {
		t.Fatal(err)
	}
	if general.Tun.CongestionController != "bbr" {
		t.Fatalf("congestion-controller was not propagated: %q", general.Tun.CongestionController)
	}
	if general.Tun.ProcessorsPerChannel != 3 {
		t.Fatalf("processors-per-channel was not propagated: %d", general.Tun.ProcessorsPerChannel)
	}
}

func TestParseTunCongestionControllerValidation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, tc := range []struct {
			stack   string
			control string
			wantErr bool
		}{
			{stack: "mips", control: ""},
			{stack: "mips", control: "BbR3"},
			{stack: "mips", control: "not-a-controller", wantErr: true},
			{stack: "mips", control: " cubic ", wantErr: true},
			{stack: "gvisor", control: "not-a-controller"},
			{stack: "system", control: "not-a-controller"},
			{stack: "mixed", control: "not-a-controller"},
		} {
			t.Run(fmt.Sprintf("%s/%q/enabled=%t", tc.stack, tc.control, enabled), func(t *testing.T) {
				body := fmt.Sprintf("tun:\n  enable: %t\n  stack: %s\n  congestion-controller: %q\n", enabled, tc.stack, tc.control)
				raw, err := UnmarshalRawConfig([]byte(body))
				if err != nil {
					t.Fatal(err)
				}
				general := &General{}
				err = parseTun(raw.Tun, &DNS{}, general)
				if tc.wantErr {
					if err == nil || !strings.Contains(err.Error(), "invalid tun congestion-controller") {
						t.Fatalf("expected congestion-controller validation error, got %v", err)
					}
					if general.Tun.CongestionController != "" || general.Tun.Enable {
						t.Fatal("invalid settings leaked into the parsed TUN config")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if general.Tun.CongestionController != tc.control || general.Tun.Enable != enabled {
					t.Fatalf("TUN config was changed during validation: %+v", general.Tun)
				}
			})
		}
	}
}

func TestDefaultRawTunTunOptions(t *testing.T) {
	rawTun := DefaultRawConfig().Tun
	// Aster deliberately keeps gVisor as the default stack even though upstream
	// mihomo flipped its default to mips (63bd52ec). Kernel DIRECT release builds
	// and all of its testing are gVisor-based.
	if rawTun.Stack != C.TunGvisor {
		t.Fatalf("aster default tun stack must stay gVisor, got %s", rawTun.Stack)
	}
	if rawTun.DNSMode != "" {
		t.Fatalf("dns-mode must default to the legacy empty value, got %q", rawTun.DNSMode)
	}
	if rawTun.CongestionController != "" {
		t.Fatalf("congestion-controller must default to empty, got %q", rawTun.CongestionController)
	}
	if rawTun.ProcessorsPerChannel != 1 {
		t.Fatalf("processors-per-channel must default to 1, got %d", rawTun.ProcessorsPerChannel)
	}
}

func TestUnmarshalRawConfigTunNewOptions(t *testing.T) {
	rawCfg, err := UnmarshalRawConfig([]byte(`
tun:
  enable: true
  stack: mips
  dns-mode: native
  congestion-controller: bbr3
  processors-per-channel: 4
`))
	if err != nil {
		t.Fatal(err)
	}
	if rawCfg.Tun.Stack != C.TunMips {
		t.Fatalf("stack: mips did not parse, got %s (%d)", rawCfg.Tun.Stack, rawCfg.Tun.Stack)
	}
	if rawCfg.Tun.DNSMode != "native" {
		t.Fatalf("dns-mode did not parse: %q", rawCfg.Tun.DNSMode)
	}
	if rawCfg.Tun.CongestionController != "bbr3" {
		t.Fatalf("congestion-controller did not parse: %q", rawCfg.Tun.CongestionController)
	}
	if rawCfg.Tun.ProcessorsPerChannel != 4 {
		t.Fatalf("processors-per-channel did not parse: %d", rawCfg.Tun.ProcessorsPerChannel)
	}
}

func TestUnmarshalRawConfigTunUnknownDNSModeRoundTripsToParseTun(t *testing.T) {
	rawCfg, err := UnmarshalRawConfig([]byte("tun:\n  dns-mode: bogus\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rawCfg.Tun.DNSMode != "bogus" {
		t.Fatalf("dns-mode did not parse into RawTun: %q", rawCfg.Tun.DNSMode)
	}
	general := &General{}
	if err := parseTun(rawCfg.Tun, &DNS{}, general); err == nil {
		t.Fatal("parseTun must reject the unknown dns-mode that UnmarshalRawConfig accepted")
	}
}
