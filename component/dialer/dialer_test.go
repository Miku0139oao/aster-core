package dialer

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"syscall"
	"testing"

	"github.com/Miku0139oao/aster-core/component/resolver"
)

type nopRawConn struct{}

func (nopRawConn) Control(f func(uintptr)) error {
	f(0)
	return nil
}
func (nopRawConn) Read(func(uintptr) bool) error  { return nil }
func (nopRawConn) Write(func(uintptr) bool) error { return nil }

func TestAddControlToDialerFastPathThenChain(t *testing.T) {
	var first, second int
	d := &net.Dialer{}
	addControlToDialer(d, func(ctx context.Context, network, address string, c syscall.RawConn) error {
		first++
		return nil
	})
	addControlToDialer(d, func(ctx context.Context, network, address string, c syscall.RawConn) error {
		second++
		return nil
	})
	if d.ControlContext == nil {
		t.Fatal("ControlContext not set")
	}
	if err := d.ControlContext(context.Background(), "tcp", "1.1.1.1:443", nopRawConn{}); err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != 1 {
		t.Fatalf("chained controls first=%d second=%d", first, second)
	}
}

func TestAddControlToDialerPropagatesError(t *testing.T) {
	want := errors.New("first failed")
	d := &net.Dialer{}
	addControlToDialer(d, func(ctx context.Context, network, address string, c syscall.RawConn) error {
		return want
	})
	var second int
	addControlToDialer(d, func(ctx context.Context, network, address string, c syscall.RawConn) error {
		second++
		return nil
	})
	err := d.ControlContext(context.Background(), "tcp", "1.1.1.1:443", nopRawConn{})
	if !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
	if second != 0 {
		t.Fatal("second control must not run after first error")
	}
}

func TestParseAddrIPv4Literal(t *testing.T) {
	ips, port, err := parseAddr(context.Background(), "tcp", "1.1.1.1:443", nil)
	if err != nil {
		t.Fatal(err)
	}
	if port != "443" || len(ips) != 1 || ips[0].String() != "1.1.1.1" {
		t.Fatalf("got %v %s", ips, port)
	}
}

func TestParseAddrIPv4MappedUnmaps(t *testing.T) {
	ips, _, err := parseAddr(context.Background(), "tcp", "[::ffff:192.0.2.1]:80", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || !ips[0].Is4() || ips[0].String() != "192.0.2.1" {
		t.Fatalf("got %v", ips)
	}
}

func TestParseAddrIPv6DisabledRejectsIPv6Literal(t *testing.T) {
	if !resolver.DisableIPv6 {
		t.Skip("IPv6 enabled")
	}
	_, _, err := parseAddr(context.Background(), "tcp", "[2001:db8::1]:443", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, resolver.ErrIPVersion) {
		t.Fatalf("err=%v", err)
	}
}

func TestParseAddrTCP6Disabled(t *testing.T) {
	if !resolver.DisableIPv6 {
		t.Skip("IPv6 enabled")
	}
	_, _, err := parseAddr(context.Background(), "tcp6", "[2001:db8::1]:443", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, resolver.ErrIPv6Disabled) {
		t.Fatalf("err=%v", err)
	}
}

func TestParseAddrTCP4RejectsIPv6(t *testing.T) {
	_, _, err := parseAddr(context.Background(), "tcp4", "[2001:db8::1]:443", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, resolver.ErrIPVersion) {
		t.Fatalf("err=%v", err)
	}
}

func TestNormalizeNetwork(t *testing.T) {
	if got := normalizeNetwork("tcp", 4); got != "tcp4" {
		t.Fatalf("got %s", got)
	}
	if got := normalizeNetwork("tcp4", 6); got != "tcp6" {
		t.Fatalf("got %s", got)
	}
	if got := normalizeNetwork("udp", 6); got != "udp6" {
		t.Fatalf("got %s", got)
	}
	if got := normalizeNetwork("tcp", 0); got != "tcp" {
		t.Fatalf("got %s", got)
	}
}

func TestBindIfaceToDialerSkipsNonGlobalUnicast(t *testing.T) {
	d := &net.Dialer{}
	if err := bindIfaceToDialer("does-not-exist", d, "tcp4", netip.MustParseAddr("127.0.0.1")); err != nil {
		t.Fatal(err)
	}
	if d.ControlContext != nil || d.Control != nil {
		t.Fatal("loopback dest must not install bind control")
	}
}

func TestDialContextToStackMismatch(t *testing.T) {
	_, err := DialContextTo(context.Background(), "tcp4", netip.MustParseAddr("2001:db8::1"), 443)
	if !errors.Is(err, ErrorNoIpAddress) {
		t.Fatalf("err=%v", err)
	}
}
