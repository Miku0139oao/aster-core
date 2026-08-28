package sing_tun

import (
	"net/netip"
	"testing"

	"github.com/metacubex/sing/common/control"
)

type stubInterfaceFinder struct {
	iface control.Interface
}

func newStubInterfaceFinder(name string) *stubInterfaceFinder {
	return &stubInterfaceFinder{iface: control.Interface{Index: 1, Name: name}}
}

func (s *stubInterfaceFinder) Update() error { return nil }
func (s *stubInterfaceFinder) Interfaces() []control.Interface {
	return []control.Interface{s.iface}
}
func (s *stubInterfaceFinder) ByName(name string) (*control.Interface, error) {
	if name == s.iface.Name {
		return &s.iface, nil
	}
	return nil, errIfaceNotFoundForTest
}
func (s *stubInterfaceFinder) ByIndex(index int) (*control.Interface, error) {
	if index == s.iface.Index {
		return &s.iface, nil
	}
	return nil, errIfaceNotFoundForTest
}
func (s *stubInterfaceFinder) ByAddr(netip.Addr) (*control.Interface, error) {
	return &s.iface, nil
}

var errIfaceNotFoundForTest = errString("iface not found")

type errString string

func (e errString) Error() string { return string(e) }

func TestFindInterfaceNameSkipsTunAndEmpty(t *testing.T) {
	old := DefaultInterfaceFinder
	t.Cleanup(func() { DefaultInterfaceFinder = old })
	DefaultInterfaceFinder = newStubInterfaceFinder("eth0")

	d := &cDialerInterfaceFinder{tunName: "Meta"}
	if got := d.FindInterfaceName(netip.MustParseAddr("1.1.1.1")); got != "eth0" {
		t.Fatalf("FindInterfaceName = %q, want eth0", got)
	}

	DefaultInterfaceFinder = newStubInterfaceFinder("Meta")
	if got := d.FindInterfaceName(netip.MustParseAddr("1.1.1.1")); got != "<invalid>" {
		t.Fatalf("FindInterfaceName with tun name = %q, want <invalid>", got)
	}
}

func TestInterfacesSnapshotStable(t *testing.T) {
	f := DefaultInterfaceFinder
	a := f.Interfaces()
	b := f.Interfaces()
	if len(a) == 0 {
		t.Skip("no network interfaces")
	}
	if len(a) != len(b) {
		t.Fatalf("Interfaces length changed: %d vs %d", len(a), len(b))
	}
	if &a[0] != &b[0] {
		t.Fatal("Interfaces() rebuilt the snapshot on a cache hit")
	}
	if err := f.Update(); err != nil {
		t.Fatalf("Update: %v", err)
	}
	c := f.Interfaces()
	if len(c) == 0 {
		t.Fatal("Interfaces() empty after Update")
	}
	// `a` is still live, so a rebuilt snapshot cannot reuse its backing array.
	if &c[0] == &a[0] {
		t.Fatal("Update() did not drop the converted snapshot")
	}
}

var ifaceListSink []control.Interface
var ifaceNameSink string

func BenchmarkFindInterfaceName(b *testing.B) {
	old := DefaultInterfaceFinder
	b.Cleanup(func() { DefaultInterfaceFinder = old })
	DefaultInterfaceFinder = newStubInterfaceFinder("eth0")
	d := &cDialerInterfaceFinder{tunName: "Meta"}
	dest := netip.MustParseAddr("1.1.1.1")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ifaceNameSink = d.FindInterfaceName(dest)
	}
}

func BenchmarkInterfaces(b *testing.B) {
	f := DefaultInterfaceFinder
	_ = f.Interfaces()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ifaceListSink = f.Interfaces()
	}
}
