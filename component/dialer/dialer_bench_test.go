package dialer

import (
	"context"
	"net"
	"net/netip"
	"syscall"
	"testing"

	"github.com/Miku0139oao/aster-core/component/keepalive"
	"github.com/Miku0139oao/aster-core/component/mptcp"
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

func BenchmarkApplyOptionsDirect(b *testing.B) {
	opts := []Option{
		WithInterface("eth0"),
		WithRoutingMark(0),
		WithTFO(false),
		WithMPTCP(false),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = applyOptions(opts...)
	}
}

func BenchmarkParseAddrIPv4(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ips, port, err := parseAddr(ctx, "tcp", "1.1.1.1:443", nil)
		if err != nil {
			b.Fatal(err)
		}
		if len(ips) != 1 || port != "443" {
			b.Fatalf("unexpected parse: %v %s", ips, port)
		}
	}
}

func BenchmarkAddControlToDialer(b *testing.B) {
	fn := func(ctx context.Context, network, address string, c syscall.RawConn) error {
		return nil
	}
	var d net.Dialer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d = net.Dialer{}
		addControlToDialer(&d, fn)
	}
	if d.ControlContext == nil {
		b.Fatal("ControlContext not installed")
	}
}

func BenchmarkBindIfaceToDialer(b *testing.B) {
	name := firstUpInterfaceName(b)
	dest := netip.MustParseAddr("1.1.1.1")
	var d net.Dialer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d = net.Dialer{}
		if err := bindIfaceToDialer(name, &d, "tcp4", dest); err != nil {
			b.Fatal(err)
		}
	}
	if d.ControlContext == nil {
		b.Fatal("bind control not installed")
	}
}

func BenchmarkDialSetup(b *testing.B) {
	ifaceName := firstUpInterfaceName(b)
	opt := option{interfaceName: ifaceName}
	dest := netip.MustParseAddr("8.8.8.8")
	var d net.Dialer
	var addr string
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d = net.Dialer{}
		keepalive.SetNetDialer(&d)
		mptcp.SetNetDialer(&d, opt.mpTcp)
		if err := bindIfaceToDialer(opt.interfaceName, &d, "tcp4", dest); err != nil {
			b.Fatal(err)
		}
		addr = net.JoinHostPort(dest.String(), "53")
	}
	if d.ControlContext == nil || addr == "" {
		b.Fatal("dial setup lost control or address")
	}
}

func BenchmarkJoinHostPortAddr(b *testing.B) {
	ip := netip.MustParseAddr("192.0.2.10")
	port := "443"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = net.JoinHostPort(ip.String(), port)
	}
}
