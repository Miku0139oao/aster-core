package kerneldirect

import (
	"net/netip"
	"testing"
	"time"
)

// Public-API A/B harness. These benches use only Register, ObserveDNS,
// ObserveFlow, Statuses, Flush, and Close so the same source can run on
// 4a59a634 and on this branch. Fill allocation (B/op during ObserveDNS) is
// reported separately from retained RAM; do not treat ns/op or HeapInuse as
// a claimed win here. Parent serial process A/B is the RAM source of truth.
//
// Caps stay at production defaults unless a control needs a smaller bound
// to force eviction. Do not lower DefaultMaxEntries/MaximumMaxEntries.

const harnessFillN = 4096

func harnessAnswers(n int) ([]netip.Addr, [][]DNSAnswer) {
	addrs := make([]netip.Addr, n)
	answers := make([][]DNSAnswer, n)
	for i := 0; i < n; i++ {
		addr := netip.AddrFrom4([4]byte{203, byte(i >> 16), byte(i >> 8), byte(i)})
		addrs[i] = addr
		answers[i] = []DNSAnswer{{Addr: addr, TTL: time.Minute}}
	}
	return addrs, answers
}

func BenchmarkKernelDirectSingleHostFill(b *testing.B) {
	_, answers := harnessAnswers(harnessFillN)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: DefaultMaxEntries})
		for _, answer := range answers {
			ObserveDNS("fill.example", answer)
		}
		_ = Statuses()
		_ = c.Close()
	}
}

func BenchmarkKernelDirectMultiHostFill(b *testing.B) {
	_, answers := harnessAnswers(256)
	hosts := []string{"a.example", "b.example", "c.example", "d.example"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: DefaultMaxEntries})
		for _, host := range hosts {
			for _, answer := range answers {
				ObserveDNS(host, answer)
			}
		}
		_ = Statuses()
		_ = c.Close()
	}
}

func BenchmarkKernelDirectSteadyRefresh(b *testing.B) {
	addrs, answers := harnessAnswers(256)
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: 256})
	defer c.Close()
	for _, answer := range answers {
		ObserveDNS("bench.example", answer)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ObserveDNS("bench.example", answers[i%len(answers)])
		_ = addrs[i%len(addrs)]
	}
}

func BenchmarkKernelDirectSteadyEvict(b *testing.B) {
	const capN = 256
	c := Register(func(string, netip.Addr) bool { return true }, func(DecisionSets) {}, ControllerOptions{MaxEntries: capN})
	defer c.Close()
	_, fill := harnessAnswers(capN)
	for _, answer := range fill {
		ObserveDNS("seed.example", answer)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := capN + i + 1
		addr := netip.AddrFrom4([4]byte{203, byte(n >> 16), byte(n >> 8), byte(n)})
		ObserveDNS("evict.example", []DNSAnswer{{Addr: addr, TTL: time.Minute}})
	}
}
