package dns

import (
	"bytes"
	"net"
	"testing"

	D "github.com/miekg/dns"
)

func TestCacheHitIsolationAcrossEncodings(t *testing.T) {
	for _, algorithm := range []string{"lru", "arc"} {
		for _, kind := range []uint16{D.TypeA, D.TypeAAAA, D.TypeCNAME, D.TypeTXT} {
			t.Run(algorithm+"/"+D.Type(kind).String(), func(t *testing.T) {
				cache := Config{CacheAlgorithm: algorithm}.newCache()
				req := new(D.Msg)
				req.SetQuestion("isolation.example.", kind)
				resp := new(D.Msg)
				resp.SetReply(req)
				hdr := D.RR_Header{Name: req.Question[0].Name, Rrtype: kind, Class: D.ClassINET, Ttl: 300}
				switch kind {
				case D.TypeA:
					resp.Answer = []D.RR{&D.A{Hdr: hdr, A: net.IPv4(192, 0, 2, 7).To4()}}
				case D.TypeAAAA:
					resp.Answer = []D.RR{&D.AAAA{Hdr: hdr, AAAA: net.ParseIP("2001:db8::7")}}
				case D.TypeCNAME:
					resp.Answer = []D.RR{&D.CNAME{Hdr: hdr, Target: "canonical.example."}}
				case D.TypeTXT:
					resp.Answer = []D.RR{&D.TXT{Hdr: hdr, Txt: []string{"original"}}}
				}
				putMsgToCache(cache, req.Question[0], resp)
				first, expire, hit := getMsgFromCache(cache, req.Question[0])
				if !hit || first == nil {
					t.Fatal("first cache hit missing")
				}
				want, err := first.Pack()
				if err != nil {
					t.Fatal(err)
				}
				first.Id++
				first.Question[0].Name = "changed.example."
				first.Answer[0].Header().Ttl = 1
				switch rr := first.Answer[0].(type) {
				case *D.A:
					rr.A[0] ^= 0xff
				case *D.AAAA:
					rr.AAAA[0] ^= 0xff
				case *D.CNAME:
					rr.Target = "changed.example."
				case *D.TXT:
					rr.Txt[0] = "changed"
				}
				second, expire2, hit := getMsgFromCache(cache, req.Question[0])
				if !hit || second == nil || !expire.Equal(expire2) {
					t.Fatal("second cache hit or expiration changed")
				}
				got, err := second.Pack()
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("caller mutated cache: got %v, err %v", second, err)
				}
			})
		}
	}
}
