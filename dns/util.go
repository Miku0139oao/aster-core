package dns

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/Miku0139oao/aster-core/common/picker"
	"github.com/Miku0139oao/aster-core/component/ech/echparser"
	"github.com/Miku0139oao/aster-core/component/resolver"
	"github.com/Miku0139oao/aster-core/log"

	D "github.com/miekg/dns"
	"golang.org/x/exp/slices"
)

const (
	MaxMsgSize = 65535
)

const serverFailureCacheTTL uint32 = 5

func minimalTTL(records []D.RR) uint32 {
	var min uint32
	found := false
	for _, rr := range records {
		if rr == nil {
			continue
		}
		ttl := rr.Header().Ttl
		if !found || ttl < min {
			min = ttl
			found = true
		}
	}
	if !found {
		return 0
	}
	return min
}

func minTTLAll(answer, ns, extra []D.RR) uint32 {
	var min uint32
	found := false
	consider := func(records []D.RR) {
		for _, rr := range records {
			if rr == nil {
				continue
			}
			ttl := rr.Header().Ttl
			if !found || ttl < min {
				min = ttl
				found = true
			}
		}
	}
	consider(answer)
	consider(ns)
	consider(extra)
	if !found {
		return 0
	}
	return min
}

func updateTTL(records []D.RR, ttl uint32) {
	if len(records) == 0 {
		return
	}
	delta := minimalTTL(records) - ttl
	for i := range records {
		if records[i] == nil {
			continue
		}
		hdr := records[i].Header()
		next := hdr.Ttl - delta
		// Match lo.Clamp(next, 1, hdr.Ttl): apply the lower bound first so a 0 TTL
		// becomes 1 instead of being pulled back down by the upper bound.
		if next < 1 {
			next = 1
		} else if next > hdr.Ttl {
			next = hdr.Ttl
		}
		hdr.Ttl = next
	}
}

func cacheKey(q D.Question) dnsCacheKey {
	return dnsCacheKey{name: q.Name, qtype: q.Qtype, qclass: q.Qclass}
}

func questionFlightKey(q D.Question) string {
	// name + NUL + type/class. Avoids miekg Question.String() concatenations.
	b := make([]byte, 0, len(q.Name)+5)
	b = append(b, q.Name...)
	b = append(b, 0, byte(q.Qtype>>8), byte(q.Qtype), byte(q.Qclass>>8), byte(q.Qclass))
	return string(b)
}

func trimDots(s string) string {
	for len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

func canShareRdata(rrs []D.RR) bool {
	for _, rr := range rrs {
		switch rr.(type) {
		case nil, *D.A, *D.AAAA, *D.CNAME, *D.DNAME, *D.NS, *D.PTR, *D.SOA:
		default:
			return false
		}
	}
	return true
}

func cloneIP(ip []byte) []byte {
	if ip == nil {
		return nil
	}
	out := make([]byte, len(ip))
	copy(out, ip)
	return out
}

func cloneShareRdata(rr D.RR) (D.RR, bool) {
	switch r := rr.(type) {
	case *D.A:
		if r == nil {
			return (*D.A)(nil), true
		}
		n := *r
		n.A = cloneIP(r.A)
		return &n, true
	case *D.AAAA:
		if r == nil {
			return (*D.AAAA)(nil), true
		}
		n := *r
		n.AAAA = cloneIP(r.AAAA)
		return &n, true
	case *D.CNAME:
		if r == nil {
			return (*D.CNAME)(nil), true
		}
		n := *r
		return &n, true
	case *D.DNAME:
		if r == nil {
			return (*D.DNAME)(nil), true
		}
		n := *r
		return &n, true
	case *D.NS:
		if r == nil {
			return (*D.NS)(nil), true
		}
		n := *r
		return &n, true
	case *D.PTR:
		if r == nil {
			return (*D.PTR)(nil), true
		}
		n := *r
		return &n, true
	case *D.SOA:
		if r == nil {
			return (*D.SOA)(nil), true
		}
		// Value-copy header and timers. Ns/Mbox are Go strings (immutable).
		n := *r
		return &n, true
	case nil:
		return nil, true
	default:
		return nil, false
	}
}

func cloneRRs(rrs []D.RR) ([]D.RR, bool) {
	if len(rrs) == 0 {
		return nil, true
	}
	out := make([]D.RR, len(rrs))
	for i, rr := range rrs {
		cloned, ok := cloneShareRdata(rr)
		if !ok {
			return nil, false
		}
		out[i] = cloned
	}
	return out, true
}

// cloneMsg returns a message the caller may mutate without affecting msg.
// RR headers/TTL are unique. A/AAAA IP bytes are copied. CNAME/NS/PTR/DNAME/SOA
// strings are shared (Go strings are immutable). Nested mutable slices on other
// RR types are not shared; those messages use Msg.Copy().
func cloneMsg(msg *D.Msg) *D.Msg {
	if msg == nil {
		return nil
	}
	if !canShareRdata(msg.Answer) || !canShareRdata(msg.Ns) || !canShareRdata(msg.Extra) {
		return msg.Copy()
	}
	answer, ok1 := cloneRRs(msg.Answer)
	ns, ok2 := cloneRRs(msg.Ns)
	extra, ok3 := cloneRRs(msg.Extra)
	if !ok1 || !ok2 || !ok3 {
		return msg.Copy()
	}
	out := new(D.Msg)
	out.MsgHdr = msg.MsgHdr
	out.Compress = msg.Compress
	if n := len(msg.Question); n > 0 {
		out.Question = make([]D.Question, n)
		copy(out.Question, msg.Question)
	}
	out.Answer = answer
	out.Ns = ns
	out.Extra = extra
	return out
}

// extraWithoutOPT returns Extra without OPT RRs. The caller's Extra slice is
// never compacted in place; a new slice is allocated only when OPT is mixed
// with other records.
func extraWithoutOPT(extra []D.RR) []D.RR {
	if len(extra) == 0 {
		return extra
	}
	n := 0
	for _, rr := range extra {
		if rr != nil && rr.Header().Rrtype == D.TypeOPT {
			continue
		}
		n++
	}
	if n == 0 {
		return nil
	}
	if n == len(extra) {
		return extra
	}
	out := make([]D.RR, 0, n)
	for _, rr := range extra {
		if rr != nil && rr.Header().Rrtype == D.TypeOPT {
			continue
		}
		out = append(out, rr)
	}
	return out
}

func dropOPT(extra []D.RR) []D.RR {
	if len(extra) == 0 {
		return extra
	}
	n := 0
	for _, rr := range extra {
		if rr != nil && rr.Header().Rrtype == D.TypeOPT {
			continue
		}
		extra[n] = rr
		n++
	}
	if n == 0 {
		return nil
	}
	for i := n; i < len(extra); i++ {
		extra[i] = nil
	}
	return extra[:n]
}

// getMsgFromCache returns a cached dns message if it exists, otherwise returns nil.
// the returned msg is a copy of the original msg, so it can be modified without affecting the original msg.
func getMsgFromCache(c dnsCache, q D.Question) (*D.Msg, time.Time, bool) {
	msg, expireTime, hit := c.GetWithExpire(cacheKey(q))
	if msg != nil {
		msg = cloneMsg(msg) // never modify the original msg
	}
	return msg, expireTime, hit
}

// peekIPsFromCache copies A/AAAA addresses out of a cache-owned message.
// It never returns cache-owned RR or []byte pointers; netip.AddrFromSlice copies octets.
func peekIPsFromCache(c dnsCache, q D.Question) (ips []netip.Addr, expire time.Time, hit bool) {
	msg, expire, hit := c.GetWithExpire(cacheKey(q))
	if !hit || msg == nil {
		return nil, expire, hit
	}
	return msgToIP(msg), expire, true
}

// putMsgToCache puts a dns message into the cache.
// the msg is copied before being stored in the cache, so it can be modified without affecting the original msg.
func putMsgToCache(c dnsCache, q D.Question, msg *D.Msg) {
	// skip dns cache for acme challenge
	if q.Qtype == D.TypeTXT && strings.HasPrefix(q.Name, "_acme-challenge.") {
		if log.Enabled(log.DEBUG) {
			log.Debugln("[DNS] dns cache ignored because of acme challenge for: %s", q.Name)
		}
		return
	}

	// Header copy so Extra can omit OPT without compacting the caller's slice.
	// OPT.Hdr.Ttl is EDNS extended-RCODE/flags, not a TTL; strip before minTTLAll.
	tmp := *msg
	tmp.Extra = extraWithoutOPT(msg.Extra)

	var ttl uint32
	if tmp.Rcode == D.RcodeServerFailure {
		// [...] a resolver MAY cache a server failure response.
		// If it does so it MUST NOT cache it for longer than five (5) minutes [...]
		ttl = serverFailureCacheTTL
	} else {
		ttl = minTTLAll(tmp.Answer, tmp.Ns, tmp.Extra)
	}
	if ttl == 0 {
		return
	}

	stored := cloneMsg(&tmp) // never modify the original msg
	// OPT RRs MUST NOT be cached, forwarded, or stored in or loaded from master files.
	stored.Extra = dropOPT(stored.Extra)

	c.SetWithExpire(cacheKey(q), stored, time.Now().Add(time.Duration(ttl)*time.Second))
}

func setMsgTTL(msg *D.Msg, ttl uint32) {
	for _, answer := range msg.Answer {
		answer.Header().Ttl = ttl
	}

	for _, ns := range msg.Ns {
		ns.Header().Ttl = ttl
	}

	for _, extra := range msg.Extra {
		if extra.Header().Rrtype == D.TypeOPT { // TTL section in OPT is the extended RCODE and flags (RFC 6891), not real TTL value
			continue
		}
		extra.Header().Ttl = ttl
	}
}

func updateMsgTTL(msg *D.Msg, ttl uint32) {
	updateTTL(msg.Answer, ttl)
	updateTTL(msg.Ns, ttl)
	updateTTL(msg.Extra, ttl)
}

func isIPRequest(q D.Question) bool {
	return q.Qclass == D.ClassINET && (q.Qtype == D.TypeA || q.Qtype == D.TypeAAAA || q.Qtype == D.TypeCNAME)
}

func transform(servers []NameServer, resolver resolver.Resolver) []dnsClient {
	ret := make([]dnsClient, 0, len(servers))
	for _, s := range servers {
		var c dnsClient
		switch s.Net {
		case "tls":
			c = newDoTClient(s.Addr, resolver, s.Params, s.ProxyAdapter, s.ProxyName)
		case "https":
			c = newDoHClient(s.Addr, resolver, s.PreferH3, s.Params, s.ProxyAdapter, s.ProxyName)
		case "dhcp":
			c = newDHCPClient(s.Addr)
		case "system":
			c = newSystemClient()
		case "tailscale":
			c = newTailscaleClient(s.Addr)
		case "rcode":
			c = newRCodeClient(s.Addr)
		case "quic":
			c = newDoQ(s.Addr, resolver, s.Params, s.ProxyAdapter, s.ProxyName)
		default:
			c = newClient(s.Addr, resolver, s.Net, s.Params, s.ProxyAdapter, s.ProxyName)
		}

		c = warpClientWithEdns0Subnet(c, s.Params)
		c = warpClientWithDisableTypes(c, s.Params)

		ret = append(ret, c)
	}
	return ret
}

type clientWithDisableTypes struct {
	dnsClient
	disableTypes map[uint16]struct{}
}

func (c clientWithDisableTypes) ExchangeContext(ctx context.Context, m *D.Msg) (msg *D.Msg, err error) {
	// filter dns request
	if slices.ContainsFunc(m.Question, c.inQuestion) {
		// In fact, DNS requests are not allowed to contain multiple questions:
		// https://stackoverflow.com/questions/4082081/requesting-a-and-aaaa-records-in-single-dns-query/4083071
		// so, when we find a question containing the type, we can simply discard the entire dns request.
		return handleMsgWithEmptyAnswer(m), nil
	}

	// do real exchange
	msg, err = c.dnsClient.ExchangeContext(ctx, m)
	if err != nil {
		return
	}

	// filter dns response
	msg.Answer = slices.DeleteFunc(msg.Answer, c.inRR)
	msg.Ns = slices.DeleteFunc(msg.Ns, c.inRR)
	msg.Extra = slices.DeleteFunc(msg.Extra, c.inRR)
	return
}

func (c clientWithDisableTypes) inQuestion(q D.Question) bool {
	_, ok := c.disableTypes[q.Qtype]
	return ok
}

func (c clientWithDisableTypes) inRR(rr D.RR) bool {
	_, ok := c.disableTypes[rr.Header().Rrtype]
	return ok
}

func warpClientWithDisableTypes(c dnsClient, params map[string]string) dnsClient {
	disableTypes := make(map[uint16]struct{})
	if params["disable-ipv4"] == "true" {
		disableTypes[D.TypeA] = struct{}{}
	}
	if params["disable-ipv6"] == "true" {
		disableTypes[D.TypeAAAA] = struct{}{}
	}
	for key, value := range params {
		const prefix = "disable-qtype-"
		if strings.HasPrefix(key, prefix) && value == "true" { // eg: disable-qtype-65=true
			qType, err := strconv.ParseUint(key[len(prefix):], 10, 16)
			if err != nil {
				continue
			}
			if _, ok := D.TypeToRR[uint16(qType)]; !ok { // check valid RR_Header.Rrtype and Question.qtype
				continue
			}
			disableTypes[uint16(qType)] = struct{}{}
		}
	}
	if len(disableTypes) > 0 {
		return clientWithDisableTypes{c, disableTypes}
	}
	return c
}

type clientWithEdns0Subnet struct {
	dnsClient
	ecsPrefix   netip.Prefix
	ecsOverride bool
}

func (c clientWithEdns0Subnet) ExchangeContext(ctx context.Context, m *D.Msg) (*D.Msg, error) {
	m = m.Copy()
	setEdns0Subnet(m, c.ecsPrefix, c.ecsOverride)
	return c.dnsClient.ExchangeContext(ctx, m)
}

func warpClientWithEdns0Subnet(c dnsClient, params map[string]string) dnsClient {
	var ecsPrefix netip.Prefix
	var ecsOverride bool
	if ecs := params["ecs"]; ecs != "" {
		prefix, err := netip.ParsePrefix(ecs)
		if err != nil {
			addr, err := netip.ParseAddr(ecs)
			if err != nil {
				log.Warnln("DNS [%s] config with invalid ecs: %s", c.Address(), ecs)
			} else {
				ecsPrefix = netip.PrefixFrom(addr, addr.BitLen())
			}
		} else {
			ecsPrefix = prefix
		}
	}

	if ecsPrefix.IsValid() {
		log.Debugln("DNS [%s] config with ecs: %s", c.Address(), ecsPrefix)
		if params["ecs-override"] == "true" {
			ecsOverride = true
		}
		return clientWithEdns0Subnet{c, ecsPrefix, ecsOverride}
	}
	return c
}

func handleMsgWithEmptyAnswer(r *D.Msg) *D.Msg {
	msg := &D.Msg{}
	msg.Answer = []D.RR{}

	msg.SetRcode(r, D.RcodeSuccess)
	msg.Authoritative = true
	msg.RecursionAvailable = true

	return msg
}

func msgToIP(msg *D.Msg) (ips []netip.Addr) {
	for _, answer := range msg.Answer {
		var ip netip.Addr
		switch ans := answer.(type) {
		case *D.AAAA:
			ip, _ = netip.AddrFromSlice(ans.AAAA)
		case *D.A:
			ip, _ = netip.AddrFromSlice(ans.A)
		default:
			continue
		}
		if !ip.IsValid() {
			continue
		}
		ip = ip.Unmap()
		ips = append(ips, ip)
	}
	return
}

func msgToDomain(msg *D.Msg) string {
	if len(msg.Question) > 0 {
		return trimDots(msg.Question[0].Name)
	}

	return ""
}

func msgToQtype(msg *D.Msg) (uint16, string) {
	if len(msg.Question) > 0 {
		qType := msg.Question[0].Qtype
		return qType, D.Type(qType).String()
	}
	return 0, ""
}

func msgToHTTPSRRInfo(msg *D.Msg) string {
	var alpns []string
	var publicName string
	var hasIPv4, hasIPv6 bool

	collect := func(rrs []D.RR) {
		for _, rr := range rrs {
			httpsRR, ok := rr.(*D.HTTPS)
			if !ok {
				continue
			}

			for _, kv := range httpsRR.Value {
				switch v := kv.(type) {
				case *D.SVCBAlpn:
					if len(alpns) == 0 && len(v.Alpn) > 0 {
						alpns = append(alpns, v.Alpn...)
					}
				case *D.SVCBIPv4Hint:
					if len(v.Hint) > 0 {
						hasIPv4 = true
					}
				case *D.SVCBIPv6Hint:
					if len(v.Hint) > 0 {
						hasIPv6 = true
					}
				case *D.SVCBECHConfig:
					if publicName == "" && len(v.ECH) > 0 {
						if cfgs, err := echparser.ParseECHConfigList(v.ECH); err == nil && len(cfgs) > 0 {
							publicName = string(cfgs[0].PublicName)
						}
					}
				}
			}
		}
	}

	collect(msg.Answer)

	// TODO: Do we need to process the data in msg.Extra?
	//      If so, do we need to validate whether the domain names within it match our request?
	//      To simplify the problem, let's ignore it for now.
	// collect(msg.Extra)

	if len(alpns) == 0 && publicName == "" && !hasIPv4 && !hasIPv6 {
		return ""
	}

	var parts []string
	if len(alpns) > 0 {
		parts = append(parts, "alpn:"+strings.Join(alpns, ","))
	}
	if publicName != "" {
		parts = append(parts, "pn:"+publicName)
	}
	if hasIPv4 {
		parts = append(parts, "ipv4hint")
	}
	if hasIPv6 {
		parts = append(parts, "ipv6hint")
	}

	return strings.Join(parts, ";")
}

func msgToLogString(msg *D.Msg) string {
	qType, qTypeStr := msgToQtype(msg)
	switch qType {
	case D.TypeHTTPS:
		return fmt.Sprintf("[%s] %s", msgToHTTPSRRInfo(msg), qTypeStr)
	default:
		return fmt.Sprintf("%s %s", msgToIP(msg), qTypeStr)
	}
}

func batchExchange(ctx context.Context, clients []dnsClient, m *D.Msg) (msg *D.Msg, cache bool, err error) {
	cache = true
	fast, ctx := picker.WithTimeout[*D.Msg](ctx, resolver.DefaultDNSTimeout)
	defer fast.Close()
	debug := log.Enabled(log.DEBUG)
	var domain, qTypeStr string
	if debug {
		domain = msgToDomain(m)
		_, qTypeStr = msgToQtype(m)
	}
	for _, client := range clients {
		if _, isRCodeClient := client.(rcodeClient); isRCodeClient {
			msg, err = client.ExchangeContext(ctx, m)
			return msg, false, err
		}
		client := client // shadow define client to ensure the value captured by the closure will not be changed in the next loop
		fast.Go(func() (*D.Msg, error) {
			if debug {
				log.Debugln("[DNS] resolve %s %s from %s", domain, qTypeStr, client.Address())
			}
			m, err := client.ExchangeContext(ctx, m)
			if err != nil {
				return nil, err
			} else if cache && (m.Rcode == D.RcodeServerFailure || m.Rcode == D.RcodeRefused) {
				// currently, cache indicates whether this msg was from a RCode client,
				// so we would ignore RCode errors from RCode clients.
				return nil, errors.New("server failure: " + D.RcodeToString[m.Rcode])
			}
			if debug {
				log.Debugln("[DNS] %s --> %s from %s", domain, msgToLogString(m), client.Address())
			}
			return m, nil
		})
	}

	msg = fast.Wait()
	if msg == nil {
		err = errors.New("all DNS requests failed")
		if fErr := fast.Error(); fErr != nil {
			err = fmt.Errorf("%w, first error: %w", err, fErr)
		}
	}
	return
}
