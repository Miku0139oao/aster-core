package dns

import (
	"net/netip"
	"strings"
	"time"

	"github.com/Miku0139oao/aster-core/common/lru"
	"github.com/Miku0139oao/aster-core/component/fakeip"
	"github.com/Miku0139oao/aster-core/component/kerneldirect"
	"github.com/Miku0139oao/aster-core/component/resolver"
	C "github.com/Miku0139oao/aster-core/constant"
	icontext "github.com/Miku0139oao/aster-core/context"
	"github.com/Miku0139oao/aster-core/log"

	D "github.com/miekg/dns"
)

type (
	handler    func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error)
	middleware func(next handler) handler
)

func withKernelDirectObservation() middleware {
	return func(next handler) handler {
		return func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) {
			msg, err := next(ctx, r)
			if err != nil || msg == nil || r == nil || len(r.Question) != 1 || !kerneldirect.HasConsumers() {
				return msg, err
			}
			question := r.Question[0]
			answers := collectKernelDirectAnswers(question, msg)
			if len(answers) != 0 {
				kerneldirect.ObserveDNS(dnsNameKey(question.Name), answers)
			}
			return msg, nil
		}
	}
}

func dnsNameKey(name string) string {
	// Match strings.TrimSuffix(name, ".") (one trailing dot), not TrimRight.
	if n := len(name); n > 0 && name[n-1] == '.' {
		name = name[:n-1]
	}
	if !hasUpperASCII(name) {
		return name
	}
	return strings.ToLower(name)
}

func hasUpperASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			return true
		}
	}
	return false
}

const (
	kernelDirectMaxAliasExpansions = 64
	kernelDirectMaxAliasRecords    = 256
	kernelDirectMaxAnswers         = 64
)

type kernelDirectAlias struct {
	target string
	ttl    uint32
}

func kernelDirectAliasTarget(name string) (string, bool) {
	target := dnsNameKey(name)
	return target, target != "" || name == "."
}

func kernelDirectDNAMEAlias(name, owner, target string) (string, bool) {
	nameLabels := D.SplitDomainName(name)
	ownerLabels := D.SplitDomainName(owner)
	if len(ownerLabels) == 0 || len(nameLabels) <= len(ownerLabels) {
		return "", false
	}
	nameSuffix := nameLabels[len(nameLabels)-len(ownerLabels):]
	for i := range ownerLabels {
		if nameSuffix[i] != ownerLabels[i] {
			return "", false
		}
	}
	targetLabels := D.SplitDomainName(target)
	labels := append([]string{}, nameLabels[:len(nameLabels)-len(ownerLabels)]...)
	return strings.Join(append(labels, targetLabels...), "."), true
}

func mergeKernelDirectAlias(aliases map[string]kernelDirectAlias, owner string, alias kernelDirectAlias) bool {
	if owner == "" {
		return false
	}
	if current, exists := aliases[owner]; exists {
		if current.target != alias.target {
			return false
		}
		if alias.ttl < current.ttl {
			aliases[owner] = alias
		}
		return true
	}
	aliases[owner] = alias
	return true
}

func kernelDirectTerminal(question D.Question, msg *D.Msg) (string, uint32, bool) {
	query := dnsNameKey(question.Name)
	if query == "" {
		return "", 0, false
	}
	cnames := make(map[string]kernelDirectAlias)
	dnames := make(map[string]kernelDirectAlias)
	aliasRecords := 0
	for _, rr := range msg.Answer {
		switch rec := rr.(type) {
		case *D.CNAME:
			if rec == nil || rec.Hdr.Class != D.ClassINET {
				continue
			}
			target, ok := kernelDirectAliasTarget(rec.Target)
			if !ok || !mergeKernelDirectAlias(cnames, dnsNameKey(rec.Hdr.Name), kernelDirectAlias{target: target, ttl: rec.Hdr.Ttl}) {
				return "", 0, false
			}
			aliasRecords++
		case *D.DNAME:
			if rec == nil || rec.Hdr.Class != D.ClassINET {
				continue
			}
			target, ok := kernelDirectAliasTarget(rec.Target)
			if !ok || !mergeKernelDirectAlias(dnames, dnsNameKey(rec.Hdr.Name), kernelDirectAlias{target: target, ttl: rec.Hdr.Ttl}) {
				return "", 0, false
			}
			aliasRecords++
		}
		if aliasRecords > kernelDirectMaxAliasRecords {
			return "", 0, false
		}
	}

	name := query
	pathTTL := ^uint32(0)
	seen := map[string]struct{}{name: {}}
	for expansion := 0; expansion < kernelDirectMaxAliasExpansions; expansion++ {
		cname, cnameFound := cnames[name]
		var (
			dnameTarget string
			dnameTTL    uint32
			bestLabels  = -1
		)
		for owner, alias := range dnames {
			target, ok := kernelDirectDNAMEAlias(name, owner, alias.target)
			if !ok {
				continue
			}
			labels := len(D.SplitDomainName(owner))
			if labels > bestLabels {
				bestLabels, dnameTarget, dnameTTL = labels, target, alias.ttl
			} else if labels == bestLabels {
				if target != dnameTarget {
					return "", 0, false
				}
				if alias.ttl < dnameTTL {
					dnameTTL = alias.ttl
				}
			}
		}

		var (
			next       string
			transition uint32
			found      bool
		)
		switch {
		case cnameFound && bestLabels >= 0:
			if cname.target != dnameTarget {
				return "", 0, false
			}
			next, transition, found = cname.target, cname.ttl, true
			if dnameTTL < transition {
				transition = dnameTTL
			}
		case cnameFound:
			next, transition, found = cname.target, cname.ttl, true
		case bestLabels >= 0:
			next, transition, found = dnameTarget, dnameTTL, true
		}
		if !found {
			return name, pathTTL, true
		}
		if _, exists := seen[next]; exists {
			return "", 0, false
		}
		seen[next] = struct{}{}
		if transition < pathTTL {
			pathTTL = transition
		}
		name = next
	}
	return "", 0, false
}

func appendKernelDirectAnswer(answers []kerneldirect.DNSAnswer, addr netip.Addr, ttl uint32) []kerneldirect.DNSAnswer {
	addr = addr.Unmap()
	if !addr.IsValid() || resolver.IsFakeIP(addr) {
		return answers
	}
	effectiveTTL := time.Duration(ttl) * time.Second
	for i := range answers {
		if answers[i].Addr == addr {
			if effectiveTTL < answers[i].TTL {
				answers[i].TTL = effectiveTTL
			}
			return answers
		}
	}
	if len(answers) >= kernelDirectMaxAnswers {
		return answers
	}
	return append(answers, kerneldirect.DNSAnswer{Addr: addr, TTL: effectiveTTL})
}

func collectKernelDirectAnswers(question D.Question, msg *D.Msg) []kerneldirect.DNSAnswer {
	if msg == nil || !msg.Response || msg.Truncated || msg.Rcode != D.RcodeSuccess || question.Qclass != D.ClassINET || (question.Qtype != D.TypeA && question.Qtype != D.TypeAAAA) {
		return nil
	}
	terminal, pathTTL, ok := kernelDirectTerminal(question, msg)
	if !ok {
		return nil
	}
	answers := make([]kerneldirect.DNSAnswer, 0, 2)
	for _, section := range [][]D.RR{msg.Answer, msg.Extra} {
		for _, rr := range section {
			var (
				addr netip.Addr
				ttl  uint32
				name string
			)
			switch rec := rr.(type) {
			case *D.A:
				if rec == nil || question.Qtype != D.TypeA || rec.Hdr.Class != D.ClassINET {
					continue
				}
				addr, _ = netip.AddrFromSlice(rec.A)
				ttl, name = rec.Hdr.Ttl, rec.Hdr.Name
			case *D.AAAA:
				if rec == nil || question.Qtype != D.TypeAAAA || rec.Hdr.Class != D.ClassINET {
					continue
				}
				addr, _ = netip.AddrFromSlice(rec.AAAA)
				ttl, name = rec.Hdr.Ttl, rec.Hdr.Name
			default:
				continue
			}
			if dnsNameKey(name) != terminal {
				continue
			}
			if pathTTL < ttl {
				ttl = pathTTL
			}
			answers = appendKernelDirectAnswer(answers, addr, ttl)
		}
	}
	return answers
}

func withHosts(mapping *lru.LruCache[netip.Addr, string]) middleware {
	return func(next handler) handler {
		return func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) {
			q := r.Question[0]

			if !isIPRequest(q) {
				return next(ctx, r)
			}

			host := trimDots(q.Name)
			handleCName := func(resp *D.Msg, domain string) {
				rr := &D.CNAME{}
				rr.Hdr = D.RR_Header{Name: q.Name, Rrtype: D.TypeCNAME, Class: D.ClassINET, Ttl: 10}
				rr.Target = domain + "."
				resp.Answer = append([]D.RR{rr}, resp.Answer...)
			}
			record, ok := resolver.DefaultHosts.Search(host, q.Qtype != D.TypeA && q.Qtype != D.TypeAAAA)
			if !ok {
				if record != nil && record.IsDomain {
					// replace request domain
					newR := r.Copy()
					newR.Question[0].Name = record.Domain + "."
					resp, err := next(ctx, newR)
					if err == nil {
						resp.Id = r.Id
						resp.Question = r.Question
						handleCName(resp, record.Domain)
					}
					return resp, err
				}
				return next(ctx, r)
			}

			msg := r.Copy()
			handleIPs := func() {
				for _, ipAddr := range record.IPs {
					if ipAddr.Is4() && q.Qtype == D.TypeA {
						rr := &D.A{}
						rr.Hdr = D.RR_Header{Name: q.Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 10}
						rr.A = ipAddr.AsSlice()
						msg.Answer = append(msg.Answer, rr)
						if mapping != nil {
							mapping.SetWithExpire(ipAddr, host, time.Now().Add(time.Second*10))
						}
					} else if ipAddr.Is6() && q.Qtype == D.TypeAAAA {
						rr := &D.AAAA{}
						rr.Hdr = D.RR_Header{Name: q.Name, Rrtype: D.TypeAAAA, Class: D.ClassINET, Ttl: 10}
						rr.AAAA = ipAddr.AsSlice()
						msg.Answer = append(msg.Answer, rr)
						if mapping != nil {
							mapping.SetWithExpire(ipAddr, host, time.Now().Add(time.Second*10))
						}
					}
				}
			}

			switch q.Qtype {
			case D.TypeA:
				handleIPs()
			case D.TypeAAAA:
				handleIPs()
			case D.TypeCNAME:
				handleCName(msg, record.Domain)
			default:
				return next(ctx, r)
			}

			ctx.SetType(icontext.DNSTypeHost)
			msg.SetRcode(r, D.RcodeSuccess)
			msg.Authoritative = true
			msg.RecursionAvailable = true
			return msg, nil
		}
	}
}

func withMapping(mapping *lru.LruCache[netip.Addr, string]) middleware {
	return func(next handler) handler {
		return func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) {
			q := r.Question[0]

			if !isIPRequest(q) {
				return next(ctx, r)
			}

			msg, err := next(ctx, r)
			if err != nil {
				return nil, err
			}

			host := trimDots(q.Name)

			for _, ans := range msg.Answer {
				var ip netip.Addr
				var ttl uint32

				switch a := ans.(type) {
				case *D.A:
					ip, _ = netip.AddrFromSlice(a.A)
					ttl = a.Hdr.Ttl
				case *D.AAAA:
					ip, _ = netip.AddrFromSlice(a.AAAA)
					ttl = a.Hdr.Ttl
				default:
					continue
				}
				if !ip.IsValid() {
					continue
				}
				if !ip.IsGlobalUnicast() {
					continue
				}
				ip = ip.Unmap()

				if ttl < 1 {
					ttl = 1
				}

				mapping.SetWithExpire(ip, host, time.Now().Add(time.Second*time.Duration(ttl)))
			}

			return msg, nil
		}
	}
}

func withFakeIP(skipper *fakeip.Skipper, fakePool *fakeip.Pool, fakePool6 *fakeip.Pool, fakeIPTTL int) middleware {
	return func(next handler) handler {
		return func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) {
			q := r.Question[0]

			host := trimDots(q.Name)
			if skipper.ShouldSkipped(host) {
				return next(ctx, r)
			}

			var rr D.RR
			switch q.Qtype {
			case D.TypeA:
				if fakePool == nil {
					return handleMsgWithEmptyAnswer(r), nil
				}
				ip := fakePool.Lookup(host)
				rr = &D.A{
					Hdr: D.RR_Header{Name: q.Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: dnsDefaultTTL},
					A:   ip.AsSlice(),
				}
			case D.TypeAAAA:
				if fakePool6 == nil {
					return handleMsgWithEmptyAnswer(r), nil
				}
				ip := fakePool6.Lookup(host)
				rr = &D.AAAA{
					Hdr:  D.RR_Header{Name: q.Name, Rrtype: D.TypeAAAA, Class: D.ClassINET, Ttl: dnsDefaultTTL},
					AAAA: ip.AsSlice(),
				}
			case D.TypeSVCB, D.TypeHTTPS:
				return handleMsgWithEmptyAnswer(r), nil
			default:
				return next(ctx, r)
			}

			msg := r.Copy()
			msg.Answer = []D.RR{rr}

			ctx.SetType(icontext.DNSTypeFakeIP)
			setMsgTTL(msg, uint32(fakeIPTTL))
			msg.SetRcode(r, D.RcodeSuccess)
			msg.Authoritative = true
			msg.RecursionAvailable = true

			return msg, nil
		}
	}
}

func withResolver(resolver resolver.Resolver, ipv6 bool) handler {
	return func(ctx *icontext.DNSContext, r *D.Msg) (*D.Msg, error) {
		ctx.SetType(icontext.DNSTypeRaw)

		q := r.Question[0]

		// return a empty AAAA msg when ipv6 disabled
		if !ipv6 && q.Qtype == D.TypeAAAA {
			return handleMsgWithEmptyAnswer(r), nil
		}

		msg, err := resolver.ExchangeContext(ctx, r)
		if err != nil {
			if log.Enabled(log.DEBUG) {
				log.Debugln("[DNS Server] Exchange %s failed: %v", q.String(), err)
			}
			return msg, err
		}
		msg.SetRcode(r, msg.Rcode)
		msg.Authoritative = true

		return msg, nil
	}
}

func compose(middlewares []middleware, endpoint handler) handler {
	length := len(middlewares)
	h := endpoint
	for i := length - 1; i >= 0; i-- {
		middleware := middlewares[i]
		h = middleware(h)
	}

	return h
}

func newHandler(resolver resolver.Resolver, mapper *ResolverEnhancer) handler {
	middlewares := []middleware{withKernelDirectObservation()}

	if mapper.useHosts {
		middlewares = append(middlewares, withHosts(mapper.mapping))
	}

	if mapper.mode == C.DNSFakeIP {
		middlewares = append(middlewares, withFakeIP(mapper.fakeIPSkipper, mapper.fakeIPPool, mapper.fakeIPPool6, mapper.fakeIPTTL))
	}

	if mapper.mode != C.DNSNormal {
		middlewares = append(middlewares, withMapping(mapper.mapping))
	}

	return compose(middlewares, withResolver(resolver, mapper.ipv6))
}
