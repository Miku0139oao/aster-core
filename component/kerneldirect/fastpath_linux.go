//go:build linux

package kerneldirect

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/rlimit"
	"github.com/metacubex/nftables"
	"github.com/metacubex/nftables/binaryutil"
	"github.com/metacubex/nftables/expr"
	"github.com/metacubex/nftables/userdata"
	"github.com/vishvananda/netlink"
	"go4.org/netipx"
	"golang.org/x/sys/unix"
)

const (
	tcFilterName             = "aster-kernel-direct"
	nftDirectRuleComment     = "aster kernel-direct eBPF mark"
	nftProxyTCPRuleComment   = "aster kernel-direct eBPF proxy tcp"
	nftProxyRouteRuleComment = "aster kernel-direct eBPF proxy route"
	tcActOK                  = int32(0)
	defaultInputMark         = uint32(0x2023)
	decisionDirect           = uint32(1)
	decisionProxy            = uint32(2)
	decisionBypass           = uint32(3)
	statDirect               = int32(0)
	statProxy                = int32(1)
	statFlowHit              = int32(2)
	statEntries              = uint32(3)
	generationKeyStackOffset = int16(-76)
	flowValueStackOffset     = int16(-72)
	flowKeyStackOffset       = int16(-64)
	lpmKeyStackOffset        = int16(-24)

	skbMarkOffset    = int16(8)
	skbLengthOffset  = int16(0)
	skbDataOffset    = int16(76)
	skbDataEndOffset = int16(80)

	ethernetHeaderLength = int32(14)
	vlanHeaderLength     = int32(4)
	etherTypeOffset      = int16(12)
	vlanEtherTypeOffset  = int16(16)
	etherTypeIPv4        = int32(0x0800)
	etherTypeIPv6        = int32(0x86dd)
	etherTypeVLAN        = int32(0x8100)
	etherTypeQinQ        = int32(0x88a8)
	ipProtocolICMP       = int32(unix.IPPROTO_ICMP)
	ipProtocolTCP        = int32(unix.IPPROTO_TCP)
	ipProtocolUDP        = int32(unix.IPPROTO_UDP)
	ipProtocolICMPv6     = int32(unix.IPPROTO_ICMPV6)
	dnsPort              = int32(53)
)

type lpmKey4 struct {
	PrefixLen uint32
	Address   [4]byte
}

type lpmKey6 struct {
	PrefixLen uint32
	Address   [16]byte
}

type flowKey struct {
	Family          uint8
	Protocol        uint8
	Padding         uint16
	Source          [16]byte
	Destination     [16]byte
	SourcePort      uint16
	DestinationPort uint16
}

type flowValue struct {
	Decision   uint32
	Generation uint32
}

type bpfCounter struct {
	Packets uint64
	Bytes   uint64
}

type attachedFilter struct {
	link   netlink.Link
	filter netlink.Filter
}

type tcFastPath struct {
	mu sync.Mutex

	mark                   uint32
	proxyMark              uint32
	inputMark              uint32
	maxEntries             uint32
	flowEntries            uint32
	proxySteering          bool
	proxyRedirectInterface string
	proxyRedirectIfIndex   int
	tableName              string
	requested              []string
	interfaces             []string
	staticDirect           []netip.Prefix
	staticProxy            []netip.Prefix
	staticBypass           []netip.Prefix
	ipv4                   *ebpf.Map
	ipv6                   *ebpf.Map
	flows                  *ebpf.Map
	generation             *ebpf.Map
	tcStats                *ebpf.Map
	program                *ebpf.Program
	filters                []attachedFilter
	entries4               map[lpmKey4]uint32
	entries6               map[lpmKey6]uint32
	generationValue        uint32
	updatedAt              time.Time
	lastError              string
	closed                 bool
}

func NewFastPath(options FastPathOptions) (FastPath, error) {
	if len(options.Interfaces) == 0 {
		return nil, errors.New("TC eBPF kernel-direct requires at least one interface")
	}
	if options.Mark == 0 {
		options.Mark = DefaultEBPFMark
	}
	if options.ProxyMark == 0 {
		options.ProxyMark = DefaultEBPFProxyMark
	}
	if options.InputMark == 0 {
		options.InputMark = defaultInputMark
	}
	if options.MaxEntries == 0 {
		options.MaxEntries = DefaultEBPFMaxEntries
	}
	if options.FlowEntries == 0 {
		options.FlowEntries = DefaultEBPFFlowEntries
	}
	if options.TableName == "" {
		options.TableName = "mihomo"
	}
	if options.Mark&options.ProxyMark != 0 {
		return nil, errors.New("TC eBPF DIRECT and PROXY marks overlap")
	}
	if options.ProxySteering && (options.ProxyMark&options.InputMark != 0 || options.Mark&options.InputMark != 0) {
		return nil, errors.New("TC eBPF decision marks overlap the auto-redirect input mark")
	}

	requested := append([]string(nil), options.Interfaces...)
	sort.Strings(requested)
	requested = compactStrings(requested)
	interfaces, err := resolveTCInterfaces(requested)
	if err != nil {
		return nil, err
	}
	proxyRedirectInterface := options.ProxyRedirectInterface
	proxyRedirectIfIndex := 0
	if proxyRedirectInterface != "" {
		redirectLink, redirectErr := netlink.LinkByName(proxyRedirectInterface)
		if redirectErr != nil {
			return nil, fmt.Errorf("find TC PROXY redirect interface %s: %w", proxyRedirectInterface, redirectErr)
		}
		if redirectLink.Attrs() == nil || redirectLink.Attrs().Index <= 0 {
			return nil, fmt.Errorf("TC PROXY redirect interface %s has no ifindex", proxyRedirectInterface)
		}
		proxyRedirectIfIndex = redirectLink.Attrs().Index
	}
	staticBypass, err := localHostPrefixes()
	if err != nil {
		return nil, err
	}

	// Kernels since 5.11 account BPF memory with memcg, while older OpenWrt
	// builds still need RLIMIT_MEMLOCK raised.
	_ = rlimit.RemoveMemlock()

	f := &tcFastPath{
		mark:                   options.Mark,
		proxyMark:              options.ProxyMark,
		inputMark:              options.InputMark,
		maxEntries:             options.MaxEntries,
		flowEntries:            options.FlowEntries,
		proxySteering:          options.ProxySteering,
		proxyRedirectInterface: proxyRedirectInterface,
		proxyRedirectIfIndex:   proxyRedirectIfIndex,
		tableName:              options.TableName,
		requested:              requested,
		interfaces:             interfaces,
		staticDirect:           normalizePrefixes(options.DirectPrefixes),
		staticProxy:            normalizePrefixes(options.ProxyPrefixes),
		staticBypass:           staticBypass,
		entries4:               make(map[lpmKey4]uint32),
		entries6:               make(map[lpmKey6]uint32),
	}
	if err := f.open(); err != nil {
		_ = f.Close()
		return nil, err
	}
	registerFastPath(f)
	return f, nil
}

func (f *tcFastPath) open() error {
	var err error
	f.ipv4, err = ebpf.NewMap(&ebpf.MapSpec{
		Name:       "aster_kd_v4",
		Type:       ebpf.LPMTrie,
		KeySize:    uint32(8),
		ValueSize:  uint32(4),
		MaxEntries: f.maxEntries,
		Flags:      unix.BPF_F_NO_PREALLOC,
	})
	if err != nil {
		return fmt.Errorf("create IPv4 decision LPM map: %w", err)
	}
	f.ipv6, err = ebpf.NewMap(&ebpf.MapSpec{
		Name:       "aster_kd_v6",
		Type:       ebpf.LPMTrie,
		KeySize:    uint32(20),
		ValueSize:  uint32(4),
		MaxEntries: f.maxEntries,
		Flags:      unix.BPF_F_NO_PREALLOC,
	})
	if err != nil {
		return fmt.Errorf("create IPv6 decision LPM map: %w", err)
	}
	f.flows, err = ebpf.NewMap(&ebpf.MapSpec{
		Name:       "aster_kd_flow",
		Type:       ebpf.LRUHash,
		KeySize:    uint32(40),
		ValueSize:  uint32(8),
		MaxEntries: f.flowEntries,
	})
	if err != nil {
		return fmt.Errorf("create 5-tuple LRU flow map: %w", err)
	}
	f.generation, err = ebpf.NewMap(&ebpf.MapSpec{
		Name:       "aster_kd_gen",
		Type:       ebpf.Array,
		KeySize:    uint32(4),
		ValueSize:  uint32(4),
		MaxEntries: 1,
	})
	if err != nil {
		return fmt.Errorf("create decision generation map: %w", err)
	}
	f.tcStats, err = ebpf.NewMap(&ebpf.MapSpec{
		Name:       "aster_kd_stat",
		Type:       ebpf.PerCPUArray,
		KeySize:    uint32(4),
		ValueSize:  uint32(16),
		MaxEntries: statEntries,
	})
	if err != nil {
		return fmt.Errorf("create decision counter map: %w", err)
	}

	// Populate static prefixes and the optional safe PROXY /0 fallback before
	// attaching TC. No packet can observe a partially populated generation.
	if err := f.replaceLocked(DecisionSets{}); err != nil {
		return err
	}

	f.program, err = ebpf.NewProgram(&ebpf.ProgramSpec{
		Name: "aster_kd_tc",
		Type: ebpf.SchedCLS,
		Instructions: classifierInstructions(
			f.ipv4.FD(),
			f.ipv6.FD(),
			f.flows.FD(),
			f.generation.FD(),
			f.tcStats.FD(),
			f.mark,
			f.proxyMark,
			f.proxyRedirectIfIndex,
		),
		License: "GPL",
	})
	if err != nil {
		return fmt.Errorf("load TC LPM/LRU classifier: %w", err)
	}

	cleanupInterfaces := append(append([]string(nil), f.requested...), f.interfaces...)
	sort.Strings(cleanupInterfaces)
	cleanupInterfaces = compactStrings(cleanupInterfaces)
	for _, interfaceName := range cleanupInterfaces {
		link, linkErr := netlink.LinkByName(interfaceName)
		if linkErr != nil {
			return fmt.Errorf("find TC interface %s: %w", interfaceName, linkErr)
		}
		if err := removeStaleTCFilters(link, interfaceName); err != nil {
			return err
		}
	}
	for _, interfaceName := range f.interfaces {
		if err := f.attach(interfaceName); err != nil {
			return err
		}
	}
	if err := installMarkRules(f.tableName, f.mark, f.proxyMark, f.inputMark, f.proxySteering && f.proxyRedirectIfIndex == 0); err != nil {
		return fmt.Errorf("install nftables eBPF decision rules: %w", err)
	}
	f.updatedAt = time.Now()
	return nil
}

func classifierInstructions(ipv4FD, ipv6FD, flowFD, generationFD, statsFD int, directMark, proxyMark uint32, proxyRedirectIfIndex int) asm.Instructions {
	clearDecisionMask := int32(^(directMark | proxyMark))
	return asm.Instructions{
		// R6 keeps the skb context. An odd generation means userspace is
		// replacing maps, so every packet safely falls through to nftables.
		asm.Mov.Reg(asm.R6, asm.R1),
		asm.StoreImm(asm.RFP, generationKeyStackOffset, 0, asm.Word),
		asm.LoadMapPtr(asm.R1, generationFD),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, int32(generationKeyStackOffset)),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "pass"),
		asm.LoadMem(asm.R8, asm.R0, 0, asm.Word),
		asm.Mov.Reg(asm.R0, asm.R8),
		asm.And.Imm(asm.R0, 1),
		asm.JNE.Imm(asm.R0, 0, "pass"),

		asm.LoadMem(asm.R2, asm.R6, skbDataOffset, asm.Word),
		asm.LoadMem(asm.R3, asm.R6, skbDataEndOffset, asm.Word),
		asm.Mov.Reg(asm.R4, asm.R2),
		asm.Add.Imm(asm.R4, ethernetHeaderLength),
		asm.JGT.Reg(asm.R4, asm.R3, "pass"),
		asm.LoadMem(asm.R5, asm.R2, etherTypeOffset, asm.Half),
		asm.HostTo(asm.BE, asm.R5, asm.Half),
		asm.JEq.Imm(asm.R5, etherTypeIPv4, "ipv4"),
		asm.JEq.Imm(asm.R5, etherTypeIPv6, "ipv6"),
		asm.JEq.Imm(asm.R5, etherTypeVLAN, "vlan"),
		asm.JEq.Imm(asm.R5, etherTypeQinQ, "vlan"),
		asm.Ja.Label("pass"),

		asm.Mov.Reg(asm.R4, asm.R2).WithSymbol("vlan"),
		asm.Add.Imm(asm.R4, ethernetHeaderLength+vlanHeaderLength),
		asm.JGT.Reg(asm.R4, asm.R3, "pass"),
		asm.LoadMem(asm.R5, asm.R2, vlanEtherTypeOffset, asm.Half),
		asm.HostTo(asm.BE, asm.R5, asm.Half),
		asm.JEq.Imm(asm.R5, etherTypeIPv4, "ipv4_vlan"),
		asm.JEq.Imm(asm.R5, etherTypeIPv6, "ipv6_vlan"),
		asm.Ja.Label("pass"),

		asm.Mov.Reg(asm.R9, asm.R2).WithSymbol("ipv4"),
		asm.Add.Imm(asm.R9, ethernetHeaderLength),
		asm.Ja.Label("ipv4_header"),
		asm.Mov.Reg(asm.R9, asm.R2).WithSymbol("ipv4_vlan"),
		asm.Add.Imm(asm.R9, ethernetHeaderLength+vlanHeaderLength),

		// IPv4 options are accepted after validating IHL; fragments, DNS and
		// destinations that must remain kernel-local all use the nft fallback.
		asm.Mov.Reg(asm.R4, asm.R9).WithSymbol("ipv4_header"),
		asm.Add.Imm(asm.R4, 20),
		asm.JGT.Reg(asm.R4, asm.R3, "pass"),
		asm.LoadMem(asm.R5, asm.R9, 0, asm.Byte),
		asm.Mov.Reg(asm.R4, asm.R5),
		asm.RSh.Imm(asm.R4, 4),
		asm.JNE.Imm(asm.R4, 4, "pass"),
		asm.Mov.Reg(asm.R4, asm.R5),
		asm.And.Imm(asm.R4, 0x0f),
		asm.JLT.Imm(asm.R4, 5, "pass"),
		asm.LSh.Imm(asm.R4, 2),
		asm.Add.Reg(asm.R4, asm.R9),
		asm.LoadMem(asm.R5, asm.R9, 6, asm.Half),
		asm.HostTo(asm.BE, asm.R5, asm.Half),
		asm.And.Imm(asm.R5, 0x3fff),
		asm.JNE.Imm(asm.R5, 0, "pass"),
		asm.LoadMem(asm.R7, asm.R9, 9, asm.Byte),
		asm.JEq.Imm(asm.R7, ipProtocolTCP, "ipv4_destination"),
		asm.JEq.Imm(asm.R7, ipProtocolUDP, "ipv4_destination"),
		asm.JNE.Imm(asm.R7, ipProtocolICMP, "pass"),

		asm.LoadMem(asm.R0, asm.R9, 16, asm.Byte).WithSymbol("ipv4_destination"),
		asm.JEq.Imm(asm.R0, 0, "pass"),
		asm.JEq.Imm(asm.R0, 10, "pass"),
		asm.JEq.Imm(asm.R0, 100, "ipv4_100"),
		asm.JEq.Imm(asm.R0, 127, "pass"),
		asm.JEq.Imm(asm.R0, 169, "ipv4_169"),
		asm.JEq.Imm(asm.R0, 172, "ipv4_172"),
		asm.JEq.Imm(asm.R0, 192, "ipv4_192"),
		asm.JEq.Imm(asm.R0, 198, "ipv4_198"),
		asm.JGT.Imm(asm.R0, 223, "pass"),
		asm.Ja.Label("ipv4_flow_init"),

		asm.LoadMem(asm.R0, asm.R9, 17, asm.Byte).WithSymbol("ipv4_100"),
		asm.JLT.Imm(asm.R0, 64, "ipv4_flow_init"),
		asm.JGT.Imm(asm.R0, 127, "ipv4_flow_init"),
		asm.Ja.Label("pass"),
		asm.LoadMem(asm.R0, asm.R9, 17, asm.Byte).WithSymbol("ipv4_169"),
		asm.JEq.Imm(asm.R0, 254, "pass"),
		asm.Ja.Label("ipv4_flow_init"),
		asm.LoadMem(asm.R0, asm.R9, 17, asm.Byte).WithSymbol("ipv4_172"),
		asm.JLT.Imm(asm.R0, 16, "ipv4_flow_init"),
		asm.JGT.Imm(asm.R0, 31, "ipv4_flow_init"),
		asm.Ja.Label("pass"),
		asm.LoadMem(asm.R0, asm.R9, 17, asm.Byte).WithSymbol("ipv4_192"),
		asm.JEq.Imm(asm.R0, 168, "pass"),
		asm.Ja.Label("ipv4_flow_init"),
		asm.LoadMem(asm.R0, asm.R9, 17, asm.Byte).WithSymbol("ipv4_198"),
		asm.JEq.Imm(asm.R0, 18, "pass"),
		asm.JEq.Imm(asm.R0, 19, "pass"),

		// A fixed 40-byte, fully initialized key stores family, protocol,
		// source/destination addresses and source/destination ports.
		asm.StoreImm(asm.RFP, -64, 0, asm.DWord).WithSymbol("ipv4_flow_init"),
		asm.StoreImm(asm.RFP, -56, 0, asm.DWord),
		asm.StoreImm(asm.RFP, -48, 0, asm.DWord),
		asm.StoreImm(asm.RFP, -40, 0, asm.DWord),
		asm.StoreImm(asm.RFP, -32, 0, asm.DWord),
		asm.StoreImm(asm.RFP, -64, 4, asm.Byte),
		asm.StoreMem(asm.RFP, -63, asm.R7, asm.Byte),
		asm.LoadMem(asm.R0, asm.R9, 12, asm.Word),
		asm.StoreMem(asm.RFP, -60, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 16, asm.Word),
		asm.StoreMem(asm.RFP, -44, asm.R0, asm.Word),
		asm.JEq.Imm(asm.R7, ipProtocolICMP, "flow_lookup_v4"),
		asm.Mov.Reg(asm.R0, asm.R4),
		asm.Add.Imm(asm.R0, 4),
		asm.JGT.Reg(asm.R0, asm.R3, "pass"),
		asm.LoadMem(asm.R5, asm.R4, 0, asm.Half),
		asm.HostTo(asm.BE, asm.R5, asm.Half),
		asm.JEq.Imm(asm.R5, dnsPort, "pass"),
		asm.StoreMem(asm.RFP, -28, asm.R5, asm.Half),
		asm.LoadMem(asm.R5, asm.R4, 2, asm.Half),
		asm.HostTo(asm.BE, asm.R5, asm.Half),
		asm.JEq.Imm(asm.R5, dnsPort, "pass"),
		asm.StoreMem(asm.RFP, -26, asm.R5, asm.Half),

		asm.LoadMapPtr(asm.R1, flowFD).WithSymbol("flow_lookup_v4"),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, int32(flowKeyStackOffset)),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "lpm_lookup_v4"),
		asm.LoadMem(asm.R1, asm.R0, 4, asm.Word),
		asm.JNE.Reg(asm.R1, asm.R8, "lpm_lookup_v4"),
		asm.LoadMem(asm.R7, asm.R0, 0, asm.Word),
		asm.JEq.Imm(asm.R7, int32(decisionDirect), "flow_hit"),
		asm.JEq.Imm(asm.R7, int32(decisionProxy), "flow_hit"),
		asm.JEq.Imm(asm.R7, int32(decisionBypass), "flow_hit"),
		asm.Ja.Label("lpm_lookup_v4"),

		asm.StoreImm(asm.RFP, lpmKeyStackOffset, 32, asm.Word).WithSymbol("lpm_lookup_v4"),
		asm.LoadMem(asm.R0, asm.R9, 16, asm.Word),
		asm.StoreMem(asm.RFP, lpmKeyStackOffset+4, asm.R0, asm.Word),
		asm.LoadMapPtr(asm.R1, ipv4FD),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, int32(lpmKeyStackOffset)),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "pass"),
		asm.LoadMem(asm.R7, asm.R0, 0, asm.Word),
		asm.JEq.Imm(asm.R7, int32(decisionDirect), "cache_decision"),
		asm.JEq.Imm(asm.R7, int32(decisionProxy), "cache_decision"),
		asm.JEq.Imm(asm.R7, int32(decisionBypass), "cache_decision"),
		asm.Ja.Label("pass"),

		asm.Mov.Reg(asm.R9, asm.R2).WithSymbol("ipv6"),
		asm.Add.Imm(asm.R9, ethernetHeaderLength),
		asm.Ja.Label("ipv6_header"),
		asm.Mov.Reg(asm.R9, asm.R2).WithSymbol("ipv6_vlan"),
		asm.Add.Imm(asm.R9, ethernetHeaderLength+vlanHeaderLength),

		asm.Mov.Reg(asm.R4, asm.R9).WithSymbol("ipv6_header"),
		asm.Add.Imm(asm.R4, 40),
		asm.JGT.Reg(asm.R4, asm.R3, "pass"),
		asm.LoadMem(asm.R5, asm.R9, 0, asm.Byte),
		asm.RSh.Imm(asm.R5, 4),
		asm.JNE.Imm(asm.R5, 6, "pass"),
		asm.LoadMem(asm.R7, asm.R9, 6, asm.Byte),
		asm.JEq.Imm(asm.R7, ipProtocolTCP, "ipv6_destination"),
		asm.JEq.Imm(asm.R7, ipProtocolUDP, "ipv6_destination"),
		asm.JNE.Imm(asm.R7, ipProtocolICMPv6, "pass"),

		asm.LoadMem(asm.R0, asm.R9, 24, asm.Byte).WithSymbol("ipv6_destination"),
		asm.JEq.Imm(asm.R0, 0, "pass"),
		asm.JEq.Imm(asm.R0, 0xfc, "pass"),
		asm.JEq.Imm(asm.R0, 0xfd, "pass"),
		asm.JEq.Imm(asm.R0, 0xff, "pass"),
		asm.JEq.Imm(asm.R0, 0xfe, "ipv6_fe"),
		asm.Ja.Label("ipv6_flow_init"),
		asm.LoadMem(asm.R0, asm.R9, 25, asm.Byte).WithSymbol("ipv6_fe"),
		asm.And.Imm(asm.R0, 0xc0),
		asm.JEq.Imm(asm.R0, 0x80, "pass"),
		asm.JEq.Imm(asm.R0, 0xc0, "pass"),

		asm.StoreImm(asm.RFP, -64, 0, asm.DWord).WithSymbol("ipv6_flow_init"),
		asm.StoreImm(asm.RFP, -56, 0, asm.DWord),
		asm.StoreImm(asm.RFP, -48, 0, asm.DWord),
		asm.StoreImm(asm.RFP, -40, 0, asm.DWord),
		asm.StoreImm(asm.RFP, -32, 0, asm.DWord),
		asm.StoreImm(asm.RFP, -64, 6, asm.Byte),
		asm.StoreMem(asm.RFP, -63, asm.R7, asm.Byte),
		asm.LoadMem(asm.R0, asm.R9, 8, asm.Word),
		asm.StoreMem(asm.RFP, -60, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 12, asm.Word),
		asm.StoreMem(asm.RFP, -56, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 16, asm.Word),
		asm.StoreMem(asm.RFP, -52, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 20, asm.Word),
		asm.StoreMem(asm.RFP, -48, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 24, asm.Word),
		asm.StoreMem(asm.RFP, -44, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 28, asm.Word),
		asm.StoreMem(asm.RFP, -40, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 32, asm.Word),
		asm.StoreMem(asm.RFP, -36, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 36, asm.Word),
		asm.StoreMem(asm.RFP, -32, asm.R0, asm.Word),
		asm.JEq.Imm(asm.R7, ipProtocolICMPv6, "flow_lookup_v6"),
		asm.Mov.Reg(asm.R0, asm.R4),
		asm.Add.Imm(asm.R0, 4),
		asm.JGT.Reg(asm.R0, asm.R3, "pass"),
		asm.LoadMem(asm.R5, asm.R4, 0, asm.Half),
		asm.HostTo(asm.BE, asm.R5, asm.Half),
		asm.JEq.Imm(asm.R5, dnsPort, "pass"),
		asm.StoreMem(asm.RFP, -28, asm.R5, asm.Half),
		asm.LoadMem(asm.R5, asm.R4, 2, asm.Half),
		asm.HostTo(asm.BE, asm.R5, asm.Half),
		asm.JEq.Imm(asm.R5, dnsPort, "pass"),
		asm.StoreMem(asm.RFP, -26, asm.R5, asm.Half),

		asm.LoadMapPtr(asm.R1, flowFD).WithSymbol("flow_lookup_v6"),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, int32(flowKeyStackOffset)),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "lpm_lookup_v6"),
		asm.LoadMem(asm.R1, asm.R0, 4, asm.Word),
		asm.JNE.Reg(asm.R1, asm.R8, "lpm_lookup_v6"),
		asm.LoadMem(asm.R7, asm.R0, 0, asm.Word),
		asm.JEq.Imm(asm.R7, int32(decisionDirect), "flow_hit"),
		asm.JEq.Imm(asm.R7, int32(decisionProxy), "flow_hit"),
		asm.JEq.Imm(asm.R7, int32(decisionBypass), "flow_hit"),
		asm.Ja.Label("lpm_lookup_v6"),

		asm.StoreImm(asm.RFP, lpmKeyStackOffset, 128, asm.Word).WithSymbol("lpm_lookup_v6"),
		asm.LoadMem(asm.R0, asm.R9, 24, asm.Word),
		asm.StoreMem(asm.RFP, lpmKeyStackOffset+4, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 28, asm.Word),
		asm.StoreMem(asm.RFP, lpmKeyStackOffset+8, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 32, asm.Word),
		asm.StoreMem(asm.RFP, lpmKeyStackOffset+12, asm.R0, asm.Word),
		asm.LoadMem(asm.R0, asm.R9, 36, asm.Word),
		asm.StoreMem(asm.RFP, lpmKeyStackOffset+16, asm.R0, asm.Word),
		asm.LoadMapPtr(asm.R1, ipv6FD),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, int32(lpmKeyStackOffset)),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "pass"),
		asm.LoadMem(asm.R7, asm.R0, 0, asm.Word),
		asm.JEq.Imm(asm.R7, int32(decisionDirect), "cache_decision"),
		asm.JEq.Imm(asm.R7, int32(decisionProxy), "cache_decision"),
		asm.JEq.Imm(asm.R7, int32(decisionBypass), "cache_decision"),
		asm.Ja.Label("pass"),

		// Cache only decisions from the current even generation. A failed LRU
		// insertion is harmless; the packet still uses the LPM decision.
		asm.StoreMem(asm.RFP, flowValueStackOffset, asm.R7, asm.Word).WithSymbol("cache_decision"),
		asm.StoreMem(asm.RFP, flowValueStackOffset+4, asm.R8, asm.Word),
		asm.LoadMapPtr(asm.R1, flowFD),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, int32(flowKeyStackOffset)),
		asm.Mov.Reg(asm.R3, asm.RFP),
		asm.Add.Imm(asm.R3, int32(flowValueStackOffset)),
		asm.Mov.Imm(asm.R4, 0),
		asm.FnMapUpdateElem.Call(),
		asm.Ja.Label("apply_decision"),

		asm.StoreImm(asm.RFP, generationKeyStackOffset, int64(statFlowHit), asm.Word).WithSymbol("flow_hit"),
		asm.LoadMapPtr(asm.R1, statsFD),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, int32(generationKeyStackOffset)),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "apply_decision"),
		asm.Mov.Imm(asm.R1, 1),
		asm.StoreXAdd(asm.R0, asm.R1, asm.DWord),

		asm.JEq.Imm(asm.R7, int32(decisionDirect), "count_direct").WithSymbol("apply_decision"),
		asm.JEq.Imm(asm.R7, int32(decisionProxy), "count_proxy"),
		asm.Ja.Label("pass"),
		asm.StoreImm(asm.RFP, generationKeyStackOffset, int64(statDirect), asm.Word).WithSymbol("count_direct"),
		asm.Mov.Imm(asm.R9, int32(directMark)),
		asm.Ja.Label("count_decision"),
		asm.StoreImm(asm.RFP, generationKeyStackOffset, int64(statProxy), asm.Word).WithSymbol("count_proxy"),
		asm.Mov.Imm(asm.R9, int32(proxyMark)),

		asm.LoadMapPtr(asm.R1, statsFD).WithSymbol("count_decision"),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, int32(generationKeyStackOffset)),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "apply_action"),
		asm.Mov.Imm(asm.R1, 1),
		asm.StoreXAdd(asm.R0, asm.R1, asm.DWord),
		asm.LoadMem(asm.R1, asm.R6, skbLengthOffset, asm.Word),
		asm.Add.Imm(asm.R0, 8),
		asm.StoreXAdd(asm.R0, asm.R1, asm.DWord),

		// A valid redirect ifindex makes PROXY packets enter the TUN queue
		// directly at TC. The zero value deliberately falls back to the mark
		// shim, which keeps older kernels and configurations compatible.
		asm.JNE.Imm(asm.R7, int32(decisionProxy), "set_mark").WithSymbol("apply_action"),
		asm.Mov.Imm(asm.R1, int32(proxyRedirectIfIndex)),
		asm.JEq.Imm(asm.R1, 0, "set_mark"),
		asm.Mov.Imm(asm.R2, 0),
		asm.FnRedirect.Call(),
		asm.Return(),

		// Decision bits are mutually exclusive; unrelated firewall/QoS mark
		// bits survive unchanged.
		asm.LoadMem(asm.R0, asm.R6, skbMarkOffset, asm.Word).WithSymbol("set_mark"),
		asm.And.Imm(asm.R0, clearDecisionMask),
		asm.Or.Reg(asm.R0, asm.R9),
		asm.StoreMem(asm.R6, skbMarkOffset, asm.R0, asm.Word),

		asm.Mov.Imm(asm.R0, tcActOK).WithSymbol("pass"),
		asm.Return(),
	}
}

func (f *tcFastPath) attach(interfaceName string) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return fmt.Errorf("find TC interface %s: %w", interfaceName, err)
	}
	qdisc := &netlink.GenericQdisc{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(0xffff, 0),
			Parent:    netlink.HANDLE_CLSACT,
		},
		QdiscType: "clsact",
	}
	if err := netlink.QdiscAdd(qdisc); err != nil && !errors.Is(err, unix.EEXIST) {
		return fmt.Errorf("add clsact qdisc on %s: %w", interfaceName, err)
	}

	filter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.HANDLE_MIN_INGRESS,
			Priority:  1,
			Protocol:  unix.ETH_P_ALL,
		},
		Fd:           f.program.FD(),
		Name:         tcFilterName,
		DirectAction: true,
	}
	if err := netlink.FilterAdd(filter); err != nil {
		return fmt.Errorf("attach decision classifier to %s: %w", interfaceName, err)
	}
	f.filters = append(f.filters, attachedFilter{link: link, filter: filter})
	return nil
}

func removeStaleTCFilters(link netlink.Link, interfaceName string) error {
	filters, err := netlink.FilterList(link, netlink.HANDLE_MIN_INGRESS)
	if err != nil {
		return fmt.Errorf("list ingress filters on %s: %w", interfaceName, err)
	}
	for _, filter := range filters {
		if bpfFilter, ok := filter.(*netlink.BpfFilter); ok && bpfFilter.Name == tcFilterName {
			if err := netlink.FilterDel(filter); err != nil {
				return fmt.Errorf("remove stale decision filter on %s: %w", interfaceName, err)
			}
		}
	}
	return nil
}

func resolveTCInterfaces(requested []string) ([]string, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("list TC interfaces: %w", err)
	}
	bridgeMembers := make(map[int][]string)
	for _, link := range links {
		attrs := link.Attrs()
		if attrs == nil || attrs.MasterIndex == 0 {
			continue
		}
		bridgeMembers[attrs.MasterIndex] = append(bridgeMembers[attrs.MasterIndex], attrs.Name)
	}

	resolved := make([]string, 0, len(requested))
	for _, interfaceName := range requested {
		link, linkErr := netlink.LinkByName(interfaceName)
		if linkErr != nil {
			return nil, fmt.Errorf("find TC interface %s: %w", interfaceName, linkErr)
		}
		members := bridgeMembers[link.Attrs().Index]
		if link.Type() == "bridge" && len(members) > 0 {
			resolved = append(resolved, members...)
			continue
		}
		resolved = append(resolved, interfaceName)
	}
	sort.Strings(resolved)
	return compactStrings(resolved), nil
}

func localHostPrefixes() ([]netip.Prefix, error) {
	addresses, err := netlink.AddrList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("list local addresses for TC bypass: %w", err)
	}
	prefixes := make([]netip.Prefix, 0, len(addresses))
	for _, address := range addresses {
		if address.IP == nil {
			continue
		}
		addr, valid := netip.AddrFromSlice(address.IP)
		if !valid {
			continue
		}
		addr = addr.Unmap()
		if !addr.IsValid() || addr.IsUnspecified() {
			continue
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return normalizePrefixes(prefixes), nil
}

func installMarkRules(tableName string, directMark, proxyMark, inputMark uint32, proxySteering bool) error {
	nft, err := nftables.New()
	if err != nil {
		return err
	}
	defer nft.CloseLasting()
	table, err := nft.ListTableOfFamily(tableName, nftables.TableFamilyINet)
	if err != nil {
		return err
	}

	var prerouting *nftables.Chain
	var redirectRule *nftables.Rule
	for _, chainName := range []string{"prerouting", "prerouting_udp_icmp"} {
		chain, chainErr := nft.ListChain(table, chainName)
		if chainErr != nil {
			return chainErr
		}
		rules, rulesErr := nft.GetRules(table, chain)
		if rulesErr != nil {
			return rulesErr
		}
		if err := deleteManagedMarkRules(nft, rules); err != nil {
			return err
		}
		if chainName == "prerouting" {
			prerouting = chain
			redirectRule = findTCPRedirectRule(rules)
		}
		nft.InsertRule(&nftables.Rule{
			Table:    table,
			Chain:    chain,
			Exprs:    markMatchExpressions(directMark, true),
			UserData: userdata.AppendString(nil, userdata.TypeComment, nftDirectRuleComment),
		})
	}

	if proxySteering {
		if prerouting == nil || redirectRule == nil {
			return errors.New("cannot find auto-redirect TCP rule for PROXY steering")
		}
		tcpExpressions := append(markMatchExpressions(proxyMark, false), cloneRuleExpressions(redirectRule.Exprs)...)
		nft.InsertRule(&nftables.Rule{
			Table:    table,
			Chain:    prerouting,
			Position: redirectRule.Handle,
			Exprs:    tcpExpressions,
			UserData: userdata.AppendString(nil, userdata.TypeComment, nftProxyTCPRuleComment),
		})
		routeExpressions := markMatchExpressions(proxyMark, false)
		routeExpressions = append(routeExpressions,
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: []byte{byte(unix.IPPROTO_TCP)}},
			&expr.Immediate{Register: 1, Data: binaryutil.NativeEndian.PutUint32(inputMark)},
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
			&expr.Ct{Key: expr.CtKeyMARK, SourceRegister: true, Register: 1},
			&expr.Counter{},
			&expr.Verdict{Kind: expr.VerdictReturn},
		)
		nft.InsertRule(&nftables.Rule{
			Table:    table,
			Chain:    prerouting,
			Position: redirectRule.Handle,
			Exprs:    routeExpressions,
			UserData: userdata.AppendString(nil, userdata.TypeComment, nftProxyRouteRuleComment),
		})
	}
	return nft.Flush()
}

func markMatchExpressions(mark uint32, withCounterReturn bool) []expr.Any {
	result := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		&expr.Bitwise{
			SourceRegister: 1,
			DestRegister:   1,
			Len:            4,
			Mask:           binaryutil.NativeEndian.PutUint32(mark),
			Xor:            []byte{0, 0, 0, 0},
		},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.NativeEndian.PutUint32(mark)},
	}
	if withCounterReturn {
		result = append(result, &expr.Counter{}, &expr.Verdict{Kind: expr.VerdictReturn})
	}
	return result
}

func cloneRuleExpressions(expressions []expr.Any) []expr.Any {
	cloned := make([]expr.Any, 0, len(expressions))
	for _, expression := range expressions {
		if _, ok := expression.(*expr.Counter); ok {
			cloned = append(cloned, &expr.Counter{})
			continue
		}
		cloned = append(cloned, expression)
	}
	return cloned
}

func findTCPRedirectRule(rules []*nftables.Rule) *nftables.Rule {
	for _, rule := range rules {
		comment, managed := userdata.GetString(rule.UserData, userdata.TypeComment)
		if managed && (comment == nftProxyTCPRuleComment || comment == nftProxyRouteRuleComment) {
			continue
		}
		for _, expression := range rule.Exprs {
			if _, ok := expression.(*expr.Redir); ok {
				return rule
			}
		}
	}
	return nil
}

func removeMarkRules(tableName string) error {
	nft, err := nftables.New()
	if err != nil {
		return err
	}
	defer nft.CloseLasting()
	table, err := nft.ListTableOfFamily(tableName, nftables.TableFamilyINet)
	if err != nil {
		return err
	}
	for _, chainName := range []string{"prerouting", "prerouting_udp_icmp"} {
		chain, chainErr := nft.ListChain(table, chainName)
		if chainErr != nil {
			return chainErr
		}
		rules, rulesErr := nft.GetRules(table, chain)
		if rulesErr != nil {
			return rulesErr
		}
		if err := deleteManagedMarkRules(nft, rules); err != nil {
			return err
		}
	}
	return nft.Flush()
}

func deleteManagedMarkRules(nft *nftables.Conn, rules []*nftables.Rule) error {
	for _, rule := range rules {
		comment, found := userdata.GetString(rule.UserData, userdata.TypeComment)
		if !found || (comment != nftDirectRuleComment && comment != nftProxyTCPRuleComment && comment != nftProxyRouteRuleComment) {
			continue
		}
		if err := nft.DelRule(rule); err != nil {
			return err
		}
	}
	return nil
}

func (f *tcFastPath) Replace(sets DecisionSets) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("TC eBPF kernel-direct is closed")
	}
	if err := f.replaceLocked(sets); err != nil {
		return f.failOpenLocked(err)
	}
	return nil
}

func (f *tcFastPath) replaceLocked(sets DecisionSets) error {
	next4, next6, err := buildLPMEntries(sets, f.staticDirect, f.staticProxy, f.staticBypass, f.proxySteering, f.maxEntries)
	if err != nil {
		return err
	}
	odd := f.generationValue + 1
	if odd&1 == 0 {
		odd++
	}
	key := uint32(0)
	if err := f.generation.Put(key, odd); err != nil {
		return fmt.Errorf("publish odd decision generation: %w", err)
	}
	f.generationValue = odd
	if err := replaceDecisionMap(f.ipv4, f.entries4, next4); err != nil {
		return fmt.Errorf("sync IPv4 LPM decisions: %w", err)
	}
	if err := replaceDecisionMap(f.ipv6, f.entries6, next6); err != nil {
		return fmt.Errorf("sync IPv6 LPM decisions: %w", err)
	}
	even := odd + 1
	if err := f.generation.Put(key, even); err != nil {
		return fmt.Errorf("publish even decision generation: %w", err)
	}
	f.generationValue = even
	f.updatedAt = time.Now()
	f.lastError = ""
	return nil
}

func buildLPMEntries(sets DecisionSets, staticDirect, staticProxy, staticBypass []netip.Prefix, proxySteering bool, maxEntries uint32) (map[lpmKey4]uint32, map[lpmKey6]uint32, error) {
	entries4 := make(map[lpmKey4]uint32)
	entries6 := make(map[lpmKey6]uint32)
	add := func(prefix netip.Prefix, decision uint32) {
		prefix, ok := normalizePrefix(prefix)
		if !ok {
			return
		}
		if prefix.Addr().Is4() {
			entries4[lpmKey4{PrefixLen: uint32(prefix.Bits()), Address: prefix.Addr().As4()}] = decision
		} else {
			entries6[lpmKey6{PrefixLen: uint32(prefix.Bits()), Address: prefix.Addr().As16()}] = decision
		}
	}
	for _, prefix := range staticDirect {
		add(prefix, decisionDirect)
	}
	if sets.Direct != nil {
		for _, prefix := range sets.Direct.Prefixes() {
			add(prefix, decisionDirect)
		}
	}
	if proxySteering {
		add(netip.MustParsePrefix("0.0.0.0/0"), decisionProxy)
		add(netip.MustParsePrefix("::/0"), decisionProxy)
		// Add PROXY last so it wins an exact-prefix collision. A
		// more-specific DIRECT prefix still wins naturally through LPM
		// longest-prefix matching.
		for _, prefix := range staticProxy {
			add(prefix, decisionProxy)
		}
		if sets.Proxy != nil {
			for _, prefix := range sets.Proxy.Prefixes() {
				add(prefix, decisionProxy)
			}
		}
	}
	// Host addresses are added last and at full prefix length. This prevents a
	// WAN-attached classifier from redirecting replies to local sockets back
	// into TUN before nftables can apply its local-address exclusion.
	for _, prefix := range staticBypass {
		add(prefix, decisionBypass)
	}
	if uint64(len(entries4))+uint64(len(entries6)) > uint64(maxEntries) {
		return nil, nil, fmt.Errorf("decision LPM maps exceed %d prefixes", maxEntries)
	}
	return entries4, entries6, nil
}

func normalizePrefixes(prefixes []netip.Prefix) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(prefixes))
	for _, prefix := range prefixes {
		if normalized, ok := normalizePrefix(prefix); ok {
			result = append(result, normalized)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return netipx.ComparePrefix(result[i], result[j]) < 0
	})
	return result
}

func normalizePrefix(prefix netip.Prefix) (netip.Prefix, bool) {
	if !prefix.IsValid() {
		return netip.Prefix{}, false
	}
	addr := prefix.Addr()
	bits := prefix.Bits()
	if addr.Is4In6() {
		addr = addr.Unmap()
		bits -= 96
	}
	if bits < 0 || bits > addr.BitLen() {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(addr, bits).Masked(), true
}

type bpfMapWriter interface {
	Delete(key any) error
	Put(key, value any) error
}

func replaceDecisionMap[K comparable](bpfMap bpfMapWriter, current, next map[K]uint32) error {
	for key, value := range current {
		if nextValue, found := next[key]; found && nextValue == value {
			continue
		}
		if err := bpfMap.Delete(key); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
			return err
		}
		delete(current, key)
	}
	for key, value := range next {
		if currentValue, found := current[key]; found && currentValue == value {
			continue
		}
		if err := bpfMap.Put(key, value); err != nil {
			return err
		}
		current[key] = value
	}
	return nil
}

func (f *tcFastPath) failOpenLocked(cause error) error {
	err := errors.Join(cause, f.detachFiltersLocked())
	f.lastError = err.Error()
	return err
}

func (f *tcFastPath) detachFiltersLocked() error {
	var result error
	remaining := make([]attachedFilter, 0, len(f.filters))
	for index := len(f.filters) - 1; index >= 0; index-- {
		attached := f.filters[index]
		if err := netlink.FilterDel(attached.filter); err != nil && !errors.Is(err, unix.ENOENT) {
			result = errors.Join(result, err)
			remaining = append(remaining, attached)
		}
	}
	f.filters = remaining
	return result
}

func (f *tcFastPath) Status() FastPathStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	status := FastPathStatus{
		Backend:                "ebpf-tc-lpm-lru",
		RequestedInterfaces:    append([]string(nil), f.requested...),
		Interfaces:             append([]string(nil), f.interfaces...),
		ProxyRedirectInterface: f.proxyRedirectInterface,
		Mark:                   f.mark,
		ProxyMark:              f.proxyMark,
		InputMark:              f.inputMark,
		IPv4:                   len(f.entries4),
		IPv6:                   len(f.entries6),
		FlowMaxEntries:         f.flowEntries,
		UpdatedAt:              f.updatedAt,
		LastError:              f.lastError,
	}
	if f.proxyRedirectIfIndex > 0 {
		status.Backend = "ebpf-tc-lpm-lru-redirect"
	}
	for _, value := range f.entries4 {
		if value == decisionDirect {
			status.DirectPrefixes++
		} else if value == decisionProxy {
			status.ProxyPrefixes++
		} else if value == decisionBypass {
			status.BypassPrefixes++
		}
	}
	for _, value := range f.entries6 {
		if value == decisionDirect {
			status.DirectPrefixes++
		} else if value == decisionProxy {
			status.ProxyPrefixes++
		} else if value == decisionBypass {
			status.BypassPrefixes++
		}
	}
	if f.tcStats != nil {
		readCounter := func(index uint32) bpfCounter {
			var values []bpfCounter
			var total bpfCounter
			if err := f.tcStats.Lookup(index, &values); err == nil {
				for _, value := range values {
					total.Packets += value.Packets
					total.Bytes += value.Bytes
				}
			}
			return total
		}
		direct := readCounter(uint32(statDirect))
		proxy := readCounter(uint32(statProxy))
		hits := readCounter(uint32(statFlowHit))
		status.DirectPackets = direct.Packets
		status.DirectBytes = direct.Bytes
		status.ProxyPackets = proxy.Packets
		status.ProxyBytes = proxy.Bytes
		status.FlowHits = hits.Packets
		status.Packets = direct.Packets
		status.Bytes = direct.Bytes
	}
	return status
}

func (f *tcFastPath) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	unregisterFastPath(f)
	result := f.detachFiltersLocked()
	if err := removeMarkRules(f.tableName); err != nil {
		result = errors.Join(result, fmt.Errorf("remove nftables eBPF decision rules: %w", err))
	}
	if f.program != nil {
		result = errors.Join(result, f.program.Close())
	}
	if f.tcStats != nil {
		result = errors.Join(result, f.tcStats.Close())
	}
	if f.generation != nil {
		result = errors.Join(result, f.generation.Close())
	}
	if f.flows != nil {
		result = errors.Join(result, f.flows.Close())
	}
	if f.ipv6 != nil {
		result = errors.Join(result, f.ipv6.Close())
	}
	if f.ipv4 != nil {
		result = errors.Join(result, f.ipv4.Close())
	}
	return result
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
