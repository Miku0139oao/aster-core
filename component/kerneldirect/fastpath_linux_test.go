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
