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
