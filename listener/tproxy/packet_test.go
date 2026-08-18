package tproxy

import (
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	C "github.com/Miku0139oao/aster-core/constant"

	"github.com/stretchr/testify/require"
)

type packetTestNatTable struct {
	conn   *net.UDPConn
	cond   *sync.Cond
	loaded bool
}

func (t *packetTestNatTable) GetOrCreate(C.UDPNatKey, func() C.PacketSender) (C.PacketSender, bool) {
	return nil, false
}
func (t *packetTestNatTable) Delete(C.UDPNatKey) {}
func (t *packetTestNatTable) GetForLocalConn(string, string) *net.UDPConn {
	return t.conn
}
func (t *packetTestNatTable) AddForLocalConn(string, string, *net.UDPConn) bool { return true }
func (t *packetTestNatTable) RangeForLocalConn(string, func(string, *net.UDPConn) bool) {
}
func (t *packetTestNatTable) GetOrCreateLockForLocalConn(string, string) (*sync.Cond, bool) {
	return t.cond, t.loaded
}
func (t *packetTestNatTable) DeleteForLocalConn(string, string)     {}
func (t *packetTestNatTable) DeleteLockForLocalConn(string, string) {}

type packetTestTunnel struct {
	nat C.NatTable
}

func (packetTestTunnel) HandleTCPConn(net.Conn, *C.Metadata)      {}
func (packetTestTunnel) HandleUDPPacket(C.UDPPacket, *C.Metadata) {}
func (t packetTestTunnel) NatTable() C.NatTable                   { return t.nat }

func TestCreateOrGetLocalConnUnlocksWaiterWhenEntryMissing(t *testing.T) {
	cond := sync.NewCond(&sync.Mutex{})
	table := &packetTestNatTable{cond: cond, loaded: true}
	tunnel := packetTestTunnel{nat: table}

	rAddr := netip.MustParseAddrPort("1.1.1.1:53")
	lAddr := netip.MustParseAddrPort("192.0.2.1:12345")

	done := make(chan error, 1)
	go func() {
		_, err := createOrGetLocalConn(rAddr, lAddr, tunnel)
		done <- err
	}()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				cond.L.Lock()
				cond.Broadcast()
				cond.L.Unlock()
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		close(stop)
		wg.Wait()
		t.Fatal("waiter did not return after broadcasts")
	}
	close(stop)
	wg.Wait()
	require.Error(t, err)

	locked := make(chan struct{})
	go func() {
		cond.L.Lock()
		close(locked)
		cond.L.Unlock()
	}()
	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter returned without unlocking the NAT cond mutex")
	}
}
