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
	// originToTarget is owned by the single Process goroutine. Reverse lookups
	// share targetToOrigin with the receive goroutine under reverseMu; this keeps
	// insertion O(1) instead of copying the complete map for every destination.
	originToTarget map[destinationKey]*net.UDPAddr
	targetToOrigin map[netip.AddrPort]netip.AddrPort
	reverseMu      sync.RWMutex

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
		targetToOrigin: make(map[netip.AddrPort]netip.AddrPort),
	}
}

const maxUDPDestinationMappings = 4096

func (s *packetSender) addMapping(originMetadata *C.Metadata, metadata *C.Metadata) bool {
	originKey := metadataDestinationKey(originMetadata)
	if s.originToTarget[originKey] != nil {
		return true
	}
	if len(s.originToTarget) >= maxUDPDestinationMappings {
		return false
	}
	originAddrPort := originMetadata.AddrPort()
	targetAddrPort := metadata.AddrPort()
	s.originToTarget[originKey] = net.UDPAddrFromAddrPort(targetAddrPort)

	s.reverseMu.Lock()
	if addr := s.targetToOrigin[targetAddrPort]; !addr.IsValid() && originAddrPort.IsValid() {
		s.targetToOrigin[targetAddrPort] = originAddrPort
	}
	s.reverseMu.Unlock()
	return true
}

func (s *packetSender) AddMapping(originMetadata *C.Metadata, metadata *C.Metadata) {
	s.addMapping(originMetadata, metadata)
}

func (s *packetSender) RestoreReadFrom(addr netip.AddrPort) netip.AddrPort {
	addr = netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port())
	s.reverseMu.RLock()
	originAddr := s.targetToOrigin[addr]
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
	return s.originToTarget[metadataDestinationKey(metadata)]
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
