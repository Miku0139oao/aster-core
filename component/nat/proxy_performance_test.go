package nat

import (
	"net"
	"net/netip"
	"sync"
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
	if _, loaded, admitted := table.GetOrCreate(key, maker); loaded || !admitted {
		b.Fatal("first lookup unexpectedly loaded")
	}
	if _, loaded, admitted := table.GetOrCreate(key, maker); !loaded || !admitted {
		b.Fatal("failed to prime NAT last-hit cache")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, loaded, admitted := table.GetOrCreate(key, maker)
		if !loaded || !admitted || got != sender {
			b.Fatal("existing NAT flow was not found")
		}
	}
}

func BenchmarkTableExistingFlowParallel(b *testing.B) {
	table := New()
	key := C.UDPNatKey{AddrPort: netip.MustParseAddrPort("192.0.2.1:12345")}
	sender := &benchmarkPacketSender{}
	maker := func() C.PacketSender { return sender }
	if _, loaded, admitted := table.GetOrCreate(key, maker); loaded || !admitted {
		b.Fatal("first lookup unexpectedly loaded")
	}
	if _, loaded, admitted := table.GetOrCreate(key, maker); !loaded || !admitted {
		b.Fatal("failed to prime NAT last-hit cache")
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			got, loaded, admitted := table.GetOrCreate(key, maker)
			if !loaded || !admitted || got != sender {
				b.Fatal("existing NAT flow was not found")
			}
		}
	})
}

func BenchmarkTableExistingFlow64(b *testing.B) {
	table := New()
	sender := &benchmarkPacketSender{}
	maker := func() C.PacketSender { return sender }
	keys := make([]C.UDPNatKey, 64)
	for i := range keys {
		keys[i] = C.UDPNatKey{AddrPort: netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 0, 2, 1}), uint16(10000+i))}
		if _, loaded, admitted := table.GetOrCreate(keys[i], maker); loaded || !admitted {
			b.Fatal("failed to create NAT flow")
		}
	}
	for i := range keys {
		if _, loaded, admitted := table.GetOrCreate(keys[i], maker); !loaded || !admitted {
			b.Fatal("failed to prime NAT last-hit cache")
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := keys[i&63]
		got, loaded, admitted := table.GetOrCreate(key, maker)
		if !loaded || !admitted || got != sender {
			b.Fatal("existing NAT flow was not found")
		}
	}
}

func BenchmarkTableFill4096(b *testing.B) {
	maker := func() C.PacketSender { return &benchmarkPacketSender{} }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		table := New()
		for j := 0; j < 4096; j++ {
			key := C.UDPNatKey{AddrPort: netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, byte(j >> 16), byte(j >> 8), byte(j)}), uint16(j+1))}
			if _, loaded, admitted := table.GetOrCreate(key, maker); loaded || !admitted {
				b.Fatal("fill lookup failed")
			}
		}
	}
}

func BenchmarkWriteBackProxyParallel(b *testing.B) {
	proxy := NewWriteBackProxy(&benchmarkWriteBack{})
	target := &benchmarkWriteBack{}
	payload := make([]byte, 1200)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for i := 0; pb.Next(); i++ {
			if i&7 == 0 {
				proxy.UpdateWriteBack(target)
			}
			if _, err := proxy.WriteBack(payload, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestWriteBackProxyConcurrentUpdateAndCall(t *testing.T) {
	proxy := NewWriteBackProxy(&benchmarkWriteBack{})
	targets := []*benchmarkWriteBack{{}, {}, {}}
	payload := make([]byte, 64)
	const workers = 8
	const iters = 10000
	var wg sync.WaitGroup
	wg.Add(workers * 2)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for n := 0; n < iters; n++ {
				proxy.UpdateWriteBack(targets[n%len(targets)])
			}
		}()
		go func() {
			defer wg.Done()
			for n := 0; n < iters; n++ {
				if got, err := proxy.WriteBack(payload, nil); err != nil || got != len(payload) {
					t.Errorf("WriteBack = %d, %v", got, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestWriteBackProxyIgnoresNilUpdate(t *testing.T) {
	proxy := NewWriteBackProxy(&benchmarkWriteBack{})
	proxy.UpdateWriteBack(nil)
	payload := []byte("x")
	n, err := proxy.WriteBack(payload, nil)
	if err != nil || n != len(payload) {
		t.Fatalf("nil update dropped the target: n=%d err=%v", n, err)
	}
}
