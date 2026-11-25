package config

import (
	"fmt"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

func TestParseDNSFakeIPDisableMapping(t *testing.T) {
	for _, disableMapping := range []bool{false, true} {
		t.Run(fmt.Sprintf("disable-mapping=%t", disableMapping), func(t *testing.T) {
			dnsCfg, err := parseDNS(&RawConfig{DNS: RawDNS{
				Enable:               true,
				NameServer:           []string{"https://doh.pub/dns-query"},
				DefaultNameserver:    []string{"114.114.114.114"},
				EnhancedMode:         C.DNSFakeIP,
				FakeIPRange:          "198.18.0.1/16",
				FakeIPDisableMapping: disableMapping,
			}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if dnsCfg.FakeIPDisableMapping != disableMapping {
				t.Fatalf("fake-ip-disable-mapping = %v, want %v", dnsCfg.FakeIPDisableMapping, disableMapping)
			}
		})
	}
}

func TestUnmarshalRawConfigFakeIPDisableMapping(t *testing.T) {
	rawCfg, err := UnmarshalRawConfig([]byte("dns:\n  fake-ip-disable-mapping: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !rawCfg.DNS.FakeIPDisableMapping {
		t.Fatal("fake-ip-disable-mapping did not parse into RawDNS")
	}
}
