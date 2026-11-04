package tunnel

import (
	"net/netip"
	"testing"

	"github.com/Miku0139oao/aster-core/common/utils"
	C "github.com/Miku0139oao/aster-core/constant"
)

// BenchmarkUDPMapHashWholeKey shows the cost the shard selection was paying:
// hashing the full UDPNatKey touches two strings and two AddrPorts.
func BenchmarkUDPMapHashWholeKey(b *testing.B) {
	key := C.UDPNatKey{
		AddrPort:        netip.MustParseAddrPort("192.0.2.1:12345"),
		IngressAddrPort: netip.MustParseAddrPort("10.0.0.1:8080"),
		IngressName:     "mixed-in",
		Raw:             "",
		IngressRaw:      "",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = utils.MapHashComparable(key)
	}
}

func BenchmarkUDPMapHashAddrPortOnly(b *testing.B) {
	key := C.UDPNatKey{
		AddrPort:        netip.MustParseAddrPort("192.0.2.1:12345"),
		IngressAddrPort: netip.MustParseAddrPort("10.0.0.1:8080"),
		IngressName:     "mixed-in",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = utils.MapHashComparable(key.AddrPort)
	}
}

// TestUDPQueueIndexSpreadsAddrPortFlows guards the shard distribution the hash
// change depends on: a few hundred distinct flows must not collapse onto one
// worker, or UDP processing serialises.
func TestUDPQueueIndexSpreadsAddrPortFlows(t *testing.T) {
	queues := initUDPQueuesForTest()
	seen := make(map[uint]int, len(queues))
	for i := 0; i < 256; i++ {
		key := C.UDPNatKey{AddrPort: netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 0, 2, 1}), uint16(1024+i))}
		seen[udpQueueIndex(key)]++
	}
	if len(seen) < len(queues)/2 {
		t.Fatalf("256 flows only reached %d of %d queues: %v", 256, len(queues), seen)
	}
}

// TestUDPQueueIndexSpreadsRawFlows is the guard that matters most: sing
// inbounds use customAddr keys with no AddrPort. If those all hashed to one
// shard, every such flow would run on a single worker.
func TestUDPQueueIndexSpreadsRawFlows(t *testing.T) {
	queues := initUDPQueuesForTest()
	seen := make(map[uint]int, len(queues))
	for i := 0; i < 256; i++ {
		key := C.UDPNatKey{Raw: netip.AddrPortFrom(netip.AddrFrom4([4]byte{198, 51, 100, 1}), uint16(2048+i)).String()}
		seen[udpQueueIndex(key)]++
	}
	if len(seen) < len(queues)/2 {
		t.Fatalf("256 raw keys only reached %d of %d queues: %v", 256, len(queues), seen)
	}
}

func initUDPQueuesForTest() []chan C.PacketAdapter {
	udpInit.Do(initUDP)
	return udpQueues
}
