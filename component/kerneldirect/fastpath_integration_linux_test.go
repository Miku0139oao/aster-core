//go:build linux

package kerneldirect

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/metacubex/nftables"
	"github.com/metacubex/nftables/binaryutil"
	"github.com/metacubex/nftables/expr"
	"github.com/metacubex/nftables/userdata"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	"go4.org/netipx"
)

func TestTCFastPathIntegration(t *testing.T) {
	if os.Getenv("ASTER_EBPF_INTEGRATION") != "1" {
		t.Skip("set ASTER_EBPF_INTEGRATION=1 in a privileged Linux network namespace")
	}

	bridgeName := fmt.Sprintf("akdb%d", os.Getpid()%100000)
	slaveName := fmt.Sprintf("akds%d", os.Getpid()%100000)
	bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: bridgeName}}
	require.NoError(t, netlink.LinkAdd(bridge))
	t.Cleanup(func() { _ = netlink.LinkDel(bridge) })
	require.NoError(t, netlink.LinkSetUp(bridge))
	dummy := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: slaveName}}
	require.NoError(t, netlink.LinkAdd(dummy))
	t.Cleanup(func() { _ = netlink.LinkDel(dummy) })
	require.NoError(t, netlink.LinkSetUp(dummy))
	require.NoError(t, netlink.LinkSetMaster(dummy, bridge))

	tableName := fmt.Sprintf("akd_test_%d", os.Getpid())
	nft, err := nftables.New()
	require.NoError(t, err)
	table := nft.AddTable(&nftables.Table{Name: tableName, Family: nftables.TableFamilyINet})
	prerouting := nft.AddChain(&nftables.Chain{Name: "prerouting", Table: table})
	preroutingUDP := nft.AddChain(&nftables.Chain{Name: "prerouting_udp_icmp", Table: table})
	nft.AddRule(&nftables.Rule{Table: table, Chain: prerouting, Exprs: []expr.Any{&expr.Counter{}}})
	nft.AddRule(&nftables.Rule{Table: table, Chain: preroutingUDP, Exprs: []expr.Any{&expr.Counter{}}})
	nft.AddRule(&nftables.Rule{Table: table, Chain: prerouting, Exprs: []expr.Any{
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{byte(ipProtocolTCP)}},
		&expr.Counter{},
		&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(39331)},
		&expr.Redir{RegisterProtoMin: 1},
		&expr.Verdict{Kind: expr.VerdictReturn},
	}})
	nft.AddRule(&nftables.Rule{Table: table, Chain: prerouting, Exprs: []expr.Any{&expr.Counter{}, &expr.Verdict{Kind: expr.VerdictReturn}}})
	// Simulate mark rules left by an unclean prior exit. NewFastPath must
	// replace, rather than duplicate, them.
	nft.AddRule(&nftables.Rule{Table: table, Chain: prerouting, Exprs: []expr.Any{&expr.Counter{}}, UserData: userdata.AppendString(nil, userdata.TypeComment, nftDirectRuleComment)})
	nft.AddRule(&nftables.Rule{Table: table, Chain: preroutingUDP, Exprs: []expr.Any{&expr.Counter{}}, UserData: userdata.AppendString(nil, userdata.TypeComment, nftDirectRuleComment)})
	require.NoError(t, nft.Flush())
	require.NoError(t, nft.CloseLasting())
	t.Cleanup(func() {
		cleanup, cleanupErr := nftables.New()
		if cleanupErr == nil {
			cleanup.DelTable(&nftables.Table{Name: tableName, Family: nftables.TableFamilyINet})
			_ = cleanup.Flush()
			_ = cleanup.CloseLasting()
		}
	})

	path, err := NewFastPath(FastPathOptions{
		Interfaces:    []string{bridgeName},
		TableName:     tableName,
		MaxEntries:    256,
		FlowEntries:   32,
		ProxySteering: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, path.Close()) })
	require.Equal(t, []string{bridgeName}, path.Status().RequestedInterfaces)
	require.Equal(t, []string{slaveName}, path.Status().Interfaces, "bridge ingress must resolve to the port that preserves skb marks")

	inspect, err := nftables.New()
	require.NoError(t, err)
	inspectTable, err := inspect.ListTableOfFamily(tableName, nftables.TableFamilyINet)
	require.NoError(t, err)
	for _, chainName := range []string{"prerouting", "prerouting_udp_icmp"} {
		chain, chainErr := inspect.ListChain(inspectTable, chainName)
		require.NoError(t, chainErr)
		rules, rulesErr := inspect.GetRules(inspectTable, chain)
		require.NoError(t, rulesErr)
		if chainName == "prerouting" {
			require.Len(t, rules, 6)
			redirectIndex := -1
			proxyTCPIndex := -1
			proxyRouteIndex := -1
			for index, rule := range rules {
				if comment, found := userdata.GetString(rule.UserData, userdata.TypeComment); found {
					switch comment {
					case nftProxyTCPRuleComment:
						proxyTCPIndex = index
					case nftProxyRouteRuleComment:
						proxyRouteIndex = index
					}
				}
				for _, expression := range rule.Exprs {
					if _, ok := expression.(*expr.Redir); ok && proxyTCPIndex != index {
						redirectIndex = index
					}
				}
			}
			require.Positive(t, redirectIndex)
			require.Positive(t, proxyTCPIndex)
			require.Positive(t, proxyRouteIndex)
			require.Less(t, proxyTCPIndex, redirectIndex, "TCP PROXY shim must precede the original redirect")
			require.Less(t, proxyRouteIndex, redirectIndex, "UDP/ICMP PROXY shim must precede the original redirect")
		} else {
			require.Len(t, rules, 2)
		}
		require.True(t, bytes.Contains(rules[0].UserData, []byte(nftDirectRuleComment)), "mark return rule was not inserted first")
	}
	require.NoError(t, inspect.CloseLasting())

	var builder netipx.IPSetBuilder
	builder.Add(netip.MustParseAddr("8.8.8.8"))
	builder.Add(netip.MustParseAddr("2001:4860:4860::8888"))
	set, err := builder.IPSet()
	require.NoError(t, err)
	require.NoError(t, path.Replace(DecisionSets{Direct: set}))

	fastPath := path.(*tcFastPath)
	frame := makeIPv4EthernetFrame(netip.MustParseAddr("8.8.8.8"))
	contextIn := make([]byte, 192)
	contextOut := make([]byte, 192)
	initialMark := uint32(0x1234)
	copy(contextIn[skbMarkOffset:skbMarkOffset+4], binaryutil.NativeEndian.PutUint32(initialMark))
	returnValue, err := fastPath.program.Run(&ebpf.RunOptions{
		Data:       frame,
		Context:    contextIn,
		ContextOut: contextOut,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(tcActOK), returnValue)
	require.Equal(t, initialMark|DefaultEBPFMark, binaryutil.NativeEndian.Uint32(contextOut[skbMarkOffset:skbMarkOffset+4]))
	status := path.Status()
	require.Equal(t, 2, status.DirectPrefixes)
	require.Equal(t, 2, status.ProxyPrefixes)
	require.Positive(t, status.BypassPrefixes)
	require.Equal(t, status.DirectPrefixes+status.ProxyPrefixes+status.BypassPrefixes, status.IPv4+status.IPv6)
	require.Equal(t, uint32(32), status.FlowMaxEntries)
	require.Equal(t, uint64(1), status.Packets)
	require.Equal(t, uint64(len(frame)), status.Bytes)

	ipv6Frame := makeIPv6EthernetFrame(netip.MustParseAddr("2001:4860:4860::8888"))
	requireProgramMarks(t, fastPath, ipv6Frame, initialMark)
	vlanFrame := makeVLANIPv4EthernetFrame(netip.MustParseAddr("8.8.8.8"))
	requireProgramMarks(t, fastPath, vlanFrame, initialMark)
	status = path.Status()
	require.Equal(t, uint64(3), status.Packets)
	require.Equal(t, uint64(len(frame)+len(ipv6Frame)+len(vlanFrame)), status.Bytes)
	require.NotZero(t, status.FlowHits, "repeated VLAN/non-VLAN 5-tuple should hit the LRU cache")

	// DNS must continue into sing-tun's hijack rules even when the resolver
	// address is present in the DIRECT map. Fragments and IPv6 extension
	// headers also use the conservative nftables fallback.
	requireProgramDoesNotMark(t, fastPath, makeIPv4EthernetFrameWithPort(netip.MustParseAddr("8.8.8.8"), 53), initialMark)
	requireProgramDoesNotMark(t, fastPath, makeIPv6EthernetFrameWithPort(netip.MustParseAddr("2001:4860:4860::8888"), 53), initialMark)
	fragment := makeIPv4EthernetFrame(netip.MustParseAddr("8.8.8.8"))
	fragment[20] = 0x20 // IPv4 MF flag.
	requireProgramDoesNotMark(t, fastPath, fragment, initialMark)
	extension := makeIPv6EthernetFrame(netip.MustParseAddr("2001:4860:4860::8888"))
	extension[20] = 44 // IPv6 fragment header.
	requireProgramDoesNotMark(t, fastPath, extension, initialMark)
	requireProgramDoesNotMark(t, fastPath, makeIPv4EthernetFrame(netip.MustParseAddr("192.168.1.1")), initialMark)
	requireProgramDoesNotMark(t, fastPath, makeIPv4EthernetFrame(netip.MustParseAddr("198.18.0.2")), initialMark)
	requireProgramDoesNotMark(t, fastPath, makeIPv6EthernetFrame(netip.MustParseAddr("fdfe:dcba:9876::2")), initialMark)
	require.Equal(t, uint64(3), path.Status().DirectPackets)

	// Safe unknown global destinations match the PROXY /0 fallback and use
	// the short nft shim; local/fake-IP traffic above remains untouched.
	requireProgramMarksWith(t, fastPath, makeIPv4EthernetFrame(netip.MustParseAddr("1.1.1.1")), initialMark, DefaultEBPFProxyMark)
	require.Equal(t, uint64(1), path.Status().ProxyPackets)

	// Reclassifying an existing 5-tuple must ignore its stale LRU entry. The
	// odd/even generation is the atomic invalidation boundary.
	var proxyOnlyBuilder netipx.IPSetBuilder
	proxyOnlyBuilder.Add(netip.MustParseAddr("8.8.8.8"))
	proxyOnly, err := proxyOnlyBuilder.IPSet()
	require.NoError(t, err)
	require.NoError(t, path.Replace(DecisionSets{Proxy: proxyOnly}))
	requireProgramMarksWith(t, fastPath, frame, initialMark, DefaultEBPFProxyMark)
	flowHitsBefore := path.Status().FlowHits
	requireProgramMarksWith(t, fastPath, frame, initialMark, DefaultEBPFProxyMark)
	require.Greater(t, path.Status().FlowHits, flowHitsBefore)

	// Exercise the complete DNS controller -> proxy-wins -> BPF map path.
	controller := Register(func(host string, _ netip.Addr) bool {
		return host == "direct.example"
	}, func(sets DecisionSets) {
		require.NoError(t, path.Replace(sets))
	})
	directAddress := netip.MustParseAddr("9.9.9.9")
	ObserveDNS("direct.example", []DNSAnswer{{Addr: directAddress, TTL: time.Minute}})
	require.Equal(t, 1, path.Status().DirectPrefixes)
	ObserveDNS("proxy.example", []DNSAnswer{{Addr: directAddress, TTL: time.Minute}})
	require.Zero(t, path.Status().DirectPrefixes, "proxy-wins must remove the DIRECT decision")
	requireProgramMarksWith(t, fastPath, makeIPv4EthernetFrame(directAddress), initialMark, DefaultEBPFProxyMark)
	ObserveDNS("direct.example", []DNSAnswer{{Addr: directAddress, TTL: time.Minute}})
	Flush()
	require.Zero(t, path.Status().DirectPrefixes, "reload flush must clear learned DIRECT prefixes")
	require.Equal(t, 2, path.Status().ProxyPrefixes, "only the IPv4/IPv6 PROXY fallbacks should remain")
	require.NoError(t, controller.Close())

	filters, err := netlink.FilterList(dummy, netlink.HANDLE_MIN_INGRESS)
	require.NoError(t, err)
	require.Condition(t, func() bool {
		for _, filter := range filters {
			if bpfFilter, ok := filter.(*netlink.BpfFilter); ok && bpfFilter.Name == tcFilterName {
				return true
			}
		}
		return false
	}, "TC filter was not attached")

	var overflowBuilder netipx.IPSetBuilder
	for index := 0; index < 255; index++ {
		overflowBuilder.Add(netip.AddrFrom4([4]byte{11, byte(index), 0, 1}))
	}
	overflowSet, err := overflowBuilder.IPSet()
	require.NoError(t, err)
	require.ErrorContains(t, path.Replace(DecisionSets{Direct: overflowSet}), "exceed 256 prefixes")
	filters, err = netlink.FilterList(dummy, netlink.HANDLE_MIN_INGRESS)
	require.NoError(t, err)
	for _, filter := range filters {
		if bpfFilter, ok := filter.(*netlink.BpfFilter); ok {
			require.NotEqual(t, tcFilterName, bpfFilter.Name, "map failure must detach TC before returning")
		}
	}

	require.NoError(t, path.Close())
	filters, err = netlink.FilterList(dummy, netlink.HANDLE_MIN_INGRESS)
	require.NoError(t, err)
	for _, filter := range filters {
		if bpfFilter, ok := filter.(*netlink.BpfFilter); ok {
			require.NotEqual(t, tcFilterName, bpfFilter.Name, "TC filter leaked after Close")
		}
	}

	inspect, err = nftables.New()
	require.NoError(t, err)
	inspectTable, err = inspect.ListTableOfFamily(tableName, nftables.TableFamilyINet)
	require.NoError(t, err)
	for _, chainName := range []string{"prerouting", "prerouting_udp_icmp"} {
		chain, chainErr := inspect.ListChain(inspectTable, chainName)
		require.NoError(t, chainErr)
		rules, rulesErr := inspect.GetRules(inspectTable, chain)
		require.NoError(t, rulesErr)
		if chainName == "prerouting" {
			require.Len(t, rules, 3, "managed nft decision rule leaked after Close")
		} else {
			require.Len(t, rules, 1, "managed nft decision rule leaked after Close")
		}
	}
	require.NoError(t, inspect.CloseLasting())
}

func TestTCFastPathRedirectIntegration(t *testing.T) {
	if os.Getenv("ASTER_EBPF_INTEGRATION") != "1" {
		t.Skip("set ASTER_EBPF_INTEGRATION=1 in a privileged Linux network namespace")
	}

	ingressName := fmt.Sprintf("akri%d", os.Getpid()%100000)
	redirectName := fmt.Sprintf("akrr%d", os.Getpid()%100000)
	for _, name := range []string{ingressName, redirectName} {
		link := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}
		require.NoError(t, netlink.LinkAdd(link))
		t.Cleanup(func() { _ = netlink.LinkDel(link) })
		require.NoError(t, netlink.LinkSetUp(link))
	}

	tableName := fmt.Sprintf("akd_redirect_%d", os.Getpid())
	nft, err := nftables.New()
	require.NoError(t, err)
	table := nft.AddTable(&nftables.Table{Name: tableName, Family: nftables.TableFamilyINet})
	prerouting := nft.AddChain(&nftables.Chain{Name: "prerouting", Table: table})
	preroutingUDP := nft.AddChain(&nftables.Chain{Name: "prerouting_udp_icmp", Table: table})
	nft.AddRule(&nftables.Rule{Table: table, Chain: prerouting, Exprs: []expr.Any{
		&expr.Immediate{Register: 1, Data: binaryutil.BigEndian.PutUint16(39331)},
		&expr.Redir{RegisterProtoMin: 1},
	}})
	nft.AddRule(&nftables.Rule{Table: table, Chain: preroutingUDP, Exprs: []expr.Any{&expr.Counter{}}})
	require.NoError(t, nft.Flush())
	require.NoError(t, nft.CloseLasting())
	t.Cleanup(func() {
		cleanup, cleanupErr := nftables.New()
		if cleanupErr == nil {
			cleanup.DelTable(&nftables.Table{Name: tableName, Family: nftables.TableFamilyINet})
			_ = cleanup.Flush()
			_ = cleanup.CloseLasting()
		}
	})

	path, err := NewFastPath(FastPathOptions{
		Interfaces:             []string{ingressName},
		ProxyRedirectInterface: redirectName,
		TableName:              tableName,
		MaxEntries:             256,
		FlowEntries:            32,
		ProxySteering:          true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, path.Close()) })
	status := path.Status()
	require.Equal(t, "ebpf-tc-lpm-lru-redirect", status.Backend)
	require.Equal(t, redirectName, status.ProxyRedirectInterface)

	inspect, err := nftables.New()
	require.NoError(t, err)
	inspectTable, err := inspect.ListTableOfFamily(tableName, nftables.TableFamilyINet)
	require.NoError(t, err)
	for _, chainName := range []string{"prerouting", "prerouting_udp_icmp"} {
		chain, chainErr := inspect.ListChain(inspectTable, chainName)
		require.NoError(t, chainErr)
		rules, rulesErr := inspect.GetRules(inspectTable, chain)
		require.NoError(t, rulesErr)
		directRules := 0
		proxyRules := 0
		for _, rule := range rules {
			comment, found := userdata.GetString(rule.UserData, userdata.TypeComment)
			if !found {
				continue
			}
			switch comment {
			case nftDirectRuleComment:
				directRules++
			case nftProxyTCPRuleComment, nftProxyRouteRuleComment:
				proxyRules++
			}
		}
		require.Equal(t, 1, directRules, "each auto-redirect chain needs one DIRECT return rule")
		require.Zero(t, proxyRules, "TC-to-TUN mode must not install PROXY nftables shims")
	}
	require.NoError(t, inspect.CloseLasting())
}

func requireProgramMarks(t *testing.T, fastPath *tcFastPath, frame []byte, initialMark uint32) {
	requireProgramMarksWith(t, fastPath, frame, initialMark, DefaultEBPFMark)
}

func requireProgramMarksWith(t *testing.T, fastPath *tcFastPath, frame []byte, initialMark, expectedMark uint32) {
	t.Helper()
	contextIn := make([]byte, 192)
	contextOut := make([]byte, 192)
	copy(contextIn[skbMarkOffset:skbMarkOffset+4], binaryutil.NativeEndian.PutUint32(initialMark))
	returnValue, err := fastPath.program.Run(&ebpf.RunOptions{Data: frame, Context: contextIn, ContextOut: contextOut})
	require.NoError(t, err)
	require.Equal(t, uint32(tcActOK), returnValue)
	require.Equal(t, initialMark|expectedMark, binaryutil.NativeEndian.Uint32(contextOut[skbMarkOffset:skbMarkOffset+4]))
}

func requireProgramDoesNotMark(t *testing.T, fastPath *tcFastPath, frame []byte, initialMark uint32) {
	t.Helper()
	contextIn := make([]byte, 192)
	contextOut := make([]byte, 192)
	copy(contextIn[skbMarkOffset:skbMarkOffset+4], binaryutil.NativeEndian.PutUint32(initialMark))
	returnValue, err := fastPath.program.Run(&ebpf.RunOptions{Data: frame, Context: contextIn, ContextOut: contextOut})
	require.NoError(t, err)
	require.Equal(t, uint32(tcActOK), returnValue)
	require.Equal(t, initialMark, binaryutil.NativeEndian.Uint32(contextOut[skbMarkOffset:skbMarkOffset+4]))
}

func makeIPv4EthernetFrame(destination netip.Addr) []byte {
	return makeIPv4EthernetFrameWithPort(destination, 443)
}

func makeIPv4EthernetFrameWithPort(destination netip.Addr, destinationPort uint16) []byte {
	frame := make([]byte, ethernetHeaderLength+20+4)
	frame[12] = 0x08
	frame[13] = 0x00
	frame[14] = 0x45
	frame[23] = byte(ipProtocolTCP)
	copy(frame[30:34], destination.AsSlice())
	binary.BigEndian.PutUint16(frame[36:38], destinationPort)
	return frame
}

func makeIPv6EthernetFrame(destination netip.Addr) []byte {
	return makeIPv6EthernetFrameWithPort(destination, 443)
}

func makeIPv6EthernetFrameWithPort(destination netip.Addr, destinationPort uint16) []byte {
	frame := make([]byte, ethernetHeaderLength+40+4)
	frame[12] = 0x86
	frame[13] = 0xdd
	frame[14] = 0x60
	frame[20] = byte(ipProtocolTCP)
	copy(frame[38:54], destination.AsSlice())
	binary.BigEndian.PutUint16(frame[56:58], destinationPort)
	return frame
}

func makeVLANIPv4EthernetFrame(destination netip.Addr) []byte {
	frame := make([]byte, ethernetHeaderLength+vlanHeaderLength+20+4)
	frame[12] = 0x81
	frame[13] = 0x00
	frame[16] = 0x08
	frame[17] = 0x00
	frame[18] = 0x45
	frame[27] = byte(ipProtocolTCP)
	copy(frame[34:38], destination.AsSlice())
	binary.BigEndian.PutUint16(frame[40:42], 443)
	return frame
}
