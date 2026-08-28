package tunnel

import (
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
)

type dropCountPacket struct {
	drops *atomic.Int64
	local net.Addr
}

func (p *dropCountPacket) Data() []byte                            { return nil }
func (p *dropCountPacket) WriteBack([]byte, net.Addr) (int, error) { return 0, nil }
func (p *dropCountPacket) Drop()                                   { p.drops.Add(1) }
func (p *dropCountPacket) LocalAddr() net.Addr                     { return p.local }

func newDropCountAdapter(drops *atomic.Int64) C.PacketAdapter {
	return C.NewPacketAdapter(&dropCountPacket{
		drops: drops,
		local: net.UDPAddrFromAddrPort(netip.MustParseAddrPort("192.0.2.1:12345")),
	}, &C.Metadata{NetWork: C.UDP, DstIP: netip.MustParseAddr("198.51.100.1"), DstPort: 443})
}

func drainSenderChannel(sender *packetSender) int {
	leftover := 0
	for {
		select {
		case packet := <-sender.ch:
			leftover++
			packet.Drop()
		default:
			return leftover
		}
	}
}

func TestPacketSenderSendAfterCloseDrops(t *testing.T) {
	sender := newPacketSender().(*packetSender)
	sender.Close()

	var drops atomic.Int64
	sender.Send(newDropCountAdapter(&drops))
	if got := drops.Load(); got != 1 {
		t.Fatalf("drops = %d, want 1", got)
	}
	if leftover := drainSenderChannel(sender); leftover != 0 {
		t.Fatalf("leaked %d packets in channel after Close", leftover)
	}
}

type captureWriteBack struct {
	addr net.Addr
	n    int
}

func (c *captureWriteBack) WriteBack(b []byte, addr net.Addr) (int, error) {
	c.addr = addr
	c.n++
	return len(b), nil
}

func TestHandleUDPToLocalReusesWaitReadAddr(t *testing.T) {
	sender := newPacketSender().(*packetSender)
	from := net.UDPAddrFromAddrPort(netip.MustParseAddrPort("198.51.100.1:443"))
	wb := &captureWriteBack{}
	conn := &udpToLocalPacketConn{payload: []byte{1}, from: from, remain: 1}
	handleUDPToLocal(wb, conn, sender, C.UDPNatKey{}, from.AddrPort())
	if wb.n != 1 {
		t.Fatalf("WriteBack calls = %d, want 1", wb.n)
	}
	if wb.addr != from {
		t.Fatalf("WriteBack addr = %v, want reused WaitReadFrom pointer", wb.addr)
	}
}

func TestHandleUDPToLocalRestoresNATAddr(t *testing.T) {
	sender := newPacketSender().(*packetSender)
	origin := &C.Metadata{DstIP: netip.MustParseAddr("192.0.2.1"), DstPort: 53}
	target := &C.Metadata{DstIP: netip.MustParseAddr("198.51.100.1"), DstPort: 53}
	sender.AddMapping(origin, target)

	from := net.UDPAddrFromAddrPort(target.AddrPort())
	wb := &captureWriteBack{}
	conn := &udpToLocalPacketConn{payload: []byte{1, 2}, from: from, remain: 2}
	handleUDPToLocal(wb, conn, sender, C.UDPNatKey{}, origin.AddrPort())
	if wb.n != 2 {
		t.Fatalf("WriteBack calls = %d, want 2", wb.n)
	}
	got, ok := wb.addr.(*net.UDPAddr)
	if !ok || got == nil {
		t.Fatalf("WriteBack addr type = %T", wb.addr)
	}
	if got.AddrPort() != origin.AddrPort() {
		t.Fatalf("NAT restore WriteBack addr = %s, want %s", got.AddrPort(), origin.AddrPort())
	}
	if got == from {
		t.Fatal("NAT restore must not reuse the mapped WaitReadFrom addr")
	}
}

func TestUDPWriteAddrCacheUnmaps4in6(t *testing.T) {
	var cache udpWriteAddrCache
	from := &net.UDPAddr{IP: net.ParseIP("198.51.100.1"), Port: 443}
	if len(from.IP) != net.IPv6len {
		t.Fatalf("setup: ParseIP IPv4 len = %d, want %d", len(from.IP), net.IPv6len)
	}
	restored := netip.MustParseAddrPort("198.51.100.1:443")
	got := cache.resolve(from, restored)
	if got == from {
		t.Fatal("4in6 WaitReadFrom addr must be unmapped for WriteBack")
	}
	if got.AddrPort() != restored {
		t.Fatalf("unmapped addr = %s, want %s", got.AddrPort(), restored)
	}
	if len(got.IP) != net.IPv4len {
		t.Fatalf("unmapped IP len = %d, want %d", len(got.IP), net.IPv4len)
	}
	if cache.resolve(from, restored) != got {
		t.Fatal("unmapped 4in6 addr was reallocated")
	}
}

func TestUDPWriteAddrCacheReusesMappedAddr(t *testing.T) {
	var cache udpWriteAddrCache
	from := net.UDPAddrFromAddrPort(netip.MustParseAddrPort("198.51.100.1:53"))
	restored := netip.MustParseAddrPort("192.0.2.1:53")
	first := cache.resolve(from, restored)
	second := cache.resolve(from, restored)
	if first == from {
		t.Fatal("mapped endpoint reused WaitReadFrom addr")
	}
	if first != second {
		t.Fatal("mapped endpoint was reallocated")
	}
	if first.AddrPort() != restored {
		t.Fatalf("mapped addr = %s, want %s", first.AddrPort(), restored)
	}
}

func TestPacketSenderCloseDoesNotStrandInflightSend(t *testing.T) {
	const packets = 256
	for i := 0; i < 50; i++ {
		sender := newPacketSender().(*packetSender)
		var drops atomic.Int64
		var wg sync.WaitGroup
		wg.Add(packets)
		for n := 0; n < packets; n++ {
			go func() {
				defer wg.Done()
				sender.Send(newDropCountAdapter(&drops))
			}()
		}
		sender.Close()
		wg.Wait()

		if leftover := drainSenderChannel(sender); leftover != 0 {
			t.Fatalf("iteration %d leaked %d packets in channel after Close", i, leftover)
		}
		if got := drops.Load(); got != packets {
			t.Fatalf("iteration %d drops = %d, want %d", i, got, packets)
		}
	}
}
