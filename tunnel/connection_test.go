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
