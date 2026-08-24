//go:build linux

package kerneldirect

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
	"go4.org/netipx"
)

func TestBuildLPMEntriesLongestPrefixAndProxyWins(t *testing.T) {
	var directBuilder netipx.IPSetBuilder
	directBuilder.Add(netip.MustParseAddr("8.8.8.8"))
	direct, err := directBuilder.IPSet()
	require.NoError(t, err)
	var proxyBuilder netipx.IPSetBuilder
	proxyBuilder.Add(netip.MustParseAddr("1.1.1.1"))
	proxy, err := proxyBuilder.IPSet()
	require.NoError(t, err)

	entries4, entries6, err := buildLPMEntries(
		DecisionSets{Direct: direct, Proxy: proxy},
		[]netip.Prefix{netip.MustParsePrefix("8.8.0.0/16"), netip.MustParsePrefix("9.9.0.0/16")},
		[]netip.Prefix{netip.MustParsePrefix("9.9.0.0/16")},
		[]netip.Prefix{netip.MustParsePrefix("9.9.9.9/32")},
		true,
		16,
	)
	require.NoError(t, err)
	require.Equal(t, decisionProxy, entries4[lpmKey4{PrefixLen: 0, Address: [4]byte{}}])
	require.Equal(t, decisionProxy, entries6[lpmKey6{PrefixLen: 0, Address: [16]byte{}}])
	require.Equal(t, decisionDirect, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("8.8.8.8").As4()}])
	require.Equal(t, decisionProxy, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("1.1.1.1").As4()}])
	require.Equal(t, decisionProxy, entries4[lpmKey4{PrefixLen: 16, Address: netip.MustParseAddr("9.9.0.0").As4()}], "PROXY must win an exact-prefix collision")
	require.Equal(t, decisionBypass, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("9.9.9.9").As4()}], "local host bypass must win over PROXY /0")
}

func mustIPSet(t *testing.T, addrs ...string) *netipx.IPSet {
	t.Helper()
	var builder netipx.IPSetBuilder
	for _, addr := range addrs {
		builder.Add(netip.MustParseAddr(addr))
	}
	set, err := builder.IPSet()
	require.NoError(t, err)
	return set
}

func TestBuildLPMEntriesReservesBypassAndProxyDefaults(t *testing.T) {
	// Non-adjacent addresses so IPSet.Prefixes() cannot aggregate them.
	direct := mustIPSet(t, "203.0.113.1", "198.51.100.1", "8.8.8.8", "1.1.1.1", "9.9.9.9", "4.4.4.4", "8.8.4.4", "1.0.0.1")

	entries4, entries6, err := buildLPMEntries(
		DecisionSets{Direct: direct},
		nil,
		nil,
		[]netip.Prefix{netip.MustParsePrefix("192.0.2.1/32")},
		true,
		4,
	)
	require.NoError(t, err)
	require.Equal(t, decisionBypass, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("192.0.2.1").As4()}])
	require.Equal(t, decisionProxy, entries4[lpmKey4{PrefixLen: 0, Address: [4]byte{}}])
	require.Equal(t, decisionProxy, entries6[lpmKey6{PrefixLen: 0, Address: [16]byte{}}])
	require.Equal(t, 1, len(entries6), "IPv6 default route must stay reserved")
	require.LessOrEqual(t, len(entries4)+len(entries6), 4)

	learned := 0
	for key, decision := range entries4 {
		if key.PrefixLen == 0 || decision == decisionBypass {
			continue
		}
		require.Equal(t, decisionDirect, decision)
		learned++
	}
	require.Equal(t, 1, learned, "exactly one learned DIRECT prefix should fit beside reserved keys")
	require.Equal(t, 3, len(entries4))
}

func TestBuildLPMEntriesPrioritizesLearnedProxyUnderCapacity(t *testing.T) {
	direct := mustIPSet(t, "1.1.1.1")
	proxy := mustIPSet(t, "8.8.8.8")
	entries4, _, err := buildLPMEntries(
		DecisionSets{Direct: direct, Proxy: proxy},
		[]netip.Prefix{netip.MustParsePrefix("8.8.8.0/24")},
		nil,
		nil,
		true,
		4,
	)
	require.NoError(t, err)
	require.Equal(t, decisionProxy, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("8.8.8.8").As4()}], "learned PROXY must win the only learned slot")
	_, keptDirect := entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("1.1.1.1").As4()}]
	require.False(t, keptDirect, "DIRECT must be dropped before PROXY under capacity pressure")
}

func TestBuildLPMEntriesZeroLearnedBudgetKeepsReservedKeys(t *testing.T) {
	direct := mustIPSet(t, "203.0.113.1", "198.51.100.1")
	entries4, entries6, err := buildLPMEntries(
		DecisionSets{Direct: direct},
		nil,
		nil,
		[]netip.Prefix{netip.MustParsePrefix("192.0.2.1/32")},
		true,
		3,
	)
	require.NoError(t, err)
	require.Equal(t, decisionBypass, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("192.0.2.1").As4()}])
	require.Equal(t, decisionProxy, entries4[lpmKey4{PrefixLen: 0, Address: [4]byte{}}])
	require.Equal(t, decisionProxy, entries6[lpmKey6{PrefixLen: 0, Address: [16]byte{}}])
	require.Equal(t, 2, len(entries4))
	require.Equal(t, 1, len(entries6))
}

func TestBuildLPMEntriesProxyWinsExistingKeyWhenBudgetExhausted(t *testing.T) {
	// overhead is 1 static DIRECT + 2 PROXY defaults; learned budget is 0.
	// The colliding PROXY prefix must still overwrite the reserved DIRECT key.
	proxy := mustIPSet(t, "8.8.8.8", "1.1.1.1")
	entries4, _, err := buildLPMEntries(
		DecisionSets{Proxy: proxy},
		[]netip.Prefix{netip.MustParsePrefix("8.8.8.8/32")},
		nil,
		nil,
		true,
		3,
	)
	require.NoError(t, err)
	require.Equal(t, decisionProxy, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("8.8.8.8").As4()}], "PROXY must win an exact-prefix collision even with no learned budget")
	_, learnedExtra := entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("1.1.1.1").As4()}]
	require.False(t, learnedExtra, "new PROXY keys must still respect the learned budget")
	require.Equal(t, decisionProxy, entries4[lpmKey4{PrefixLen: 0, Address: [4]byte{}}])
}

func TestBuildLPMEntriesLearnedOverlapDoesNotConsumeBudget(t *testing.T) {
	// 8.8.8.8 is already reserved as static DIRECT. It must not spend the
	// single learned slot that 1.1.1.1 needs.
	direct := mustIPSet(t, "8.8.8.8", "1.1.1.1")
	entries4, _, err := buildLPMEntries(
		DecisionSets{Direct: direct},
		[]netip.Prefix{netip.MustParsePrefix("8.8.8.8/32")},
		nil,
		nil,
		true,
		4,
	)
	require.NoError(t, err)
	require.Equal(t, decisionDirect, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("8.8.8.8").As4()}])
	require.Equal(t, decisionDirect, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("1.1.1.1").As4()}])
}

func TestBuildLPMEntriesIgnoresStaticProxyWithoutSteering(t *testing.T) {
	direct := mustIPSet(t, "203.0.113.1", "198.51.100.1")
	entries4, entries6, err := buildLPMEntries(
		DecisionSets{Direct: direct},
		nil,
		[]netip.Prefix{netip.MustParsePrefix("1.1.1.0/24"), netip.MustParsePrefix("2001:db8::/32")},
		nil,
		false,
		2,
	)
	require.NoError(t, err)
	require.Equal(t, decisionDirect, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("203.0.113.1").As4()}])
	require.Equal(t, decisionDirect, entries4[lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("198.51.100.1").As4()}])
	require.Equal(t, 2, len(entries4))
	require.Empty(t, entries6)
	_, hasDefault := entries4[lpmKey4{PrefixLen: 0, Address: [4]byte{}}]
	require.False(t, hasDefault)
}

func TestBuildLPMEntriesRejectsPrefixOverflow(t *testing.T) {
	_, _, err := buildLPMEntries(
		DecisionSets{},
		[]netip.Prefix{
			netip.MustParsePrefix("8.8.8.8/32"),
			netip.MustParsePrefix("9.9.9.9/32"),
			netip.MustParsePrefix("2001:4860:4860::8888/128"),
		},
		nil,
		nil,
		true,
		4,
	)
	require.ErrorContains(t, err, "exceed 4 prefixes")
}

func TestClassifierInstructionsResolveReferences(t *testing.T) {
	instructions := classifierInstructions(1, 2, 3, 4, 5, DefaultEBPFMark, DefaultEBPFProxyMark, 0)
	var output bytes.Buffer
	err := instructions.Marshal(&output, binary.LittleEndian)
	require.NoError(t, err)
	require.NotEmpty(t, output.Bytes())
}

type failingDecisionMap struct {
	keys      map[lpmKey4]uint32
	puts      int
	failPutAt int
}

func (m *failingDecisionMap) Delete(key any) error {
	delete(m.keys, key.(lpmKey4))
	return nil
}

func (m *failingDecisionMap) Put(key, value any) error {
	m.puts++
	if m.puts == m.failPutAt {
		return errors.New("injected map write failure")
	}
	m.keys[key.(lpmKey4)] = value.(uint32)
	return nil
}

func TestReplaceDecisionMapTracksPartialWritesForSafeRetry(t *testing.T) {
	first := lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("8.8.8.8").As4()}
	second := lpmKey4{PrefixLen: 32, Address: netip.MustParseAddr("1.1.1.1").As4()}
	current := make(map[lpmKey4]uint32)
	next := map[lpmKey4]uint32{first: decisionDirect, second: decisionProxy}
	kernelMap := &failingDecisionMap{keys: make(map[lpmKey4]uint32), failPutAt: 2}

	require.ErrorContains(t, replaceDecisionMap[lpmKey4](kernelMap, current, next), "injected map write failure")
	require.Len(t, current, 1, "successful partial Put must remain tracked")
	require.Equal(t, current, kernelMap.keys)

	require.NoError(t, replaceDecisionMap[lpmKey4](kernelMap, current, map[lpmKey4]uint32{}))
	require.Empty(t, current)
	require.Empty(t, kernelMap.keys)
}
