package nat

import (
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"
)

func TestGetOrCreateLocalConnPromiseCannotMissCompletion(t *testing.T) {
	table := New()
	flow := C.UDPNatKey{AddrPort: netip.MustParseAddrPort("192.0.2.1:12345")}
	_, _, admitted := table.GetOrCreate(flow, func() C.PacketSender { return nil })
	if !admitted {
		t.Fatal("failed to create test NAT entry")
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	maker := func() (*net.UDPConn, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return listener, nil
	}

	const waiters = 32
	results := make(chan *net.UDPConn, waiters)
	errs := make(chan error, waiters)
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			conn, err := table.GetOrCreateLocalConn(flow, "198.51.100.1:53", maker)
			results <- conn
			errs <- err
		}()
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("local connection maker did not start")
	}
	close(release)
	wg.Wait()
	close(results)
	close(errs)

	if calls.Load() != 1 {
		t.Fatalf("maker called %d times", calls.Load())
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for conn := range results {
		if conn != listener {
			t.Fatal("waiter received a different connection")
		}
	}
}

func TestTableAdmissionIsBoundedAndRecoversAfterDelete(t *testing.T) {
	table := New(2)
	keys := []C.UDPNatKey{
		{AddrPort: netip.MustParseAddrPort("192.0.2.1:1001")},
		{AddrPort: netip.MustParseAddrPort("192.0.2.2:1002")},
		{AddrPort: netip.MustParseAddrPort("192.0.2.3:1003")},
	}
	var makers atomic.Int64
	maker := func() C.PacketSender {
		makers.Add(1)
		return nil
	}
	for _, key := range keys[:2] {
		_, loaded, admitted := table.GetOrCreate(key, maker)
		if loaded || !admitted {
			t.Fatalf("initial admission loaded=%t admitted=%t", loaded, admitted)
		}
	}
	if _, _, admitted := table.GetOrCreate(keys[2], maker); admitted {
		t.Fatal("NAT entry beyond the limit was admitted")
	}
	if _, loaded, admitted := table.GetOrCreate(keys[0], maker); !loaded || !admitted {
		t.Fatal("existing NAT entry was rejected at capacity")
	}
	if makers.Load() != 2 || table.Size() != 2 || table.Rejected() != 1 {
		t.Fatalf("unexpected table metrics: makers=%d size=%d rejected=%d", makers.Load(), table.Size(), table.Rejected())
	}
	table.Delete(keys[0])
	if _, loaded, admitted := table.GetOrCreate(keys[2], maker); loaded || !admitted {
		t.Fatal("capacity was not released after delete")
	}
	if table.Size() != 2 {
		t.Fatalf("table size = %d", table.Size())
	}
}

func TestNatCacheSlotSpreadsSequentialPorts(t *testing.T) {
	seen := make(map[uint]struct{}, natHitSlots)
	for i := 0; i < natHitSlots; i++ {
		key := C.UDPNatKey{AddrPort: netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 0, 2, 1}), uint16(10000+i))}
		slot := natCacheSlot(key)
		if _, dup := seen[slot]; dup {
			t.Fatalf("slot %d collided for sequential client ports", slot)
		}
		seen[slot] = struct{}{}
	}
}

func TestGetOrCreateCacheComparesFullKey(t *testing.T) {
	table := New()
	a := C.UDPNatKey{AddrPort: netip.MustParseAddrPort("192.0.2.1:12345"), IngressType: 1, IngressName: "in-a"}
	b := C.UDPNatKey{AddrPort: netip.MustParseAddrPort("192.0.2.1:12345"), IngressType: 2, IngressName: "in-b"}
	sa := &benchmarkPacketSender{}
	sb := &benchmarkPacketSender{}
	gotA, loaded, admitted := table.GetOrCreate(a, func() C.PacketSender { return sa })
	if loaded || !admitted || gotA != sa {
		t.Fatal("failed to create flow A")
	}
	gotB, loaded, admitted := table.GetOrCreate(b, func() C.PacketSender { return sb })
	if loaded || !admitted || gotB != sb {
		t.Fatal("failed to create flow B")
	}
	for i := 0; i < 8; i++ {
		if got, loaded, admitted := table.GetOrCreate(a, func() C.PacketSender { return sa }); !loaded || !admitted || got != sa {
			t.Fatal("flow A lookup returned the wrong sender")
		}
		if got, loaded, admitted := table.GetOrCreate(b, func() C.PacketSender { return sb }); !loaded || !admitted || got != sb {
			t.Fatal("flow B lookup returned the wrong sender")
		}
	}
}

func TestGetOrCreateAfterDeleteDoesNotResurrectCachedFlow(t *testing.T) {
	table := New()
	key := C.UDPNatKey{AddrPort: netip.MustParseAddrPort("192.0.2.8:34567")}
	first := &benchmarkPacketSender{}
	got, loaded, admitted := table.GetOrCreate(key, func() C.PacketSender { return first })
	if loaded || !admitted || got != first {
		t.Fatal("failed to create first NAT entry")
	}
	// Prime the last-hit cache.
	if got, loaded, admitted = table.GetOrCreate(key, func() C.PacketSender { return first }); !loaded || !admitted || got != first {
		t.Fatal("existing NAT entry was not cached")
	}
	table.Delete(key)
	second := &benchmarkPacketSender{}
	got, loaded, admitted = table.GetOrCreate(key, func() C.PacketSender { return second })
	if loaded || !admitted || got != second {
		t.Fatalf("deleted NAT flow was resurrected: loaded=%t admitted=%t got=%v", loaded, admitted, got)
	}
	if table.Size() != 1 {
		t.Fatalf("table size = %d", table.Size())
	}
}

func TestGetOrCreateDeleteRecreateKeepsSizeNonNegative(t *testing.T) {
	table := New(8)
	key := C.UDPNatKey{AddrPort: netip.MustParseAddrPort("192.0.2.9:40000")}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				sender := &benchmarkPacketSender{}
				table.GetOrCreate(key, func() C.PacketSender { return sender })
				table.Delete(key)
			}
		}()
	}
	wg.Wait()
	if sz := table.Size(); sz < 0 || sz > 1 {
		t.Fatalf("table size = %d", sz)
	}
}

func TestGetOrCreateLocalConnRetriesAfterFailure(t *testing.T) {
	table := New()
	flow := C.UDPNatKey{AddrPort: netip.MustParseAddrPort("192.0.2.2:23456")}
	_, _, admitted := table.GetOrCreate(flow, func() C.PacketSender { return nil })
	if !admitted {
		t.Fatal("failed to create test NAT entry")
	}
	expected := errors.New("listen failed")
	if _, err := table.GetOrCreateLocalConn(flow, "198.51.100.2:53", func() (*net.UDPConn, error) {
		return nil, expected
	}); !errors.Is(err, expected) {
		t.Fatalf("unexpected first error: %v", err)
	}

	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	conn, err := table.GetOrCreateLocalConn(flow, "198.51.100.2:53", func() (*net.UDPConn, error) {
		return listener, nil
	})
	if err != nil || conn != listener {
		t.Fatalf("retry failed: conn=%v err=%v", conn, err)
	}
}
