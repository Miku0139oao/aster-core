package dns

import (
	"net"
	"net/netip"
	"time"

	D "github.com/miekg/dns"
)

// cacheEntry is what the resolver retains. Successful A/AAAA answers with no
// authority/additional data are stored as IP addresses so the cache does not
// keep a cloned DNS message tree (Msg + RR headers + net.IP slices) per name.
type cacheEntry struct {
	msg      *D.Msg
	ips      []netip.Addr
	hdr      D.MsgHdr
	compress bool
}

func (e *cacheEntry) isCompact() bool {
	return e != nil && e.msg == nil
}

type msgCache struct {
	inner interface {
		GetWithExpire(key dnsCacheKey) (*cacheEntry, time.Time, bool)
		SetWithExpire(key dnsCacheKey, value *cacheEntry, expire time.Time)
		Clear()
	}
}

func (c *msgCache) GetWithExpire(key dnsCacheKey) (*D.Msg, time.Time, bool) {
	entry, expire, hit := c.inner.GetWithExpire(key)
	if !hit || entry == nil {
		return nil, expire, hit
	}
	if entry.msg != nil {
		return entry.msg, expire, true
	}
	return entry.expand(key), expire, true
}

func (c *msgCache) SetWithExpire(key dnsCacheKey, value *D.Msg, expire time.Time) {
	c.inner.SetWithExpire(key, encodeCacheEntry(key, value), expire)
}

func (c *msgCache) Clear() {
	c.inner.Clear()
}

func (c *msgCache) peekIPs(key dnsCacheKey) ([]netip.Addr, time.Time, bool) {
	entry, expire, hit := c.inner.GetWithExpire(key)
	if !hit || entry == nil {
		return nil, expire, hit
	}
	if entry.isCompact() {
		ips := make([]netip.Addr, len(entry.ips))
		copy(ips, entry.ips)
		return ips, expire, true
	}
	if entry.msg == nil {
		return nil, expire, hit
	}
	return msgToIP(entry.msg), expire, true
}

func encodeCacheEntry(key dnsCacheKey, msg *D.Msg) *cacheEntry {
	if msg == nil {
		return &cacheEntry{}
	}
	if compact := compactIPs(key, msg); compact != nil {
		return compact
	}
	return &cacheEntry{msg: msg}
}

// compactIPs keeps only pure A/AAAA answers whose owner name matches the
// question. CNAME chains, SOA/NXDOMAIN authority, and glue Extra stay as a
// full message so DNS-server responses remain complete.
func compactIPs(key dnsCacheKey, msg *D.Msg) *cacheEntry {
	if len(msg.Ns) > 0 || len(msg.Extra) > 0 || len(msg.Answer) == 0 {
		return nil
	}
	ips := make([]netip.Addr, 0, len(msg.Answer))
	for _, rr := range msg.Answer {
		switch rec := rr.(type) {
		case *D.A:
			if rec == nil || rec.Hdr.Name != key.name {
				return nil
			}
			ip, ok := netip.AddrFromSlice(rec.A)
			if !ok {
				return nil
			}
			ips = append(ips, ip.Unmap())
		case *D.AAAA:
			if rec == nil || rec.Hdr.Name != key.name {
				return nil
			}
			ip, ok := netip.AddrFromSlice(rec.AAAA)
			if !ok {
				return nil
			}
			ips = append(ips, ip.Unmap())
		default:
			return nil
		}
	}
	return &cacheEntry{ips: ips, hdr: msg.MsgHdr, compress: msg.Compress}
}

// compactAnswerTTL is large so ExchangeContext's updateMsgTTL can subtract
// remaining lifetime without underflowing a 1-second placeholder.
const compactAnswerTTL = 1 << 30

func (e *cacheEntry) expand(key dnsCacheKey) *D.Msg {
	msg := new(D.Msg)
	msg.MsgHdr = e.hdr
	msg.Compress = e.compress
	msg.Question = []D.Question{{Name: key.name, Qtype: key.qtype, Qclass: key.qclass}}
	msg.Answer = make([]D.RR, len(e.ips))
	for i, ip := range e.ips {
		ip = ip.Unmap()
		if ip.Is4() {
			a := ip.As4()
			msg.Answer[i] = &D.A{
				Hdr: D.RR_Header{Name: key.name, Rrtype: D.TypeA, Class: key.qclass, Ttl: compactAnswerTTL},
				A:   net.IPv4(a[0], a[1], a[2], a[3]).To4(),
			}
			continue
		}
		a := ip.As16()
		aaaa := make(net.IP, net.IPv6len)
		copy(aaaa, a[:])
		msg.Answer[i] = &D.AAAA{
			Hdr:  D.RR_Header{Name: key.name, Rrtype: D.TypeAAAA, Class: key.qclass, Ttl: compactAnswerTTL},
			AAAA: aaaa,
		}
	}
	return msg
}
