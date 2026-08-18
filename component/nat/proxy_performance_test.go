package nat

import (
	"net"
	"net/netip"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

type benchmarkWriteBack struct{}

type benchmarkPacketSender struct {
	C.PacketSender
}

func (*benchmarkWriteBack) WriteBack(b []byte, _ net.Addr) (int, error) {
	return len(b), nil
}

func BenchmarkWriteBackProxyUpdate(b *testing.B) {
	proxy := NewWriteBackProxy(&benchmarkWriteBack{})
	target := &benchmarkWriteBack{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		proxy.UpdateWriteBack(target)
	}
}

func BenchmarkWriteBackProxyCall(b *testing.B) {
	proxy := NewWriteBackProxy(&benchmarkWriteBack{})
	payload := make([]byte, 1200)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := proxy.WriteBack(payload, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTableExistingFlow(b *testing.B) {
	table := New()
	key := C.UDPNatKey{AddrPort: netip.MustParseAddrPort("192.0.2.1:12345")}
	sender := &benchmarkPacketSender{}
	maker := func() C.PacketSender { return sender }
	if _, loaded := table.GetOrCreate(key, maker); loaded {
		b.Fatal("first lookup unexpectedly loaded")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, loaded := table.GetOrCreate(key, maker)
		if !loaded || got != sender {
			b.Fatal("existing NAT flow was not found")
		}
	}
}
