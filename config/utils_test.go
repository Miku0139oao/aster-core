package config

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDialerProxies(t *testing.T) {
	testCases := []struct {
		testName string
		proxy    []map[string]any
		// the proxy named in a cycle error depends on map iteration order,
		// so any of these errors is accepted; nil means no error
		errs []string
	}{
		{
			testName: "ValidReference",
			proxy: []map[string]any{ // create proxy with valid dialer-proxy reference
				{"name": "base-proxy", "type": "socks5", "server": "127.0.0.1", "port": 1080},
				{"name": "proxy-with-dialer", "type": "socks5", "server": "127.0.0.1", "port": 1081, "dialer-proxy": "base-proxy"},
			},
		},
		{
			testName: "NotFoundReference",
			proxy: []map[string]any{ // create proxy with non-existent dialer-proxy reference
				{"name": "base-proxy", "type": "socks5", "server": "127.0.0.1", "port": 1080},
				{"name": "valid-proxy-with-dialer", "type": "socks5", "server": "127.0.0.1", "port": 1081, "dialer-proxy": "base-proxy"},
				{"name": "proxy-with-dialer", "type": "socks5", "server": "127.0.0.1", "port": 1081, "dialer-proxy": "non-existent-proxy"},
			},
			errs: []string{"proxy [proxy-with-dialer] dialer-proxy [non-existent-proxy] not found"},
		},
		{
			testName: "CircularDependency",
			proxy: []map[string]any{ // proxy-b -> proxy-a <-> proxy-c
				{"name": "proxy-a", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "proxy-c"},
				{"name": "proxy-b", "type": "socks5", "server": "127.0.0.1", "port": 1081, "dialer-proxy": "proxy-a"},
				{"name": "proxy-c", "type": "socks5", "server": "127.0.0.1", "port": 1082, "dialer-proxy": "proxy-a"},
			},
			errs: []string{
				"proxy [proxy-a] has circular dialer-proxy dependency",
				"proxy [proxy-b] has circular dialer-proxy dependency",
				"proxy [proxy-c] has circular dialer-proxy dependency",
			},
		},
		{
			testName: "ComplexChain",
			proxy: []map[string]any{ // proxy-d, proxy-e and proxy-f share the chain proxy-c -> proxy-b -> proxy-a
				{"name": "proxy-a", "type": "socks5", "server": "127.0.0.1", "port": 1080},
				{"name": "proxy-b", "type": "socks5", "server": "127.0.0.1", "port": 1081, "dialer-proxy": "proxy-a"},
				{"name": "proxy-c", "type": "socks5", "server": "127.0.0.1", "port": 1082, "dialer-proxy": "proxy-b"},
				{"name": "proxy-d", "type": "socks5", "server": "127.0.0.1", "port": 1083, "dialer-proxy": "proxy-c"},
				{"name": "proxy-e", "type": "socks5", "server": "127.0.0.1", "port": 1084, "dialer-proxy": "proxy-c"},
				{"name": "proxy-f", "type": "socks5", "server": "127.0.0.1", "port": 1085, "dialer-proxy": "proxy-c"},
			},
		},
		{
			testName: "EmptyDialerProxy",
			proxy: []map[string]any{ // create proxy without dialer-proxy
				{"name": "simple-proxy", "type": "socks5", "server": "127.0.0.1", "port": 1080},
				{"name": "another-simple-proxy", "type": "socks5", "server": "127.0.0.1", "port": 1081},
			},
		},
		{
			testName: "SelfReference",
			proxy: []map[string]any{ // create proxy that references itself
				{"name": "base-proxy", "type": "socks5", "server": "127.0.0.1", "port": 1080},
				{"name": "self-proxy", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "self-proxy"},
			},
			errs: []string{"proxy [self-proxy] has circular dialer-proxy dependency"},
		},
		{
			testName: "NotFoundBeforeCycle",
			proxy: []map[string]any{
				{"name": "a", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "b"},
				{"name": "b", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "a"},
				{"name": "c", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "missing"},
			},
			errs: []string{"proxy [c] dialer-proxy [missing] not found"},
		},
		{
			testName: "TwoProxyCycle",
			proxy: []map[string]any{
				{"name": "a", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "b"},
				{"name": "b", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "a"},
				{"name": "c", "type": "socks5", "server": "127.0.0.1", "port": 1080},
			},
			errs: []string{
				"proxy [a] has circular dialer-proxy dependency",
				"proxy [b] has circular dialer-proxy dependency",
			},
		},
		{
			testName: "LongChainIntoCycle",
			// x -> a -> b -> c -> d -> e -> c, beside an acyclic chain y -> z, w -> z
			proxy: []map[string]any{
				{"name": "x", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "a"},
				{"name": "a", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "b"},
				{"name": "b", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "c"},
				{"name": "c", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "d"},
				{"name": "d", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "e"},
				{"name": "e", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "c"},
				{"name": "y", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "z"},
				{"name": "z", "type": "socks5", "server": "127.0.0.1", "port": 1080},
				{"name": "w", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "z"},
			},
			errs: []string{
				"proxy [x] has circular dialer-proxy dependency",
				"proxy [a] has circular dialer-proxy dependency",
				"proxy [b] has circular dialer-proxy dependency",
				"proxy [c] has circular dialer-proxy dependency",
				"proxy [d] has circular dialer-proxy dependency",
				"proxy [e] has circular dialer-proxy dependency",
			},
		},
		{
			testName: "SharedRelayBesideCycle",
			// p and q share an acyclic relay, t leads into the cycle r <-> s
			proxy: []map[string]any{
				{"name": "p", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "relay"},
				{"name": "q", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "relay"},
				{"name": "relay", "type": "socks5", "server": "127.0.0.1", "port": 1080},
				{"name": "r", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "s"},
				{"name": "s", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "r"},
				{"name": "t", "type": "socks5", "server": "127.0.0.1", "port": 1080, "dialer-proxy": "s"},
			},
			errs: []string{
				"proxy [r] has circular dialer-proxy dependency",
				"proxy [s] has circular dialer-proxy dependency",
				"proxy [t] has circular dialer-proxy dependency",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.testName, func(t *testing.T) {
			config := RawConfig{Proxy: testCase.proxy}
			// repeat so that different map iteration orders are covered
			for i := 0; i < 20; i++ {
				_, _, err := parseProxies(&config)
				if testCase.errs == nil {
					assert.NoError(t, err)
				} else if assert.Error(t, err) {
					assert.Contains(t, testCase.errs, err.Error())
				}
			}
		})
	}
}

// linearDialerGraph builds a single dialer-proxy chain of n proxies, where
// every proxy dials through the previous one.
func linearDialerGraph(n int) map[string]string {
	graph := make(map[string]string, n)
	for i := 1; i < n; i++ {
		graph[fmt.Sprintf("proxy-%d", i)] = fmt.Sprintf("proxy-%d", i-1)
	}
	return graph
}

// TestValidateDialerProxiesHasCycleLinear checks that a long chain is accepted
// and, via the allocated-bytes bound, that the walk does not allocate per
// starting node as the old quadratic implementation did.
func TestValidateDialerProxiesHasCycleLinear(t *testing.T) {
	const n = 2000

	graph := linearDialerGraph(n)
	start := time.Now()
	require.NoError(t, validateDialerProxiesHasCycle(graph))
	elapsed := time.Since(start)

	// The linear walk takes well under a millisecond; the old quadratic one
	// needed 13s at n=3000.  The bound is deliberately generous so it only
	// catches a return to quadratic behaviour, not slow CI machines.
	assert.Less(t, elapsed, 500*time.Millisecond, "cycle check should be linear, took %s for %d proxies", elapsed, n)

	// A cycle appended at the end of the same chain must still be reported.
	graph["proxy-0"] = fmt.Sprintf("proxy-%d", n-1)
	assert.ErrorContains(t, validateDialerProxiesHasCycle(graph), "circular dialer-proxy dependency")
}

func BenchmarkValidateDialerProxiesHasCycle(b *testing.B) {
	for _, n := range []int{500, 2000, 8000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			graph := linearDialerGraph(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := validateDialerProxiesHasCycle(graph); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestValidateDialerProxiesHasCycleAllocs guards the memory side of the fix:
// bytes allocated must grow with the graph size, not with its square.  The
// quadratic implementation allocated a fresh visited map and path slice for
// every starting node, so its footprint grew with n*n.
func TestValidateDialerProxiesHasCycleAllocs(t *testing.T) {
	// Measure a single walk of each graph instead of using testing.Benchmark,
	// which would run for its own time budget and make the suite slow.  A
	// quadratic implementation allocates a map and a slice per starting node,
	// so a fixed single-shot measurement still separates the two shapes.
	measure := func(n int) uint64 {
		graph := linearDialerGraph(n)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		if err := validateDialerProxiesHasCycle(graph); err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}

	const factor = 100 // 100 -> 10000
	small := measure(100)
	large := measure(100 * factor)
	require.NotZero(t, small, "expected the walk to allocate")

	// Linear growth is roughly `factor` times; quadratic growth would be about
	// factor^2 = 10000 times.  The threshold only trips on a quadratic return.
	assert.Less(t, large, small*4*factor,
		"allocated bytes should grow linearly: %d bytes for 100 nodes, %d bytes for %d nodes",
		small, large, 100*factor)
}
