package tunnel

import (
	"context"
	"errors"
	"net"
	"net/netip"
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
	// originToTarget is owned by the single Process goroutine. The initial
	// mapping is installed before Process starts, so outbound packet lookups do
	// not need a lock. Reverse lookups use an immutable atomic snapshot because
	// they are read concurrently by the receive goroutine.
	originToTarget  map[destinationKey]*net.UDPAddr
	targetToOrigin  map[netip.Addr]netip.Addr
	reverseMappings atomic.Pointer[map[netip.Addr]netip.Addr]

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

		originToTarget: make(map[destinationKey]*net.UDPAddr),
		targetToOrigin: make(map[netip.Addr]netip.Addr),
	}
}

func (s *packetSender) AddMapping(originMetadata *C.Metadata, metadata *C.Metadata) {
	originKey := metadataDestinationKey(originMetadata)
	originAddr := originMetadata.DstIP.Unmap()
	targetAddr := metadata.DstIP.Unmap()
	if s.originToTarget[originKey] == nil {
		s.originToTarget[originKey] = net.UDPAddrFromAddrPort(netip.AddrPortFrom(targetAddr, metadata.DstPort))
	}

	if addr := s.targetToOrigin[targetAddr]; !addr.IsValid() { // overwrite only if the record is illegal
		s.targetToOrigin[targetAddr] = originAddr
		snapshot := make(map[netip.Addr]netip.Addr, len(s.targetToOrigin))
		for target, origin := range s.targetToOrigin {
			snapshot[target] = origin
		}
		s.reverseMappings.Store(&snapshot)
	}
}

func (s *packetSender) RestoreReadFrom(addr netip.Addr) netip.Addr {
	addr = addr.Unmap()
	snapshot := s.reverseMappings.Load()
	if snapshot != nil {
		if originAddr := (*snapshot)[addr]; originAddr.IsValid() {
			return originAddr
		}
	}
	return addr
}

const udpDeadlineRefreshInterval = time.Second

func (s *packetSender) RefreshReadDeadline(pc C.PacketConn) {
	now := time.Now()
	nowUnixNano := now.UnixNano()
	nextRefresh := s.nextDeadlineRefresh.Load()
	if nowUnixNano < nextRefresh || !s.nextDeadlineRefresh.CompareAndSwap(nextRefresh, now.Add(udpDeadlineRefreshInterval).UnixNano()) {
		return
	}
	// The extra refresh interval ensures an active association never expires
	// before udpTimeout even when its last packet arrives just after a refresh.
	_ = pc.SetReadDeadline(now.Add(udpTimeout + udpDeadlineRefreshInterval))
}

func (s *packetSender) targetAddress(metadata *C.Metadata) *net.UDPAddr {
	return s.originToTarget[metadataDestinationKey(metadata)]
}

func (s *packetSender) processPacket(pc C.PacketConn, packet C.PacketAdapter) {
	defer packet.Drop()
	metadata := packet.Metadata()

	addr := s.targetAddress(metadata)

	if addr == nil {
		originMetadata := metadata  // save origin metadata
		metadata = metadata.Clone() // don't modify PacketAdapter's metadata

		_ = preHandleMetadata(metadata) // error was pre-checked
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
		s.AddMapping(originMetadata, metadata)
		addr = metadata.UDPAddr()
	}
	if handleUDPToRemote(packet, pc, addr) == nil {
		s.RefreshReadDeadline(pc)
	}
}

func (s *packetSender) Process(pc C.PacketConn, proxy C.WriteBackProxy) {
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
		// put ok, so don't drop packet, will process by other side of chan
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

func handleUDPToLocal(writeBack C.WriteBack, pc C.PacketConn, sender C.PacketSender, key C.UDPNatKey, oAddrPort netip.AddrPort) {
	defer func() {
		sender.Close()
		_ = pc.Close()
		closeAllLocalCoon(key)
		natTable.Delete(key)
	}()

	for {
		sender.RefreshReadDeadline(pc)
		data, put, from, err := pc.WaitReadFrom()
		if err != nil {
			return
		}

		fromUDPAddr, isUDPAddr := from.(*net.UDPAddr)
		if !isUDPAddr {
			fromUDPAddr = net.UDPAddrFromAddrPort(oAddrPort) // oAddrPort was Unmapped
			log.Warnln("server return a [%T](%s) which isn't a *net.UDPAddr, force replace to (%s), this may be caused by a wrongly implemented server", from, from, oAddrPort)
		} else if fromUDPAddr == nil {
			fromUDPAddr = net.UDPAddrFromAddrPort(oAddrPort) // oAddrPort was Unmapped
			log.Warnln("server return a nil *net.UDPAddr, force replace to (%s), this may be caused by a wrongly implemented server", oAddrPort)
		}

		fromAddrPort := fromUDPAddr.AddrPort()
		fromAddr := fromAddrPort.Addr().Unmap()

		// restore DestinationNAT
		fromAddr = sender.RestoreReadFrom(fromAddr).Unmap()

		fromAddrPort = netip.AddrPortFrom(fromAddr, fromAddrPort.Port())

		_, err = writeBack.WriteBack(data, net.UDPAddrFromAddrPort(fromAddrPort))
		if put != nil {
			put()
		}
		if err != nil {
			return
		}
	}
}

func closeAllLocalCoon(lAddr C.UDPNatKey) {
	natTable.RangeForLocalConn(lAddr.String(), func(key string, value *net.UDPConn) bool {
		conn := value

		conn.Close()
		log.Debugln("Closing TProxy local conn... lAddr=%s rAddr=%s", lAddr, key)
		return true
	})
}

func handleSocket(inbound, outbound net.Conn) {
	N.Relay(inbound, outbound)
}
