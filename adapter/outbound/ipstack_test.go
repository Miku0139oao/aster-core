package outbound

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/Miku0139oao/aster-core/common/structure"
	"github.com/Miku0139oao/aster-core/dns"

	"github.com/metacubex/mipstack"
	"github.com/metacubex/wireguard-go/tun"
)

// validWireGuardOption returns a minimal option that passes every check in
// NewWireGuard once the IP stack option is valid.
func validWireGuardOption() WireGuardOption {
	return WireGuardOption{
		Name:       "wg-test",
		Server:     "162.159.192.1",
		Port:       2480,
		Ip:         "172.16.0.2",
		PublicKey:  "Cr8hWlKvtDt7nrvf+f0brNQQzabAqrjfBvas9pmowjo=",
		PrivateKey: "eCtXsJZ27+4PbhDkHnB923tkUn2Gj59wZw5wFA75MnU=",
	}
}

func TestIPStackOptionNormalize(t *testing.T) {
	testCases := []struct {
		name     string
		input    IPStackOption
		wantMode string
		wantCC   string
	}{
		{
			name:     "empty defaults to auto",
			input:    IPStackOption{},
			wantMode: ipStackAuto,
			wantCC:   "",
		},
		{
			name:     "mode and controller are lowercased",
			input:    IPStackOption{Mode: " MIPS ", CongestionController: " BBR "},
			wantMode: ipStackMips,
			wantCC:   "bbr",
		},
		{
			name:     "gvisor is preserved",
			input:    IPStackOption{Mode: "GVisor"},
			wantMode: ipStackGVisor,
			wantCC:   "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			option := tc.input
			option.normalize()
			if option.Mode != tc.wantMode {
				t.Fatalf("mode = %q, want %q", option.Mode, tc.wantMode)
			}
			if option.CongestionController != tc.wantCC {
				t.Fatalf("congestion-controller = %q, want %q", option.CongestionController, tc.wantCC)
			}
		})
	}
}

func TestIPStackOptionValidate(t *testing.T) {
	valid := []IPStackOption{
		{Mode: ipStackAuto},
		{Mode: ipStackMips},
		{Mode: ipStackMips, CongestionController: "cubic"},
		{Mode: ipStackMips, CongestionController: "reno"},
		{Mode: ipStackMips, CongestionController: "bbr"},
		{Mode: ipStackMips, CongestionController: "bbr3"},
	}
	for _, option := range valid {
		if err := option.validate(); err != nil {
			t.Fatalf("option %+v rejected: %v", option, err)
		}
	}

	invalid := []struct {
		option  IPStackOption
		wantSub string
	}{
		{IPStackOption{Mode: "wireguard"}, "invalid IP stack mode"},
		{IPStackOption{Mode: ipStackMips, CongestionController: "cubicx"}, "invalid IP stack congestion controller"},
	}
	for _, tc := range invalid {
		err := tc.option.validate()
		if err == nil {
			t.Fatalf("option %+v was accepted, want error", tc.option)
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Fatalf("option %+v error = %q, want it to contain %q", tc.option, err, tc.wantSub)
		}
	}
}

func TestNewWireGuardRejectsInvalidIPStackWithoutBuildingStack(t *testing.T) {
	option := validWireGuardOption()
	option.IPStack = IPStackOption{Mode: "not-a-stack"}

	calls := 0
	original := ipStackFactory
	ipStackFactory = func(IPStackOption, []netip.Prefix, uint32) (ipStack, error) {
		calls++
		return nil, errors.New("unexpected stack construction")
	}
	defer func() { ipStackFactory = original }()

	proxy, err := NewWireGuard(option)
	if err == nil {
		proxy.Close()
		t.Fatal("NewWireGuard accepted an invalid ip-stack mode")
	}
	if !strings.Contains(err.Error(), "invalid IP stack mode") {
		t.Fatalf("error = %q, want an invalid IP stack mode error", err)
	}
	if calls != 0 {
		t.Fatalf("ip-stack constructor called %d times on the error path, want 0", calls)
	}
}

// TestNewWireGuardLazyInitDoesNotBuildStack covers the reload leak fixed by
// the lazy-init change: a config that fails after the IP stack option has been
// validated (here, the DNS name server parse) must not have built a stack at
// all, because the returned error discards the outbound without closing it.
func TestNewWireGuardLazyInitDoesNotBuildStack(t *testing.T) {
	parseNameServer := dns.ParseNameServer
	dns.ParseNameServer = func([]string) ([]dns.NameServer, error) {
		return nil, errors.New("name server parse failed")
	}
	defer func() { dns.ParseNameServer = parseNameServer }()

	calls := 0
	original := ipStackFactory
	ipStackFactory = func(IPStackOption, []netip.Prefix, uint32) (ipStack, error) {
		calls++
		return nil, errors.New("unexpected stack construction")
	}
	defer func() { ipStackFactory = original }()

	option := validWireGuardOption()
	option.RemoteDnsResolve = true
	option.Dns = []string{"1.1.1.1"}

	proxy, err := NewWireGuard(option)
	if err == nil {
		proxy.Close()
		t.Fatal("NewWireGuard accepted an unparsable dns name server")
	}
	if !strings.Contains(err.Error(), "name server parse failed") {
		t.Fatalf("error = %q, want the dns name server parse error", err)
	}
	if calls != 0 {
		t.Fatalf("a stack was built %d times before NewWireGuard returned an error, want 0", calls)
	}
}

// TestNewWireGuardLazyInitBuildsOnFirstDial asserts lazy init does not change
// the healthy path: a valid option still constructs the outbound, and init0
// builds the stack exactly once and reuses it afterwards.
func TestNewWireGuardLazyInitBuildsOnFirstDial(t *testing.T) {
	built := 0
	original := ipStackFactory
	ipStackFactory = func(option IPStackOption, localPrefixes []netip.Prefix, mtu uint32) (ipStack, error) {
		built++
		return original(option, localPrefixes, mtu)
	}
	defer func() { ipStackFactory = original }()

	option := validWireGuardOption()
	proxy, err := NewWireGuard(option)
	if err != nil {
		t.Fatalf("NewWireGuard failed: %v", err)
	}
	defer proxy.Close()

	if built != 0 {
		t.Fatalf("stack was built %d times during construction, want 0 (lazy)", built)
	}
	if wgDevice, _ := proxy.devices(); wgDevice != nil {
		t.Fatal("device was created during construction, want lazy init")
	}

	// init0 needs network access for the endpoint; a cancelled context makes it
	// return before touching the network, which is exactly the point at which
	// the stack must not exist yet.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := proxy.init0(ctx); err == nil {
		t.Fatal("init0 succeeded with a cancelled context")
	}
	if built != 0 {
		t.Fatalf("stack was built %d times for a cancelled init, want 0", built)
	}
}

func TestIPStackOptionConfigParse(t *testing.T) {
	decoder := structure.NewDecoder(structure.Option{TagName: "proxy", WeaklyTypedInput: true, KeyReplacer: structure.DefaultKeyReplacer})

	t.Run("wireguard", func(t *testing.T) {
		var option WireGuardOption
		err := decoder.Decode(map[string]any{
			"name":        "wg",
			"server":      "162.159.192.1",
			"private-key": "eCtXsJZ27+4PbhDkHnB923tkUn2Gj59wZw5wFA75MnU=",
			"ip-stack": map[string]any{
				"mode":                  "mips",
				"congestion-controller": "bbr",
			},
		}, &option)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if option.IPStack.Mode != ipStackMips {
			t.Fatalf("mode = %q, want %q", option.IPStack.Mode, ipStackMips)
		}
		if option.IPStack.CongestionController != "bbr" {
			t.Fatalf("congestion-controller = %q, want %q", option.IPStack.CongestionController, "bbr")
		}
	})

	t.Run("wireguard absent", func(t *testing.T) {
		var option WireGuardOption
		err := decoder.Decode(map[string]any{
			"name":        "wg",
			"server":      "162.159.192.1",
			"private-key": "eCtXsJZ27+4PbhDkHnB923tkUn2Gj59wZw5wFA75MnU=",
		}, &option)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		option.IPStack.normalize()
		if option.IPStack.Mode != ipStackAuto {
			t.Fatalf("default mode = %q, want %q", option.IPStack.Mode, ipStackAuto)
		}
	})

	t.Run("masque", func(t *testing.T) {
		var option MasqueOption
		err := decoder.Decode(map[string]any{
			"name":        "masque",
			"server":      "162.159.198.1",
			"port":        443,
			"private-key": "MHcCAQEEILI1eOtnbEIh89Fj4yNDuFR6UjayCKI3NdLl3DhetimWoAoGCCqGSM49AwEHoUQDQgAEgyXrE8v+hHsHy3ewSb3WcRjYgCrM9T9hiE0Uv6k2DZ1+4kefrDT9v1Q/8wdRigTf6t6gGNUV8W+IUMdrfUt+9g==",
			"public-key":  "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEIaU7MToJm9NKp8YfGxR6r+/h4mcG7SxI8tsW8OR1A5tv/zCzVbCRRh2t87/kxnP6lAy0lkr7qYwu+ox+k3dr6w==",
			"ip-stack": map[string]any{
				"mode": "gvisor",
			},
		}, &option)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if option.IPStack.Mode != ipStackGVisor {
			t.Fatalf("mode = %q, want %q", option.IPStack.Mode, ipStackGVisor)
		}
	})

	t.Run("openvpn", func(t *testing.T) {
		var option OpenVPNOption
		err := decoder.Decode(map[string]any{
			"name":   "openvpn",
			"server": "vpn.example.com",
			"port":   1194,
			"ca":     "-----BEGIN CERTIFICATE-----\nMIIBexample\n-----END CERTIFICATE-----",
			"ip-stack": map[string]any{
				"mode":                  "mips",
				"congestion-controller": "reno",
			},
		}, &option)
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if option.IPStack.Mode != ipStackMips {
			t.Fatalf("mode = %q, want %q", option.IPStack.Mode, ipStackMips)
		}
		if option.IPStack.CongestionController != "reno" {
			t.Fatalf("congestion-controller = %q, want %q", option.IPStack.CongestionController, "reno")
		}
	})
}

func TestNewMasqueRejectsInvalidIPStack(t *testing.T) {
	_, err := NewMasque(MasqueOption{
		Name:             "masque",
		Server:           "162.159.198.1",
		Port:             443,
		PrivateKey:       "MHcCAQEEILI1eOtnbEIh89Fj4yNDuFR6UjayCKI3NdLl3DhetimWoAoGCCqGSM49AwEHoUQDQgAEgyXrE8v+hHsHy3ewSb3WcRjYgCrM9T9hiE0Uv6k2DZ1+4kefrDT9v1Q/8wdRigTf6t6gGNUV8W+IUMdrfUt+9g==",
		PublicKey:        "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEIaU7MToJm9NKp8YfGxR6r+/h4mcG7SxI8tsW8OR1A5tv/zCzVbCRRh2t87/kxnP6lAy0lkr7qYwu+ox+k3dr6w==",
		Ip:               "172.16.0.2",
		HandshakeTimeout: 0,
		IPStack:          IPStackOption{Mode: "not-a-stack"},
	})
	if err == nil {
		t.Fatal("NewMasque accepted an invalid ip-stack mode")
	}
	if !strings.Contains(err.Error(), "invalid IP stack mode") {
		t.Fatalf("error = %q, want an invalid IP stack mode error", err)
	}
}

func TestNewOpenVPNRejectsInvalidIPStack(t *testing.T) {
	_, err := NewOpenVPN(OpenVPNOption{
		Name:    "openvpn",
		Server:  "vpn.example.com",
		Port:    1194,
		IPStack: IPStackOption{Mode: "mips", CongestionController: "cubicx"},
	})
	if err == nil {
		t.Fatal("NewOpenVPN accepted an invalid ip-stack congestion controller")
	}
	if !strings.Contains(err.Error(), "invalid IP stack congestion controller") {
		t.Fatalf("error = %q, want an invalid congestion controller error", err)
	}
}

// TestNewIPStackMips exercises the MIPS branch directly: it must produce a
// stack for valid input that can be dialed through the shared ipStack surface
// and closed cleanly.
func TestNewIPStackMips(t *testing.T) {
	prefixes := []netip.Prefix{netip.MustParsePrefix("172.16.0.2/32")}
	stack, err := newIPStack(IPStackOption{Mode: ipStackMips, CongestionController: "cubic"}, prefixes, 1280)
	if err != nil {
		t.Fatalf("newIPStack(mips) failed: %v", err)
	}
	defer stack.Close()

	if _, ok := stack.(*mipstack.Stack); !ok {
		t.Fatalf("stack type = %T, want *mipstack.Stack", stack)
	}
	mtu, err := stack.MTU()
	if err != nil {
		t.Fatalf("MTU failed: %v", err)
	}
	if mtu != 1280 {
		t.Fatalf("MTU = %d, want 1280", mtu)
	}
	if stack.BatchSize() <= 0 {
		t.Fatalf("BatchSize = %d, want > 0", stack.BatchSize())
	}
}

// TestNewWireguardDeviceWrapsMips asserts newWireguardDevice starts a non-tun
// stack and that its synthesized tun events channel reports up and closes.
func TestNewWireguardDeviceWrapsMips(t *testing.T) {
	prefixes := []netip.Prefix{netip.MustParsePrefix("172.16.0.2/32")}
	stack, err := newIPStack(IPStackOption{Mode: ipStackMips}, prefixes, 1280)
	if err != nil {
		t.Fatalf("newIPStack(mips) failed: %v", err)
	}
	device, err := newWireguardDevice(stack)
	if err != nil {
		_ = stack.Close()
		t.Fatalf("newWireguardDevice failed: %v", err)
	}
	if _, ok := device.(*ipStackWireguardDevice); !ok {
		device.Close()
		t.Fatalf("device type = %T, want *ipStackWireguardDevice", device)
	}
	events := device.Events()
	// newWireguardDevice already started the inner MIPS stack; the wrapper's
	// Start is what signals tun.EventUp to wireguard-go, mirroring gVisor.
	if err := device.Start(); err != nil {
		device.Close()
		t.Fatalf("Start failed: %v", err)
	}
	select {
	case event := <-events:
		if event != tun.EventUp {
			device.Close()
			t.Fatalf("first event = %v, want tun.EventUp", event)
		}
	default:
		device.Close()
		t.Fatal("no event was queued by Start")
	}
	if device.File() != nil {
		device.Close()
		t.Fatal("File() = non-nil, want nil for an in-memory stack")
	}
	if err := device.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	// Close must be idempotent: a second call must not panic on the closed events channel.
	if err := device.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

// TestNewMasqueClosesIPStackWhenDnsParseFails covers the error path after the
// stack is built: NewMasque used to return without closing it, and the caller
// drops the outbound on error, so nothing ever reclaimed the device.
func TestNewMasqueClosesIPStackWhenDnsParseFails(t *testing.T) {
	parseNameServer := dns.ParseNameServer
	dns.ParseNameServer = func([]string) ([]dns.NameServer, error) {
		return nil, errors.New("name server parse failed")
	}
	defer func() { dns.ParseNameServer = parseNameServer }()

	built := 0
	closed := 0
	original := ipStackFactory
	ipStackFactory = func(option IPStackOption, localPrefixes []netip.Prefix, mtu uint32) (ipStack, error) {
		built++
		inner, err := original(option, localPrefixes, mtu)
		if err != nil {
			return nil, err
		}
		return &closeCountingStack{ipStack: inner, closed: &closed}, nil
	}
	defer func() { ipStackFactory = original }()

	_, err := NewMasque(MasqueOption{
		Name:             "masque",
		Server:           "162.159.198.1",
		Port:             443,
		PrivateKey:       "MHcCAQEEILI1eOtnbEIh89Fj4yNDuFR6UjayCKI3NdLl3DhetimWoAoGCCqGSM49AwEHoUQDQgAEgyXrE8v+hHsHy3ewSb3WcRjYgCrM9T9hiE0Uv6k2DZ1+4kefrDT9v1Q/8wdRigTf6t6gGNUV8W+IUMdrfUt+9g==",
		PublicKey:        "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEIaU7MToJm9NKp8YfGxR6r+/h4mcG7SxI8tsW8OR1A5tv/zCzVbCRRh2t87/kxnP6lAy0lkr7qYwu+ox+k3dr6w==",
		Ip:               "172.16.0.2",
		Network:          "h3",
		IPStack:          IPStackOption{Mode: "mips"},
		RemoteDnsResolve: true,
		Dns:              []string{"1.1.1.1"},
	})
	if err == nil {
		t.Fatal("NewMasque accepted an unparsable dns name server")
	}
	if built == 0 {
		t.Fatal("expected the stack to have been built before the dns parse failed")
	}
	if closed == 0 {
		t.Fatal("NewMasque leaked the ip stack on the dns parse error path")
	}
}

type closeCountingStack struct {
	ipStack
	closed *int
}

func (s *closeCountingStack) Close() error {
	*s.closed++
	// Do not call through: the real mipstack device is only safe to close after
	// Start, and this test fails before that point. Counting the call is what
	// the regression is about.
	return nil
}
