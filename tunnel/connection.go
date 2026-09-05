package tunnel

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	N "github.com/Miku0139oao/aster-core/common/net"
	C "github.com/Miku0139oao/aster-core/constant"
	"github.com/Miku0139oao/aster-core/log"
)

type packetSender struct {
	ctx    context.Context
	cancel context.CancelFunc
	ch     chan C.PacketAdapter

	// destination NAT mapping
	// Origin state is owned by the single Process goroutine (and the dial
	// goroutine before Process starts). Reverse lookups share reverse state with
	// the receive goroutine under reverseMu.
	// The first destination is stored inline with nil maps. A second distinct
	// origin or reverse target promotes that side into a map; the two sides
	// promote independently so a reverse collision can keep reverse inline.
	hasSingle      bool
	singleOrigin   destinationKey
	singleTarget   *net.UDPAddr
	originToTarget map[destinationKey]*net.UDPAddr

	reverseMu      sync.RWMutex
	hasSingleRev   bool
	singleRevFrom  netip.AddrPort
	singleRevTo    netip.AddrPort
	targetToOrigin map[netip.AddrPort]netip.AddrPort

	nextDeadlineRefresh atomic.Int64
}

type destinationKey struct {
	host string
	ip   netip.Addr
	port uint16
}

func metadataDestinationKey(metadata *C.Metadata) destinationKey {
	if metadata.Host != "" {
		return destinationKey{host: metadata.Host, port: metadata.DstPort}
	}
	return destinationKey{ip: metadata.DstIP.Unmap(), port: metadata.DstPort}
}

// newPacketSender return a chan based C.PacketSender
// It ensures that packets can be sent sequentially and without blocking
func newPacketSender() C.PacketSender {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan C.PacketAdapter, senderCapacity)
	return &packetSender{
		ctx:    ctx,
		cancel: cancel,
		ch:     ch,
	}
}

const maxUDPDestinationMappings = 4096

func (s *packetSender) addMapping(originMetadata *C.Metadata, metadata *C.Metadata) bool {
	originKey := metadataDestinationKey(originMetadata)
	if s.hasOrigin(originKey) {
		return true
	}
	if s.originCount() >= maxUDPDestinationMappings {
		return false
	}
	originAddrPort := originMetadata.AddrPort()
	targetAddrPort := metadata.AddrPort()
	s.storeOrigin(originKey, net.UDPAddrFromAddrPort(targetAddrPort))

	s.reverseMu.Lock()
	s.storeReverseLocked(targetAddrPort, originAddrPort)
	s.reverseMu.Unlock()
	return true
}

func (s *packetSender) hasOrigin(key destinationKey) bool {
	if s.originToTarget != nil {
		return s.originToTarget[key] != nil
	}
	return s.hasSingle && s.singleOrigin == key
}

func (s *packetSender) originCount() int {
	if s.originToTarget != nil {
		return len(s.originToTarget)
	}
	if s.hasSingle {
		return 1
	}
	return 0
}

func (s *packetSender) storeOrigin(key destinationKey, target *net.UDPAddr) {
	if s.originToTarget != nil {
		s.originToTarget[key] = target
		return
	}
	if !s.hasSingle {
		s.hasSingle = true
		s.singleOrigin = key
		s.singleTarget = target
		return
	}
	originToTarget := make(map[destinationKey]*net.UDPAddr, 2)
	originToTarget[s.singleOrigin] = s.singleTarget
	originToTarget[key] = target
	s.originToTarget = originToTarget
	s.hasSingle = false
	s.singleOrigin = destinationKey{}
	s.singleTarget = nil
}

func (s *packetSender) storeReverseLocked(target, origin netip.AddrPort) {
	if !origin.IsValid() {
		return
	}
	if s.targetToOrigin != nil {
		if addr := s.targetToOrigin[target]; !addr.IsValid() {
			s.targetToOrigin[target] = origin
		}
		return
	}
	if s.hasSingleRev {
		if s.singleRevFrom == target {
			return // first-wins
		}
		targetToOrigin := make(map[netip.AddrPort]netip.AddrPort, 2)
		targetToOrigin[s.singleRevFrom] = s.singleRevTo
		targetToOrigin[target] = origin
		s.targetToOrigin = targetToOrigin
		s.hasSingleRev = false
		s.singleRevFrom = netip.AddrPort{}
		s.singleRevTo = netip.AddrPort{}
		return
	}
	s.hasSingleRev = true
	s.singleRevFrom = target
	s.singleRevTo = origin
}

func (s *packetSender) AddMapping(originMetadata *C.Metadata, metadata *C.Metadata) {
	s.addMapping(originMetadata, metadata)
}

func (s *packetSender) RestoreReadFrom(addr netip.AddrPort) netip.AddrPort {
	addr = netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port())
	s.reverseMu.RLock()
	var originAddr netip.AddrPort
	if s.targetToOrigin != nil {
		originAddr = s.targetToOrigin[addr]
	} else if s.hasSingleRev && s.singleRevFrom == addr {
		originAddr = s.singleRevTo
	}
	s.reverseMu.RUnlock()
	if originAddr.IsValid() {
		return originAddr
	}
	return addr
}

const udpDeadlineRefreshInterval = time.Second

var udpDeadlineEpoch = time.Now()

func (s *packetSender) RefreshReadDeadline(pc C.PacketConn) {
	now := time.Now()
	elapsed := time.Since(udpDeadlineEpoch).Nanoseconds()
	nextRefresh := s.nextDeadlineRefresh.Load()
	if elapsed < nextRefresh || !s.nextDeadlineRefresh.CompareAndSwap(nextRefresh, elapsed+udpDeadlineRefreshInterval.Nanoseconds()) {
		return
	}
	// The extra refresh interval ensures an active association never expires
	// before udpTimeout even when its last packet arrives just after a refresh.
	_ = pc.SetReadDeadline(now.Add(udpTimeout + udpDeadlineRefreshInterval))
}

func (s *packetSender) targetAddress(metadata *C.Metadata) *net.UDPAddr {
	key := metadataDestinationKey(metadata)
	if s.originToTarget != nil {
		return s.originToTarget[key]
	}
	if s.hasSingle && s.singleOrigin == key {
		return s.singleTarget
	}
	return nil
}

func (s *packetSender) processPacket(pc C.PacketConn, packet C.PacketAdapter) {
	defer packet.Drop()
	metadata := packet.Metadata()

	addr := s.targetAddress(metadata)

	if addr == nil {
		originMetadata := metadata  // save origin metadata
		metadata = metadata.Clone() // don't modify PacketAdapter's metadata

		if err := preHandleMetadata(metadata); err != nil {
			log.Warnln("[UDP] Destination metadata became invalid: %s", err)
			return
		}
		metadata = metadata.Pure()
		if metadata.Host != "" {
			// TODO: ResolveUDP may take a long time to block the Process loop
			//       but we want keep sequence sending so can't open a new goroutine
			if err := pc.ResolveUDP(s.ctx, metadata); err != nil {
				log.Warnln("[UDP] Resolve Ip error: %s", err)
				return
			}
		}

		if !metadata.DstIP.IsValid() {
			log.Warnln("[UDP] Destination ip not valid: %#v", metadata)
			return
		}
		if !s.addMapping(originMetadata, metadata) {
			log.Warnln("[UDP] Destination mapping limit reached (%d)", maxUDPDestinationMappings)
			return
		}
		addr = metadata.UDPAddr()
	}
	if handleUDPToRemote(packet, pc, addr) == nil {
		s.RefreshReadDeadline(pc)
	}
}

func (s *packetSender) Process(pc C.PacketConn, proxy C.WriteBackProxy) {
	defer s.dropAll()
	for {
		select {
		case <-s.ctx.Done():
			return // sender closed
		case packet := <-s.ch:
			if proxy != nil {
				proxy.UpdateWriteBack(packet.WriteBackTarget())
			}
			s.processPacket(pc, packet)
		}
	}
}

func (s *packetSender) dropAll() {
	for {
		select {
		case data := <-s.ch:
			data.Drop() // drop all data still in chan
		default:
			return // no data, exit goroutine
		}
	}
}

func (s *packetSender) Send(packet C.PacketAdapter) {
	select {
	case <-s.ctx.Done():
		packet.Drop() // sender closed before Send()
		return
	default:
	}

	select {
	case s.ch <- packet:
		// Close may drain the channel between enqueue and now. Re-drain so a
		// packet that landed after dropAll is not stranded without Drop().
		select {
		case <-s.ctx.Done():
			s.dropAll()
		default:
		}
	case <-s.ctx.Done():
		packet.Drop() // sender closed when putting data to chan
	default:
		packet.Drop() // chan is full
	}
}

func (s *packetSender) Close() {
	s.cancel()
	s.dropAll()
}

func (s *packetSender) DoSniff(metadata *C.Metadata) error { return nil }

func handleUDPToRemote(packet C.UDPPacket, pc C.PacketConn, addr *net.UDPAddr) error {
	if addr == nil {
		return errors.New("udp addr invalid")
	}

	if _, err := pc.WriteTo(packet.Data(), addr); err != nil {
		return err
	}
	return nil
}

type udpWriteAddrCache struct {
	addr     *net.UDPAddr
	addrPort netip.AddrPort
}

// resolve returns a *net.UDPAddr for WriteBack. Destination NAT that leaves the
// WaitReadFrom endpoint unchanged reuses that address, so the per-packet
// UDPAddr+IP allocation from net.UDPAddrFromAddrPort is skipped in the common
// identity path. A mapped endpoint is cached for the association.
func (c *udpWriteAddrCache) resolve(from *net.UDPAddr, restored netip.AddrPort) *net.UDPAddr {
	if from != nil && from.AddrPort() == restored {
		if ip, ok := netip.AddrFromSlice(from.IP); ok && !ip.Is4In6() {
			return from
		}
	}
	if c.addr != nil && c.addrPort == restored {
		return c.addr
	}
	c.addr = net.UDPAddrFromAddrPort(restored)
	c.addrPort = restored
	return c.addr
}

func handleUDPToLocal(writeBack C.WriteBack, pc C.PacketConn, sender C.PacketSender, key C.UDPNatKey, oAddrPort netip.AddrPort) {
	defer func() {
		sender.Close()
		_ = pc.Close()
		closeAllLocalCoon(key)
		natTable.Delete(key)
	}()

	var (
		writeCache   udpWriteAddrCache
		fallbackAddr *net.UDPAddr
	)

	for {
		sender.RefreshReadDeadline(pc)
		data, put, from, err := pc.WaitReadFrom()
		if err != nil {
			return
		}

		fromUDPAddr, isUDPAddr := from.(*net.UDPAddr)
		if !isUDPAddr {
			if fallbackAddr == nil {
				fallbackAddr = net.UDPAddrFromAddrPort(oAddrPort) // oAddrPort was Unmapped
			}
			fromUDPAddr = fallbackAddr
			log.Warnln("server return a [%T](%s) which isn't a *net.UDPAddr, force replace to (%s), this may be caused by a wrongly implemented server", from, from, oAddrPort)
		} else if fromUDPAddr == nil {
			if fallbackAddr == nil {
				fallbackAddr = net.UDPAddrFromAddrPort(oAddrPort) // oAddrPort was Unmapped
			}
			fromUDPAddr = fallbackAddr
			log.Warnln("server return a nil *net.UDPAddr, force replace to (%s), this may be caused by a wrongly implemented server", oAddrPort)
		}

		fromAddrPort := fromUDPAddr.AddrPort()

		// restore DestinationNAT, including the original port when aliases share
		// one resolved IP but target different services.
		restored := sender.RestoreReadFrom(fromAddrPort)

		_, err = writeBack.WriteBack(data, writeCache.resolve(fromUDPAddr, restored))
		if put != nil {
			put()
		}
		if err != nil {
			return
		}
	}
}

func closeAllLocalCoon(flow C.UDPNatKey) {
	natTable.RangeForLocalConn(flow, func(key string, value *net.UDPConn) bool {
		conn := value

		conn.Close()
		log.Debugln("Closing TProxy local conn... flow=%s rAddr=%s", flow.String(), key)
		return true
	})
}

func handleSocket(inbound, outbound net.Conn) {
	N.Relay(inbound, outbound)
}
